package object

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func transportationScope() core.TransportationScope {
	return core.TransportationScope{Member: core.MemberIdentity{Federation: "fed", Generation: 4, Name: "owner", Handle: 1, Incarnation: 8}, CallbackEpoch: 3}
}

func TestTransportationPreparationDoesNotActivateAndReplayDoesNotReapply(t *testing.T) {
	f := newTransportationFixture(t)
	ctx, scope := context.Background(), transportationScope()
	change, err := f.r.PrepareAttributeTransportationChange(ctx, scope, f.obj, []core.AttributeHandle{1}, core.TransportTypeBestEffort)
	transportationOK(t, err)
	f.checkAttribute(t, 1, core.TransportTypeReliable)
	if len(change.Ticket) != 32 || change.Object != f.obj || change.Transportation != core.TransportTypeBestEffort {
		t.Fatalf("invalid preparation: %+v", change)
	}
	_, err = f.r.PrepareAttributeTransportationChange(ctx, scope, f.obj, []core.AttributeHandle{2, 1}, core.TransportTypeBestEffort)
	if !errors.Is(err, core.ErrAttributeAlreadyBeingChanged) {
		t.Fatalf("overlap error=%v", err)
	}
	other, err := f.r.PrepareAttributeTransportationChange(ctx, scope, f.obj, []core.AttributeHandle{2}, core.TransportTypeBestEffort)
	transportationOK(t, err)
	activated, err := f.r.ActivateTransportationChange(ctx, scope, change.Ticket)
	transportationOK(t, err)
	if activated.Transportation != change.Transportation {
		t.Fatal("activation did not report achieved transportation")
	}
	f.checkAttribute(t, 1, core.TransportTypeBestEffort)
	f.checkAttribute(t, 2, core.TransportTypeReliable)
	transportationOK(t, f.r.RequestAttributeTransportationTypeChange(ctx, "fed", 1, f.obj, []core.AttributeHandle{1}, core.TransportTypeReliable))
	_, err = f.r.ActivateTransportationChange(ctx, scope, change.Ticket)
	transportationOK(t, err)
	f.checkAttribute(t, 1, core.TransportTypeReliable)
	_, err = f.r.ActivateTransportationChange(ctx, scope, other.Ticket)
	transportationOK(t, err)
	f.checkAttribute(t, 2, core.TransportTypeBestEffort)
}

func TestTransportationActivationIsIdentityAndEpochFenced(t *testing.T) {
	for _, field := range []string{"federation", "generation", "handle", "incarnation", "epoch", "ticket"} {
		t.Run(field, func(t *testing.T) {
			f := newTransportationFixture(t)
			ctx, scope := context.Background(), transportationScope()
			change, err := f.r.PrepareInteractionTransportationChange(ctx, scope, 7, core.TransportTypeBestEffort)
			transportationOK(t, err)
			wrong, ticket := scope, append([]byte(nil), change.Ticket...)
			switch field {
			case "federation":
				wrong.Member.Federation = testOther
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
			if _, err := f.r.ActivateTransportationChange(ctx, wrong, ticket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
				t.Fatalf("foreign activation error=%v", err)
			}
			if got := f.r.InteractionTransportType("fed", 1, 7); got != core.TransportTypeReliable {
				t.Fatalf("early transport=%v", got)
			}
			_, err = f.r.ActivateTransportationChange(ctx, scope, change.Ticket)
			transportationOK(t, err)
		})
	}
}

func TestTransportationPendingTicketsRetireOnLifecycle(t *testing.T) {
	for _, lifecycle := range []string{"restore", "destroy", "resign"} {
		t.Run(lifecycle, func(t *testing.T) {
			f := newTransportationFixture(t)
			ctx, scope := context.Background(), transportationScope()
			change, err := f.r.PrepareAttributeTransportationChange(ctx, scope, f.obj, []core.AttributeHandle{1}, core.TransportTypeReliable)
			transportationOK(t, err)
			switch lifecycle {
			case "restore":
				commit, err := f.r.PrepareImportState("fed", f.r.ExportState("fed"))
				transportationOK(t, err)
				commit()
			case "destroy":
				f.r.transports.removeFederation("fed")
			case "resign":
				plan := f.r.PrepareResignDeletions(ctx, "fed", 1, false)
				plan.CommitState()
				plan.Release()
			}
			if _, err := f.r.ActivateTransportationChange(ctx, scope, change.Ticket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
				t.Fatalf("retired ticket error=%v", err)
			}
		})
	}
}

func TestTransportationOwnershipLossPermanentlyShrinksPendingConfirmation(t *testing.T) {
	for _, requested := range [][]core.AttributeHandle{{1}, {1, 2}} {
		f := newTransportationFixture(t)
		ctx, scope := context.Background(), transportationScope()
		prepared, err := f.r.PrepareAttributeTransportationChange(ctx, scope, f.obj, requested, core.TransportTypeReliable)
		transportationOK(t, err)
		transportationOK(t, f.owners.UnconditionalDivest(ctx, "fed", 1, f.obj, []core.AttributeHandle{1}))
		transportationOK(t, f.owners.AcquireIfAvailable(ctx, "fed", 1, f.obj, []core.AttributeHandle{1}, nil))
		activated, err := f.r.ActivateTransportationChange(ctx, scope, prepared.Ticket)
		transportationOK(t, err)
		if len(activated.Attributes) != len(requested)-1 || (len(activated.Attributes) == 1 && activated.Attributes[0] != 2) {
			t.Fatalf("activation must exclude lost and reacquired attribute: %+v", activated)
		}
		f.checkAttribute(t, 1, core.TransportTypeBestEffort)
		f.checkAttribute(t, 2, core.TransportTypeReliable)
		_, err = f.r.PrepareAttributeTransportationChange(ctx, scope, f.obj, []core.AttributeHandle{1}, core.TransportTypeReliable)
		transportationOK(t, err)
	}
}

func TestTransportationInteractionPreparationIsPendingUntilActivation(t *testing.T) {
	f := newTransportationFixture(t)
	ctx, scope := context.Background(), transportationScope()
	change, err := f.r.PrepareInteractionTransportationChange(ctx, scope, 7, core.TransportTypeBestEffort)
	transportationOK(t, err)
	if _, err := f.r.PrepareInteractionTransportationChange(ctx, scope, 7, core.TransportTypeReliable); !errors.Is(err, core.ErrInteractionClassAlreadyBeingChanged) {
		t.Fatalf("pending error=%v", err)
	}
	transportationOK(t, f.decl.UnpublishInteractionClass(ctx, "fed", 1, 7))
	// Confirmation has no publication precondition; an unpublished query still uses the FDD.
	_, err = f.r.ActivateTransportationChange(ctx, scope, change.Ticket)
	transportationOK(t, err)
	transportationOK(t, f.decl.PublishInteractionClass(ctx, "fed", 1, 7))
	if got := f.r.InteractionTransportType("fed", 1, 7); got != core.TransportTypeBestEffort {
		t.Fatalf("transport=%v", got)
	}
}
