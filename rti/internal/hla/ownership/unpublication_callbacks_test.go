package ownership

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestUnpublicationCallbackGuardPendingRequestAndTerminalReceipt(t *testing.T) {
	m, _, scope := strictTerminalManager(t, Options{})
	ctx := context.Background()
	if err := m.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	selected := map[core.ObjectHandle][]core.AttributeHandle{100: {1}}
	if release, err := m.PrepareUnpublication(ctx, "fed", 2, selected); !errors.Is(err, core.ErrOwnershipAcquisitionPending) || release != nil {
		t.Fatal("pending request allowed unpublication", err)
	}
	if err := m.CancelAcquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if release, err := m.PrepareUnpublication(ctx, "fed", 2, selected); !errors.Is(err, core.ErrOwnershipAcquisitionPending) || release != nil {
		t.Fatal("cancel RPC/queue admission allowed unpublication before callback", err)
	}
	id := m.ExportState("fed").PendingCallbacks[0].CallbackID
	receipt, finish, err := m.BeginOwnershipCallbackDelivery(ctx, scope, id)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if err := m.ReportOwnershipCallbackInvocation(ctx, scope, receipt); err != nil {
		t.Fatal(err)
	}
	release, err := m.PrepareUnpublication(ctx, "fed", 2, selected)
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()
}

func TestUnpublicationCallbackGuardPinsAgainstAcquisitionMutation(t *testing.T) {
	m, _, _ := strictTerminalManager(t, Options{})
	release, err := m.PrepareUnpublication(context.Background(), "fed", 2, map[core.ObjectHandle][]core.AttributeHandle{100: {1}})
	if err != nil {
		t.Fatal(err)
	}
	if m.mu.TryLock() {
		m.mu.Unlock()
		release()
		t.Fatal("unpublication snapshot did not pin ownership mutation")
	}
	if !m.mu.TryRLock() {
		release()
		t.Fatal("read-only lease unnecessarily excludes readers")
	}
	m.mu.RUnlock()
	release()
	release()
	if !m.mu.TryLock() {
		t.Fatal("idempotent release did not release ownership mutation")
	}
	m.mu.Unlock()
	if err := m.Acquire(context.Background(), "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PrepareUnpublication(context.Background(), "fed", 2, map[core.ObjectHandle][]core.AttributeHandle{100: {1}}); !errors.Is(err, core.ErrOwnershipAcquisitionPending) {
		t.Fatal("acquisition after declaration lease not observed", err)
	}
}

func TestUnpublicationCallbackGuardSelectsCanonicalAttributes(t *testing.T) {
	m, _, scope := strictTerminalManager(t, Options{})
	unavailableTerminal(t, m, scope, 1)
	for _, selected := range []map[core.ObjectHandle][]core.AttributeHandle{{100: {2}}, {101: {1}}, {100: {}}, {}} {
		release, err := m.PrepareUnpublication(context.Background(), "fed", 2, selected)
		if err != nil {
			t.Fatal("unrelated selection blocked", selected, err)
		}
		release()
	}
	if _, err := m.PrepareUnpublication(context.Background(), "fed", 2, map[core.ObjectHandle][]core.AttributeHandle{100: {2, 1}}); !errors.Is(err, core.ErrOwnershipAcquisitionPending) {
		t.Fatal("later selected pending attribute allowed", err)
	}
	if release, err := m.PrepareUnpublication(context.Background(), "fed", 2, map[core.ObjectHandle][]core.AttributeHandle{100: {2, 0}}); err == nil || release != nil {
		t.Fatal("invalid later attribute accepted")
	}
	if !m.mu.TryLock() {
		t.Fatal("failed validation leaked lease")
	}
	m.mu.Unlock()
}

func TestUnpublicationGuardDoesNotOverblockAcquiredOrRTIOwnedAttribute(t *testing.T) {
	m, _, scope := strictTerminalManager(t, Options{})
	if err := m.Acquire(context.Background(), "fed", 2, 101, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	if !m.HasAcquisitionObligation("fed", 2, 101, nil) {
		t.Fatal("test requires uninvoked acquired callback")
	}
	release, err := m.PrepareUnpublication(context.Background(), "fed", 2, map[core.ObjectHandle][]core.AttributeHandle{101: {1}})
	if err != nil {
		t.Fatal("5.3.3f excludes attributes already owned by publisher", err)
	}
	release()
	m.RegisterInitialOwnership("fed", ^core.FederateHandle(0), 102, []core.AttributeHandle{1})
	if err := m.AcquireIfAvailable(context.Background(), scope.Federation, 2, 102, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	release, err = m.PrepareUnpublication(context.Background(), "fed", 2, map[core.ObjectHandle][]core.AttributeHandle{102: {1}})
	if err != nil {
		t.Fatal("5.3.3f requires other joined-federate ownership, not RTI ownership", err)
	}
	release()
}

func TestUnpublicationCommitOwnedAttributesAndRetainedAcquisitionOutcome(t *testing.T) {
	m, _, scope := strictTerminalManager(t, Options{})
	ctx := context.Background()
	if err := m.Acquire(ctx, "fed", 2, 101, []core.AttributeHandle{1, 2}, nil); err != nil {
		t.Fatal(err)
	}
	plan, err := m.PrepareUnpublicationCommit(ctx, "fed", 2, map[core.ObjectHandle][]core.AttributeHandle{101: {1}})
	if err != nil {
		t.Fatal(err)
	}
	if m.mu.TryRLock() {
		m.mu.RUnlock()
		plan.Release()
		t.Fatal("prepare did not hold mutation lease through declaration commit")
	}
	// Parent declaration commit belongs here, before releasing object pin.
	plan.Commit()
	plan.Commit()
	plan.Release()
	if m.IsOwnedBy("fed", 2, 101, 1) || !m.IsOwnedBy("fed", 2, 101, 2) {
		t.Fatal("unpublication did not relinquish exactly selected owned attribute")
	}
	if !m.HasAcquisitionObligation("fed", 2, 101, []core.AttributeHandle{1}) {
		t.Fatal("unpublication silently deleted postcommit terminal result")
	}
	if err := plan.Dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	if err := plan.Dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	id := m.ExportState("fed").PendingCallbacks[0].CallbackID
	receipt, finish, err := m.BeginOwnershipCallbackDelivery(ctx, scope, id)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if err := m.ReportOwnershipCallbackInvocation(ctx, scope, receipt); err != nil {
		t.Fatal(err)
	}
}

func TestUnpublicationCommitReleaseAndEmptySelectionAtomic(t *testing.T) {
	m, _, _ := strictTerminalManager(t, Options{})
	before := m.ExportState("fed")
	plan, err := m.PrepareUnpublicationCommit(context.Background(), "fed", 1, map[core.ObjectHandle][]core.AttributeHandle{100: {1}})
	if err != nil {
		t.Fatal(err)
	}
	plan.Release()
	plan.Release()
	plan.Commit()
	if err := plan.Dispatch(context.Background()); !errors.Is(err, core.ErrOwnershipNotInTransfer) {
		t.Fatal("aborted plan dispatched", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("aborted declaration changed ownership")
	}
	plan, err = m.PrepareUnpublicationCommit(context.Background(), "fed", 1, map[core.ObjectHandle][]core.AttributeHandle{100: {}})
	if err != nil {
		t.Fatal(err)
	}
	plan.Commit()
	if err := plan.Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("empty selected attrs unpublished whole object")
	}
	if _, err := m.PrepareUnpublicationCommit(context.Background(), "fed", 1, map[core.ObjectHandle][]core.AttributeHandle{100: {1}, 101: {0}}); err == nil {
		t.Fatal("invalid later object selection accepted")
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("invalid later selection partially divested")
	}
}

func TestUnpublicationCommitQueuedAcquirerAtomicFanoutFailedDispatch(t *testing.T) {
	out := &projectedTerminalOutbox{}
	m, err := New(Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 7}
	if err := m.EnableOwnershipCallbackReceipts(ctx, scope); err != nil {
		t.Fatal(err)
	}
	for _, obj := range []core.ObjectHandle{100, 200} {
		m.RegisterInitialOwnership("fed", 1, obj, []core.AttributeHandle{1})
		if err := m.Acquire(ctx, "fed", 2, obj, []core.AttributeHandle{1}, nil); err != nil {
			t.Fatal(err)
		}
	}
	out.beforeReserve = func() {
		if !m.mu.TryLock() {
			t.Fatal("unpublication dispatch retained ownership mutation lease")
		}
		m.mu.Unlock()
	}
	out.reserveFailure = core.ErrFederateOverflow
	plan, err := m.PrepareUnpublicationCommit(ctx, "fed", 1, map[core.ObjectHandle][]core.AttributeHandle{100: {1}, 200: {1}})
	if err != nil {
		t.Fatal(err)
	}
	plan.Commit()
	if m.IsOwnedBy("fed", 1, 100, 1) || m.IsOwnedBy("fed", 1, 200, 1) {
		t.Fatal("unpublication left caller owning selected attributes")
	}
	if err := plan.Dispatch(ctx); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatal("failed atomic fanout not reported", err)
	}
	state := m.ExportState("fed")
	if len(state.PendingCallbacks) != 2 || len(state.PendingAcquires) != 0 {
		t.Fatal("failed dispatch lost committed acquisition outcomes", state)
	}
	if !m.HasAcquisitionObligation("fed", 2, 100, nil) || !m.HasAcquisitionObligation("fed", 2, 200, nil) {
		t.Fatal("one fanout recipient/object lifetime lost")
	}
	out.reserveFailure = nil
	if err := m.RetryOwnershipCallbacks(ctx, scope); err != nil {
		t.Fatal(err)
	}
}

func TestUnpublicationCommitCapacityFailureDoesNotDivest(t *testing.T) {
	m, _, scope := strictTerminalManager(t, Options{CallbackWindow: 1})
	ctx := context.Background()
	unavailableTerminal(t, m, scope, 2)
	if err := m.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState("fed")
	if plan, err := m.PrepareUnpublicationCommit(ctx, "fed", 1, map[core.ObjectHandle][]core.AttributeHandle{100: {1}}); !errors.Is(err, core.ErrOwnershipCallbackCapacity) || plan != nil {
		t.Fatal("terminal capacity failed late after declaration preparation", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("failed callback capacity changed declaration-side ownership")
	}
}
