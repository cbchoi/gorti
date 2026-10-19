package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMOMTransportationPreparationDoesNotChangeTransportBeforeCallbackEntry(t *testing.T) {
	for _, service := range []string{momServiceRequestAttributeTransportationTypeChange, momServiceRequestInteractionTransportationTypeChange} {
		t.Run(service, func(t *testing.T) {
			f := newMOMExecutionFixture(t, service)
			ctx := context.Background()
			const fed = "catalog-provider"
			if f.command.Object != 0 {
				if err := f.deps.Objects.RequestAttributeTransportationTypeChange(ctx, fed, f.target, f.command.Object, f.command.Attributes, core.TransportTypeReliable); err != nil {
					t.Fatal(err)
				}
			}
			query := func() core.TransportType {
				t.Helper()
				var actual core.TransportType
				var err error
				if f.command.Object != 0 {
					actual, err = f.deps.Objects.QueryAttributeTransportationType(ctx, fed, f.target, f.command.Object, f.command.Attributes[0])
				} else {
					actual, err = f.deps.Objects.QueryInteractionTransportationType(ctx, fed, f.sender, f.target, f.command.InteractionClass)
				}
				if err != nil {
					t.Fatal(err)
				}
				return actual
			}
			before := query()
			if before == core.TransportTypeBestEffort {
				t.Fatal("fixture already best effort")
			}
			if _, err := f.executor.ExecuteMOMService(ctx, fed, f.sender, f.command); err != nil {
				t.Fatal(err)
			}
			if actual := query(); actual != before {
				t.Fatal("MOM request activated transport before confirmation entry")
			}
			if _, err := f.executor.ExecuteMOMService(ctx, fed, f.sender, f.command); !errors.Is(err, core.ErrAttributeAlreadyBeingChanged) && !errors.Is(err, core.ErrInteractionClassAlreadyBeingChanged) {
				t.Fatalf("duplicate pending preparation=%v", err)
			}
			if _, err := f.deps.Objects.ActivateTransportationChange(ctx, f.prepared.Scope, f.prepared.Ticket); err != nil {
				t.Fatal(err)
			}
			if actual := query(); actual != core.TransportTypeBestEffort {
				t.Fatal("actual callback entry did not activate prepared transport")
			}
		})
	}
}

type momActivationWithoutCancellation struct{ core.TransportationActivation }

func TestMOMTransportationMissingCancellationFailsBeforePreparation(t *testing.T) {
	f := newMOMExecutionFixture(t, momServiceRequestInteractionTransportationTypeChange)
	deps := f.executor.(*momServiceExecutor).deps
	confirmation, err := newMOMTransportationConfirmation(momTransportationConfirmationDependencies{Federations: deps.Federations, Float: momActivationWithoutCancellation{deps.FloatObjects}, Integer: momActivationWithoutCancellation{deps.IntegerObjects}, Outbox: delivery.New(4), CallbackEpoch: func(core.FederationName, core.FederateHandle) (uint64, error) {
		t.Fatal("unavailable confirmation read callback epoch")
		return 0, nil
	}})
	if confirmation != nil || status.Code(err) != codes.Unimplemented {
		t.Fatalf("missing cancellation: helper present=%t error=%v", confirmation != nil, err)
	}
	if _, err := f.executor.ExecuteMOMService(context.Background(), "catalog-provider", f.sender, f.command); err != nil {
		t.Fatal("failed composition left a pending ticket:", err)
	}
}

func TestMOMTransportationQueueOverflowRollsBackPendingPreparation(t *testing.T) {
	f := newMOMExecutionFixture(t, momServiceRequestInteractionTransportationTypeChange)
	deps := f.executor.(*momServiceExecutor).deps
	queue := delivery.NewWithBatch(1, 1, 0)
	const fed = "catalog-provider"
	queue.Bind(fed, f.target)
	confirmation, err := newMOMTransportationConfirmation(momTransportationConfirmationDependencies{Federations: deps.Federations, Float: deps.FloatObjects, Integer: deps.IntegerObjects, CallbackEpoch: queue.CallbackEpoch, Outbox: queue})
	if err != nil {
		t.Fatal(err)
	}
	blocker := &momTransportationEvent{event: &rtiv1.FederateEvent{}}
	if err := queue.Send(context.Background(), fed, f.target, blocker); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		err := confirmation(context.Background(), fed, f.target, f.command, core.TransportTypeBestEffort)
		if !errors.Is(err, core.ErrFederateOverflow) {
			t.Fatalf("attempt %d retained a failed preparation: %v", attempt, err)
		}
		actual, err := f.deps.Objects.QueryInteractionTransportationType(context.Background(), fed, f.sender, f.target, f.command.InteractionClass)
		if err != nil || actual != core.TransportTypeReliable {
			t.Fatalf("rejected delivery changed transportation: %v %v", actual, err)
		}
	}
	stream, release, err := queue.Subscribe(context.Background(), fed, f.target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	}()
	<-stream
	if err := confirmation(context.Background(), fed, f.target, f.command, core.TransportTypeBestEffort); err != nil {
		t.Fatal("retry after draining real queue:", err)
	}
	select {
	case batch := <-stream:
		if len(batch) != 1 || len(batch[0].(*momTransportationEvent).Inner().GetTransportationChangeConfirmed().Ticket) != 32 {
			t.Fatal("retry did not queue a real prepared confirmation")
		}
	case <-time.After(time.Second):
		t.Fatal("confirmation queue did not deliver retry")
	}
}
