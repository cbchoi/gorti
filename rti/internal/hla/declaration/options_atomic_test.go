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

func TestDeclarationAdvisoryAdmissionFailureRetry(t *testing.T) {
	for _, kind := range []string{"ordinary-object", "regional-object", "ordinary-interaction", "regional-interaction"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			m := declaration.New()
			out := delivery.NewWithBatch(1, 1, 0)
			m.SetAdvisoryOutbox(out)
			first, cancel1, err := out.Subscribe(ctx, "f", 1)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cancel1() }()
			second, cancel2, err := out.Subscribe(ctx, "f", 2)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cancel2() }()
			for _, h := range []core.FederateHandle{1, 2} {
				if err := m.PublishObjectClassAttributes(ctx, "f", h, 10, []core.AttributeHandle{1}); err != nil {
					t.Fatal(err)
				}
				if err := m.PublishInteractionClass(ctx, "f", h, 20); err != nil {
					t.Fatal(err)
				}
			}
			hooks := 0
			m.SetOnSubscribeObjectClass(func(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, []core.AttributeHandle) {
				hooks++
			})
			invoke := func() error {
				switch kind {
				case "ordinary-object":
					return m.SubscribeObjectClassAttributes(ctx, "f", 3, 10, []core.AttributeHandle{1})
				case "regional-object":
					return m.SetRegionalObjectSubscriptionInterest(ctx, "f", 3, 10, []core.AttributeHandle{1})
				case "ordinary-interaction":
					return m.SubscribeInteractionClass(ctx, "f", 3, 20)
				default:
					return m.SetRegionalInteractionSubscriptionInterest(ctx, "f", 3, 20, true)
				}
			}
			if err := out.Send(ctx, "f", 2, &declaration.AdvisoryEvent{Sequence: 99}); err != nil {
				t.Fatal(err)
			}
			before := m.ExportState("f")
			if err := invoke(); !errors.Is(err, core.ErrFederateOverflow) {
				t.Fatalf("full queue error=%v", err)
			}
			if !reflect.DeepEqual(before, m.ExportState("f")) {
				t.Fatal("failed advisory admission committed declaration state")
			}
			if hooks != 0 {
				t.Fatal("failed subscribe emitted late-discovery hook")
			}
			select {
			case <-first:
				t.Fatal("failed all-recipient admission emitted partial callback")
			default:
			}
			<-second
			if err := invoke(); err != nil {
				t.Fatal(err)
			}
			for _, queue := range []<-chan []core.OutboundEvent{first, second} {
				select {
				case batch := <-queue:
					if len(batch) != 1 || batch[0].(*declaration.AdvisoryEvent).Sequence == 99 {
						t.Fatal("retry callback mismatch")
					}
				default:
					t.Fatal("retry lost advisory edge")
				}
			}
			if err := invoke(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-first:
				t.Fatal("repeat produced duplicate edge")
			default:
			}
			select {
			case <-second:
				t.Fatal("repeat produced duplicate edge")
			default:
			}
		})
	}
}

func TestDeclarationResignCleanupWithFullAdvisoryQueue(t *testing.T) {
	ctx := context.Background()
	m := declaration.New()
	_ = m.PublishObjectClassAttributes(ctx, "f", 1, 10, []core.AttributeHandle{1})
	_ = m.PublishObjectClassAttributes(ctx, "f", 3, 10, []core.AttributeHandle{1})
	_ = m.SubscribeObjectClassAttributes(ctx, "f", 3, 10, []core.AttributeHandle{1})
	_ = m.PublishInteractionClass(ctx, "f", 1, 20)
	_ = m.PublishInteractionClass(ctx, "f", 3, 20)
	_ = m.SubscribeInteractionClass(ctx, "f", 3, 20)
	_ = m.SetAdvisorySwitch(ctx, "f", 3, core.AdvisoryAttributeScope, false)
	_ = m.SetRegionalObjectSubscriptionInterest(ctx, "f", 3, 10, []core.AttributeHandle{1})
	_ = m.SetRegionalInteractionSubscriptionInterest(ctx, "f", 3, 20, true)
	out := delivery.NewWithBatch(1, 1, 0)
	queue, cancel, err := out.Subscribe(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()
	if err := out.Send(ctx, "f", 1, &declaration.AdvisoryEvent{Sequence: 99}); err != nil {
		t.Fatal(err)
	}
	m.SetAdvisoryOutbox(out)
	m.OnFederateResign("f", 3)
	state := m.ExportState("f")
	if len(state.ObjectSubscriptions) != 0 || len(state.InteractionSubscriptions) != 0 || len(state.ObjectOptions) != 0 || len(state.InteractionOptions) != 0 || len(state.AdvisorySwitches) != 0 || len(state.RegionalObjectInterests) != 0 || len(state.RegionalInteractionInterests) != 0 {
		t.Fatalf("queue-full resign retained participant state: %+v", state)
	}
	if !reflect.DeepEqual(m.PublishersFor(ctx, "f", 10, 1), []core.FederateHandle{1}) || !reflect.DeepEqual(m.InteractionPublishersFor(ctx, "f", 20), []core.FederateHandle{1}) {
		t.Fatal("resigned publications retained")
	}
	if batch := <-queue; len(batch) != 1 || batch[0].Seq() != 99 {
		t.Fatal("failed lifecycle admission changed existing callback queue")
	}
}
