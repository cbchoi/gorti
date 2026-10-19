package object

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func transportationCapacityReviewRequests(scope core.TransportationScope, count int) []TransportationCheckpointRestore {
	requests := make([]TransportationCheckpointRestore, count)
	for i := range requests {
		memberScope := scope
		memberScope.Member.Handle = core.FederateHandle(i + 1)
		requests[i] = TransportationCheckpointRestore{Scope: memberScope, Binding: TransportationCheckpointBinding{
			Interaction: 7, Transportation: core.TransportTypeBestEffort,
		}}
	}
	return requests
}

func transportationCapacityReviewCounts(r *Registry) (int, int) {
	r.transports.mu.RLock()
	defer r.transports.mu.RUnlock()
	return len(r.transports.tickets), len(transportationCheckpointLeasedTickets(r.transports))
}

func TestTransportationCheckpointCapacityReviewActivatedAcknowledgementsBounded(t *testing.T) {
	f := newTransportationFixture(t)
	ctx, scope := context.Background(), transportationScope()
	foreign := scope
	foreign.Member.Federation = testForeign
	transportationOK(t, f.decl.PublishInteractionClass(ctx, testForeign, 1, 7))
	state, owned := f.r.ExportState("fed"), f.owners.ExportState("fed")
	requests := transportationCapacityReviewRequests(scope, 4097)
	for i := range requests {
		requests[i].Binding.Activated = true
	}
	oversized, err := f.r.PrepareTransportationCheckpoint(ctx, requests, state, owned, &transportationFOM{})
	if oversized != nil {
		oversized.Release()
	}
	if !errors.Is(err, core.ErrTransportationPendingLimit) {
		t.Fatal("activated acknowledgements bypassed store capacity", err)
	}
	if live, reserved := transportationCapacityReviewCounts(f.r); live != 0 || reserved != 0 {
		t.Fatalf("oversized acknowledgement batch leaked state: live=%d reserved=%d", live, reserved)
	}
	p, err := f.r.PrepareTransportationCheckpoint(ctx, requests[:4096], state, owned, &transportationFOM{})
	transportationOK(t, err)
	defer p.Rollback()
	foreignRequests := transportationCapacityReviewRequests(foreign, 1)
	foreignRequests[0].Binding.Activated = true
	extra, err := f.r.PrepareTransportationCheckpoint(ctx, foreignRequests, state, owned, &transportationFOM{})
	if extra != nil {
		extra.Release()
	}
	if !errors.Is(err, core.ErrTransportationPendingLimit) {
		t.Fatal("foreign acknowledgement ignored existing lease capacity", err)
	}
	if _, err := f.r.PrepareInteractionTransportationChange(ctx, foreign, 7, core.TransportTypeReliable); !errors.Is(err, core.ErrTransportationPendingLimit) {
		t.Fatal("normal foreign prepare ignored activated lease capacity", err)
	}
	commit, err := f.r.PrepareImportState("fed", state)
	transportationOK(t, err)
	commit()
	p.Commit()
	if live, reserved := transportationCapacityReviewCounts(f.r); live != 4096 || reserved != 0 {
		t.Fatalf("unconsumed acknowledgement install escaped bound: live=%d reserved=%d", live, reserved)
	}
	if _, err := f.r.PrepareInteractionTransportationChange(ctx, foreign, 7, core.TransportTypeReliable); !errors.Is(err, core.ErrTransportationPendingLimit) {
		t.Fatal("normal foreign prepare ignored unconsumed acknowledgement capacity", err)
	}
	if got := f.r.InteractionTransportType("fed", 1, 7); got != core.TransportTypeReliable {
		t.Fatal("acknowledgement installation reapplied transport effect", got)
	}
	ticket := p.Changes()[0].Ticket
	p.Rollback()
	if live, reserved := transportationCapacityReviewCounts(f.r); live != 0 || reserved != 0 {
		t.Fatalf("acknowledgement rollback leaked capacity: live=%d reserved=%d", live, reserved)
	}
	if _, err := f.r.ActivateTransportationChange(ctx, scope, ticket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
		t.Fatal("rolled-back acknowledgement ticket remained usable", err)
	}
	_, err = f.r.PrepareInteractionTransportationChange(ctx, foreign, 7, core.TransportTypeReliable)
	transportationOK(t, err)
}

func TestTransportationCheckpointCapacityReviewForeignPrepareRespectsLease(t *testing.T) {
	f := newTransportationFixture(t)
	ctx, scope := context.Background(), transportationScope()
	foreign := scope
	foreign.Member.Federation = testForeign
	transportationOK(t, f.decl.PublishInteractionClass(ctx, testForeign, 1, 7))
	p, err := f.r.PrepareTransportationCheckpoint(ctx, transportationCapacityReviewRequests(scope, 4096), f.r.ExportState("fed"), f.owners.ExportState("fed"), &transportationFOM{})
	transportationOK(t, err)
	defer p.Release()
	leased := p.Changes()[0]
	if _, err := f.r.ActivateTransportationChange(ctx, scope, leased.Ticket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
		t.Fatal("capacity lease granted activation authority before import", err)
	}
	if _, err := f.r.PrepareInteractionTransportationChange(ctx, foreign, 7, core.TransportTypeReliable); !errors.Is(err, core.ErrTransportationPendingLimit) {
		t.Fatal("foreign normal preparation ignored reserved capacity", err)
	}
	if live, reserved := transportationCapacityReviewCounts(f.r); live != 0 || reserved != 4096 {
		t.Fatalf("failed foreign prepare changed capacity: live=%d reserved=%d", live, reserved)
	}
	if got := f.r.InteractionTransportType("fed", 1, 7); got != core.TransportTypeReliable {
		t.Fatal("lease preparation applied transportation effect", got)
	}
	p.Release()
	p.Release()
	if live, reserved := transportationCapacityReviewCounts(f.r); live != 0 || reserved != 0 {
		t.Fatalf("lease rollback leaked capacity: live=%d reserved=%d", live, reserved)
	}
	if _, err := f.r.ActivateTransportationChange(ctx, scope, leased.Ticket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
		t.Fatal("released lease retained activation authority", err)
	}
	_, err = f.r.PrepareInteractionTransportationChange(ctx, foreign, 7, core.TransportTypeReliable)
	transportationOK(t, err)
}

func TestTransportationCheckpointCapacityReviewReplacementCreditBoundedAfterImport(t *testing.T) {
	f := newTransportationFixture(t)
	ctx, source := context.Background(), transportationScope()
	foreign := source
	foreign.Member.Federation = testForeign
	transportationOK(t, f.decl.PublishInteractionClass(ctx, testForeign, 1, 7))
	foreignChange, err := f.r.PrepareInteractionTransportationChange(ctx, foreign, 7, core.TransportTypeBestEffort)
	transportationOK(t, err)
	old, err := f.r.ReserveTransportationCheckpoint(ctx, transportationCapacityReviewRequests(source, 4095))
	transportationOK(t, err)
	old.Commit()
	defer old.Rollback()
	oldTicket := old.Changes()[0].Ticket
	state, owned := f.r.ExportState("fed"), f.owners.ExportState("fed")
	fresh := source
	fresh.Member.Incarnation++
	fresh.CallbackEpoch++
	if _, err := f.r.PrepareTransportationCheckpoint(ctx, transportationCapacityReviewRequests(fresh, 4096), state, owned, &transportationFOM{}); !errors.Is(err, core.ErrTransportationPendingLimit) {
		t.Fatal("replacement credit also credited foreign ticket", err)
	}
	if live, reserved := transportationCapacityReviewCounts(f.r); live != 4096 || reserved != 0 {
		t.Fatalf("failed oversized replacement leaked state: live=%d reserved=%d", live, reserved)
	}
	p, err := f.r.PrepareTransportationCheckpoint(ctx, transportationCapacityReviewRequests(fresh, 4095), state, owned, &transportationFOM{})
	transportationOK(t, err)
	defer p.Rollback()
	freshTicket := p.Changes()[0].Ticket
	if _, err := f.r.ActivateTransportationChange(ctx, fresh, freshTicket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
		t.Fatal("replacement lease installed before domain import", err)
	}
	commit, err := f.r.PrepareImportState("fed", state)
	transportationOK(t, err)
	commit()
	if live, reserved := transportationCapacityReviewCounts(f.r); live != 1 || reserved != 4095 {
		t.Fatalf("import did not preserve foreign ticket and lease: live=%d reserved=%d", live, reserved)
	}
	p.Commit()
	p.Commit()
	p.Release()
	if live, reserved := transportationCapacityReviewCounts(f.r); live != 4096 || reserved != 0 {
		t.Fatalf("replacement install exceeded capacity or leaked lease: live=%d reserved=%d", live, reserved)
	}
	if _, err := f.r.ActivateTransportationChange(ctx, source, oldTicket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
		t.Fatal("old source ticket survived domain import", err)
	}
	if got := f.r.InteractionTransportType("fed", 1, 7); got != core.TransportTypeReliable {
		t.Fatal("import or installation reapplied historic effect", got)
	}
	p.Rollback()
	p.Rollback()
	if live, reserved := transportationCapacityReviewCounts(f.r); live != 1 || reserved != 0 {
		t.Fatalf("installed rollback damaged foreign state or leaked capacity: live=%d reserved=%d", live, reserved)
	}
	if _, err := f.r.ActivateTransportationChange(ctx, fresh, freshTicket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
		t.Fatal("rolled-back replacement ticket remained usable", err)
	}
	if got := f.r.InteractionTransportType("fed", 1, 7); got != core.TransportTypeReliable {
		t.Fatal("lease rollback applied transportation effect", got)
	}
	_, err = f.r.ActivateTransportationChange(ctx, foreign, foreignChange.Ticket)
	transportationOK(t, err)
}
