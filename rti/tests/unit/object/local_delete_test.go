package object_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func localDeleteFixture(t *testing.T) (*object.Registry, *declaration.Manager, *regionDeliveryOutbox, core.ObjectHandle) {
	t.Helper()
	ctx := context.Background()
	d := declaration.New()
	out := &regionDeliveryOutbox{}
	r, err := object.New(object.Options{Declarations: d, Outbox: out, FOMs: restoreFOMs{}, Clock: core.NewRealClock()})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.PublishObjectClassAttributes(ctx, "local", 1, 7, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{2, 3} {
		if err := d.SubscribeObjectClassAttributes(ctx, "local", h, 7, []core.AttributeHandle{1}); err != nil {
			t.Fatal(err)
		}
	}
	obj, _, err := r.Register(ctx, "local", 1, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	out.deliveries = nil
	return r, d, out, obj
}

func hasLocalKnowledge(r *object.Registry, h core.FederateHandle, obj core.ObjectHandle) bool {
	for _, d := range r.ExportState("local").Discoveries {
		if d.Federate == h && d.Object == obj {
			return true
		}
	}
	return false
}

func TestLocalDeleteRemovesOnlyCallerKnowledge(t *testing.T) {
	r, d, out, obj := localDeleteFixture(t)
	ctx := context.Background()
	if err := d.UnsubscribeObjectClassAttributes(ctx, "local", 2, 7, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if err := r.LocalDelete(ctx, "local", 2, obj); err != nil {
		t.Fatal(err)
	}
	if hasLocalKnowledge(r, 2, obj) || !hasLocalKnowledge(r, 3, obj) || !hasLocalKnowledge(r, 1, obj) {
		t.Fatal("local deletion must forget only the caller")
	}
	if r.ClassOf("local", obj) != 7 || len(out.deliveries) != 0 {
		t.Fatal("local deletion changed global state or notified a peer")
	}
	if err := r.LocalDelete(ctx, "local", 2, obj); !errors.Is(err, core.ErrObjectHandleInvalid) {
		t.Fatalf("repeat deletion: %v", err)
	}
}

func TestLocalDeleteRejectsUnknownAndOwnedObjects(t *testing.T) {
	r, _, _, obj := localDeleteFixture(t)
	for _, tc := range []struct {
		caller core.FederateHandle
		want   error
	}{{4, core.ErrObjectHandleInvalid}, {1, core.ErrFederateOwnsAttributes}} {
		if err := r.LocalDelete(context.Background(), "local", tc.caller, obj); !errors.Is(err, tc.want) {
			t.Fatalf("caller %d: got %v want %v", tc.caller, err, tc.want)
		}
	}
	if !hasLocalKnowledge(r, 1, obj) {
		t.Fatal("failed deletion changed owner knowledge")
	}
}

func TestLocalDeleteRediscoveryForContinuingSubscription(t *testing.T) {
	r, _, out, obj := localDeleteFixture(t)
	if err := r.LocalDelete(context.Background(), "local", 2, obj); err != nil {
		t.Fatal(err)
	}
	if len(out.deliveries) != 1 || out.deliveries[0].Recipient != 2 {
		t.Fatalf("rediscovery deliveries: %+v", out.deliveries)
	}
	e := out.deliveries[0].Event.(*object.CallbackEvent)
	if e.Kind != object.CallbackDiscover || e.Object != obj || !hasLocalKnowledge(r, 2, obj) {
		t.Fatalf("rediscovery: %+v", e)
	}
}
