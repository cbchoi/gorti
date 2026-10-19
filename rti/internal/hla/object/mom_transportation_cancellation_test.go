package object

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestMOMTransportationCancellationRetiresOnlyPendingScopeTicket(t *testing.T) {
	for _, attribute := range []bool{false, true} {
		f := newTransportationFixture(t)
		ctx, scope := context.Background(), transportationScope()
		prepare := func() (core.TransportationChange, error) {
			if attribute {
				return f.r.PrepareAttributeTransportationChange(ctx, scope, f.obj, []core.AttributeHandle{1}, core.TransportTypeBestEffort)
			}
			return f.r.PrepareInteractionTransportationChange(ctx, scope, 7, core.TransportTypeBestEffort)
		}
		change, err := prepare()
		transportationOK(t, err)
		for _, mismatch := range []string{"federation", "generation", "handle", "incarnation", "epoch", "ticket"} {
			wrong, ticket := scope, append([]byte(nil), change.Ticket...)
			switch mismatch {
			case "federation":
				wrong.Member.Federation = testForeign
			case "generation":
				wrong.Member.Generation++
			case "handle":
				wrong.Member.Handle++
			case "incarnation":
				wrong.Member.Incarnation++
			case "epoch":
				wrong.CallbackEpoch++
			case "ticket":
				ticket[0] ^= 1
			}
			if err := f.r.CancelTransportationChange(ctx, wrong, ticket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
				t.Fatalf("foreign %s cancellation=%v", mismatch, err)
			}
		}
		transportationOK(t, f.r.CancelTransportationChange(ctx, scope, change.Ticket))
		if _, err := f.r.ActivateTransportationChange(ctx, scope, change.Ticket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
			t.Fatal("cancelled ticket activated", err)
		}
		if attribute {
			f.checkAttribute(t, 1, core.TransportTypeReliable)
		} else if f.r.InteractionTransportType("fed", 1, 7) != core.TransportTypeReliable {
			t.Fatal("cancelled request changed transport")
		}
		next, err := prepare()
		transportationOK(t, err)
		_, err = f.r.ActivateTransportationChange(ctx, scope, next.Ticket)
		transportationOK(t, err)
		if err := f.r.CancelTransportationChange(ctx, scope, next.Ticket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
			t.Fatal("activated ticket cancellation succeeded", err)
		}
		if _, err := f.r.ActivateTransportationChange(ctx, scope, next.Ticket); err != nil {
			t.Fatal("cancellation erased activation retry evidence", err)
		}
	}
}

func TestMOMTransportationCancellationAndActivationHaveOneWinner(t *testing.T) {
	for attempt := 0; attempt < 40; attempt++ {
		f := newTransportationFixture(t)
		ctx, scope := context.Background(), transportationScope()
		change, err := f.r.PrepareInteractionTransportationChange(ctx, scope, 7, core.TransportTypeBestEffort)
		transportationOK(t, err)
		start := make(chan struct{})
		cancelled, activated := make(chan error, 1), make(chan error, 1)
		go func() { <-start; cancelled <- f.r.CancelTransportationChange(ctx, scope, change.Ticket) }()
		go func() {
			<-start
			_, err := f.r.ActivateTransportationChange(ctx, scope, change.Ticket)
			activated <- err
		}()
		close(start)
		cancelErr, activationErr := <-cancelled, <-activated
		if (cancelErr == nil) == (activationErr == nil) {
			t.Fatalf("cancel=%v activation=%v", cancelErr, activationErr)
		}
		if cancelErr == nil {
			if !errors.Is(activationErr, core.ErrTransportationTicketInvalid) || f.r.InteractionTransportType("fed", 1, 7) != core.TransportTypeReliable {
				t.Fatal("cancelled activation changed transport")
			}
		} else if !errors.Is(cancelErr, core.ErrTransportationTicketInvalid) || f.r.InteractionTransportType("fed", 1, 7) != core.TransportTypeBestEffort {
			t.Fatal("activated ticket was cancelled")
		}
	}
}
