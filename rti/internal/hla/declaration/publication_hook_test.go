package declaration_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

func TestPublicationHookRunsAfterCommitForNewAttributesOnly(t *testing.T) {
	ctx := context.Background()
	m := declaration.New()
	var calls [][]core.AttributeHandle
	m.SetOnPublishObjectClass(func(ctx context.Context, fed core.FederationName, h core.FederateHandle, class core.ObjectClassHandle, attrs []core.AttributeHandle) {
		// Reentry proves that the hook does not hold the declaration lock.
		if err := m.ValidateObjectPublication(ctx, fed, h, class, attrs); err != nil {
			t.Error("hook ran before publication commit", err)
		}
		calls = append(calls, append([]core.AttributeHandle(nil), attrs...))
		attrs[0] = 999
	})
	input := []core.AttributeHandle{2, 1, 2}
	if err := m.PublishObjectClassAttributes(ctx, "f", 1, 7, input); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, []core.AttributeHandle{2, 1, 2}) || !reflect.DeepEqual(calls, [][]core.AttributeHandle{{1, 2}}) {
		t.Fatal("input mutated or publication delta duplicated", input, calls)
	}
	if err := m.PublishObjectClassAttributes(ctx, "f", 1, 7, []core.AttributeHandle{2, 1}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatal("idempotent publication invoked the continuation hook")
	}
	if err := m.PublishObjectClassAttributes(ctx, "f", 1, 7, []core.AttributeHandle{3, 2}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, [][]core.AttributeHandle{{1, 2}, {3}}) {
		t.Fatal("hook included existing publication", calls)
	}
	if err := m.UnpublishObjectClassAttributes(ctx, "f", 1, 7, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if err := m.PublishObjectClassAttributes(ctx, "f", 1, 7, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Fatal("republication did not resume offers")
	}
}

func TestPublicationHookDoesNotRunWhenAdvisoryReservationFails(t *testing.T) {
	ctx := context.Background()
	m := declaration.New()
	out := delivery.NewWithBatch(1, 1, 0)
	m.SetAdvisoryOutbox(out)
	queue, release, err := out.Subscribe(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	}()
	if err := m.SubscribeObjectClassAttributes(ctx, "f", 2, 7, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if err := out.Send(ctx, "f", 1, &declaration.AdvisoryEvent{}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	m.SetOnPublishObjectClass(func(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle) {
		calls++
	})
	before := m.ExportState("f")
	if err := m.PublishObjectClassAttributes(ctx, "f", 1, 7, []core.AttributeHandle{1}); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatal("expected failed publication admission", err)
	}
	if calls != 0 || !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("failed publication emitted ownership continuation or mutated state")
	}
	<-queue
	if err := m.PublishObjectClassAttributes(ctx, "f", 1, 7, []core.AttributeHandle{1}); err != nil || calls != 1 {
		t.Fatal("publication retry did not notify once", err, calls)
	}
}
