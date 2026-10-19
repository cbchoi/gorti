package object

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestTransportationCheckpointProjectionRetainsOwnershipLoss(t *testing.T) {
	for _, attrs := range [][]core.AttributeHandle{{1}, {1, 2}} {
		f := newTransportationFixture(t)
		ctx, scope := context.Background(), transportationScope()
		change, err := f.r.PrepareAttributeTransportationChange(ctx, scope, f.obj, attrs, core.TransportTypeReliable)
		transportationOK(t, err)
		transportationOK(t, f.owners.UnconditionalDivest(ctx, "fed", 1, f.obj, []core.AttributeHandle{1}))
		transportationOK(t, f.owners.AcquireIfAvailable(ctx, "fed", 1, f.obj, []core.AttributeHandle{1}, nil))
		projected := f.r.ExportTransportationCheckpoint("fed")[string(change.Ticket)]
		if projected.Scope != scope || len(projected.Binding.Attributes) != len(attrs)-1 || slices.Contains(projected.Binding.ActualAttributes, 1) || projected.Binding.Activated {
			t.Fatal("lost attribute was reinserted into projection", projected)
		}
		commit, err := f.r.PrepareImportState("fed", f.r.ExportState("fed"))
		transportationOK(t, err)
		commit()
		fresh := scope
		fresh.Member.Generation++
		fresh.Member.Incarnation++
		fresh.CallbackEpoch++
		reservation, err := f.r.ReserveTransportationCheckpoint(ctx, []TransportationCheckpointRestore{{Scope: fresh, Binding: projected.Binding}})
		transportationOK(t, err)
		defer reservation.Release()
		restored := reservation.Changes()[0]
		if bytes.Equal(restored.Ticket, change.Ticket) || restored.Scope != fresh {
			t.Fatal("source ticket or scope reused")
		}
		reservation.Commit()
		activated, err := f.r.ActivateTransportationChange(ctx, fresh, restored.Ticket)
		transportationOK(t, err)
		if !slices.Equal(activated.Attributes, projected.Binding.Attributes) {
			t.Fatal("restored activation expanded retained subset")
		}
		f.checkAttribute(t, 1, core.TransportTypeBestEffort)
		if len(attrs) == 2 {
			f.checkAttribute(t, 2, core.TransportTypeReliable)
		}
		if _, err := f.r.ActivateTransportationChange(ctx, scope, change.Ticket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
			t.Fatal("old ticket survived restore", err)
		}
	}
}

func TestTransportationCheckpointActivatedAcknowledgementDoesNotReapply(t *testing.T) {
	f := newTransportationFixture(t)
	ctx, scope := context.Background(), transportationScope()
	change, err := f.r.PrepareInteractionTransportationChange(ctx, scope, 7, core.TransportTypeBestEffort)
	transportationOK(t, err)
	_, err = f.r.ActivateTransportationChange(ctx, scope, change.Ticket)
	transportationOK(t, err)
	binding := f.r.ExportTransportationCheckpoint("fed")[string(change.Ticket)].Binding
	if !binding.Activated {
		t.Fatal("activation phase not captured")
	}
	transportationOK(t, f.r.RequestInteractionTransportationTypeChange(ctx, "fed", 1, 7, core.TransportTypeReliable))
	transportationOK(t, f.decl.UnpublishInteractionClass(ctx, "fed", 1, 7))
	commit, err := f.r.PrepareImportState("fed", f.r.ExportState("fed"))
	transportationOK(t, err)
	commit()
	scope.Member.Incarnation++
	scope.CallbackEpoch++
	reservation, err := f.r.ReserveTransportationCheckpoint(ctx, []TransportationCheckpointRestore{{Scope: scope, Binding: binding}})
	transportationOK(t, err)
	defer reservation.Release()
	reservation.Commit()
	_, err = f.r.ActivateTransportationChange(ctx, scope, reservation.Changes()[0].Ticket)
	transportationOK(t, err)
	if got := f.r.InteractionTransportType("fed", 1, 7); got != core.TransportTypeReliable {
		t.Fatal("restored acknowledgement reapplied activated effect", got)
	}
}

func TestTransportationCheckpointBatchAtomicAndRollback(t *testing.T) {
	f := newTransportationFixture(t)
	ctx, scope := context.Background(), transportationScope()
	change, err := f.r.PrepareAttributeTransportationChange(ctx, scope, f.obj, []core.AttributeHandle{1}, core.TransportTypeBestEffort)
	transportationOK(t, err)
	binding := f.r.ExportTransportationCheckpoint("fed")[string(change.Ticket)].Binding
	commit, err := f.r.PrepareImportState("fed", f.r.ExportState("fed"))
	transportationOK(t, err)
	commit()
	bad := binding
	bad.ActualAttributes = []core.AttributeHandle{2}
	if _, err := f.r.ReserveTransportationCheckpoint(ctx, []TransportationCheckpointRestore{{Scope: scope, Binding: binding}, {Scope: scope, Binding: bad}}); err == nil {
		t.Fatal("mismatched registered coordinate accepted")
	}
	if len(f.r.transports.tickets) != 0 {
		t.Fatal("failed batch left partial ticket state")
	}
	for _, commitFirst := range []bool{false, true} {
		reservation, err := f.r.ReserveTransportationCheckpoint(ctx, []TransportationCheckpointRestore{{Scope: scope, Binding: binding}})
		transportationOK(t, err)
		ticket := reservation.Changes()[0].Ticket
		if commitFirst {
			reservation.Commit()
			reservation.Rollback()
		} else {
			reservation.Release()
		}
		if _, err := f.r.ActivateTransportationChange(ctx, scope, ticket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
			t.Fatal("aborted prepublication ticket remains usable", err)
		}
	}
	f.checkAttribute(t, 1, core.TransportTypeReliable)
}

func TestTransportationCheckpointActivatedTicketsEnterBoundedCacheOnConsumption(t *testing.T) {
	f := newTransportationFixture(t)
	ctx, scope := context.Background(), transportationScope()
	change, err := f.r.PrepareInteractionTransportationChange(ctx, scope, 7, core.TransportTypeBestEffort)
	transportationOK(t, err)
	_, err = f.r.ActivateTransportationChange(ctx, scope, change.Ticket)
	transportationOK(t, err)
	binding := f.r.ExportTransportationCheckpoint("fed")[string(change.Ticket)].Binding
	transportationOK(t, f.r.RequestInteractionTransportationTypeChange(ctx, "fed", 1, 7, core.TransportTypeReliable))
	commit, err := f.r.PrepareImportState("fed", f.r.ExportState("fed"))
	transportationOK(t, err)
	commit()
	requests := make([]TransportationCheckpointRestore, 257)
	for i := range requests {
		requests[i] = TransportationCheckpointRestore{Scope: scope, Binding: binding}
	}
	reservation, err := f.r.ReserveTransportationCheckpoint(ctx, requests)
	transportationOK(t, err)
	defer reservation.Release()
	reservation.Commit()
	changes := reservation.Changes()
	for _, fresh := range changes {
		_, err := f.r.ActivateTransportationChange(ctx, scope, fresh.Ticket)
		transportationOK(t, err)
	}
	if len(f.r.ExportTransportationCheckpoint("fed")) != 256 {
		t.Fatal("consumed restored tickets did not enter bounded completed cache")
	}
	if _, err := f.r.ActivateTransportationChange(ctx, scope, changes[0].Ticket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
		t.Fatal("oldest consumed restored ticket was not retired", err)
	}
	if got := f.r.InteractionTransportType("fed", 1, 7); got != core.TransportTypeReliable {
		t.Fatal("acknowledgement cache consumption reapplied effect", got)
	}
}
