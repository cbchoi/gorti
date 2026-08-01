// TASK-274 (M24 W1) — ReleaseAllOwnedBy + CancelPendingFor.

package m24spec

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/ownership"
)

// nopOutbox satisfies core.Outbox without recording.
type nopOutbox struct{}

func (nopOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	return nil
}

func newOwnershipMgr(t *testing.T) *ownership.Manager {
	t.Helper()
	mgr, err := ownership.New(ownership.Options{Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("ownership.New: %v", err)
	}
	return mgr
}

// TestSpec_M24_ReleaseAllOwnedBy_DropsRecords — owner federate's
// records are removed after ReleaseAllOwnedBy.
func TestSpec_M24_ReleaseAllOwnedBy_DropsRecords(t *testing.T) {
	mgr := newOwnershipMgr(t)
	ctx := context.Background()
	const fed core.FederationName = "fed"
	const owner = core.FederateHandle(1)
	const obj = core.ObjectHandle(10)
	mgr.RegisterInitialOwnership(fed, owner, obj, []core.AttributeHandle{1, 2, 3})

	// Pre-release: owner reads back.
	for _, attr := range []core.AttributeHandle{1, 2, 3} {
		if got, ok := mgr.QueryOwnership(fed, obj, attr); !ok || got != owner {
			t.Fatalf("OwnerOf(%d) = (%d, %v); want (%d, true)", attr, got, ok, owner)
		}
	}

	released := mgr.ReleaseAllOwnedBy(ctx, fed, owner)
	if len(released) != 3 {
		t.Errorf("ReleaseAllOwnedBy returned %d entries, want 3", len(released))
	}
	for _, attr := range []core.AttributeHandle{1, 2, 3} {
		if _, ok := mgr.QueryOwnership(fed, obj, attr); ok {
			t.Errorf("OwnerOf(%d) still has owner after release", attr)
		}
	}
}

// TestSpec_M24_ReleaseAllOwnedBy_OtherFederateUntouched — only the
// resigning federate's records drop; peers retain ownership.
func TestSpec_M24_ReleaseAllOwnedBy_OtherFederateUntouched(t *testing.T) {
	mgr := newOwnershipMgr(t)
	ctx := context.Background()
	const fed core.FederationName = "fed"
	mgr.RegisterInitialOwnership(fed, 1, 10, []core.AttributeHandle{1})
	mgr.RegisterInitialOwnership(fed, 2, 20, []core.AttributeHandle{1})

	mgr.ReleaseAllOwnedBy(ctx, fed, 1)

	if got, ok := mgr.QueryOwnership(fed, 20, 1); !ok || got != 2 {
		t.Errorf("OwnerOf(20,1) = (%d, %v); peer must be untouched", got, ok)
	}
	if _, ok := mgr.QueryOwnership(fed, 10, 1); ok {
		t.Errorf("OwnerOf(10,1) still has owner after release")
	}
}

// TestSpec_M24_ReleaseAllOwnedBy_Idempotent — calling twice returns
// empty the second time.
func TestSpec_M24_ReleaseAllOwnedBy_Idempotent(t *testing.T) {
	mgr := newOwnershipMgr(t)
	ctx := context.Background()
	const fed core.FederationName = "fed"
	mgr.RegisterInitialOwnership(fed, 1, 10, []core.AttributeHandle{1})

	if got := mgr.ReleaseAllOwnedBy(ctx, fed, 1); len(got) != 1 {
		t.Errorf("first release returned %d, want 1", len(got))
	}
	if got := mgr.ReleaseAllOwnedBy(ctx, fed, 1); len(got) != 0 {
		t.Errorf("second release returned %d, want 0 (idempotent)", len(got))
	}
}

// TestSpec_M24_CancelPendingFor — drops pending divests / acquires
// keyed by the federate.
func TestSpec_M24_CancelPendingFor_NoOpOnCleanState(t *testing.T) {
	mgr := newOwnershipMgr(t)
	ctx := context.Background()
	if got := mgr.CancelPendingFor(ctx, "fed", 1); got != 0 {
		t.Errorf("CancelPendingFor on clean state = %d, want 0", got)
	}
}
