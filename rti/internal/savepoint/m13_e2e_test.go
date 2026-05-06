package savepoint

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/ddm"
	"github.com/cbchoi/gorti/rti/internal/mom"
	"github.com/cbchoi/gorti/rti/internal/ownership"
	syncpkg "github.com/cbchoi/gorti/rti/internal/sync"
)

// TestM13_StructuredSaveRestore_RoundTrip is the integration test
// for M13 (docs/srs.md §10.4): a save bundle written by sync /
// ownership / mom / ddm Marshalers, restored by their Unmarshalers
// via the savepoint Manager, must reconstruct the per-manager state
// without sole reliance on event-log replay.
//
// Save → kill mgr → restart mgr → restore → verify per-manager state
// matches pre-save (sync points still pending, ownership still in
// transfer, MOM counters preserved).
func TestM13_StructuredSaveRestore_RoundTrip(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	ctx := context.Background()
	const fed = core.FederationName("demo")

	// --- pre-save: build state across all four managers -----------------
	syncA := mustSyncMgr(t)
	ownA := mustOwnMgr(t)
	momA := mustMomMgr(t)
	ddmA, _, _ := mustDDMMgr(t)

	if err := syncA.Register(ctx, fed, "phase1", []byte("sync-tag"),
		[]core.FederateHandle{1, 2, 3}); err != nil {
		t.Fatalf("Register sync: %v", err)
	}
	if err := syncA.Achieve(ctx, fed, 1, "phase1"); err != nil {
		t.Fatalf("Achieve: %v", err)
	}
	ownA.RegisterInitialOwnership(fed, 1, 100, []core.AttributeHandle{1, 2, 3})
	if err := ownA.NegotiatedDivest(ctx, fed, 1, 100,
		[]core.AttributeHandle{1}, []byte("tag")); err != nil {
		t.Fatalf("NegotiatedDivest: %v", err)
	}
	_ = momA.FederationCreated(ctx, fed, []core.FOMModule{{Path: "fom.xml"}})
	_ = momA.FederateJoined(ctx, fed, 1, "alpha", "Sensor")
	_ = momA.FederateJoined(ctx, fed, 2, "beta", "Actuator")
	momA.IncrementUpdatesSent(fed, 1)
	momA.IncrementUpdatesSent(fed, 1)
	momA.IncrementInteractionsReceived(fed, 2)
	rs, _ := ddmA.LookupRoutingSpace(fed, "default")
	dim, _ := ddmA.LookupDimension(fed, rs, "x")
	rh, err := ddmA.CreateRegion(ctx, fed, 1, rs, []ddm.DimensionHandle{dim})
	if err != nil {
		t.Fatalf("CreateRegion: %v", err)
	}

	// --- save -----------------------------------------------------------
	mgrA, err := New(Options{
		Outbox:      &fakeOutbox{},
		BundleStore: store,
		Members:     func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1, 2, 3} },
		ManagerSnapshots: map[string]ManagerSnapshotter{
			ManagerSnapshotKeySync:      syncA,
			ManagerSnapshotKeyOwnership: ownA,
			ManagerSnapshotKeyMOM:       momA,
			ManagerSnapshotKeyDDM:       ddmA,
		},
	})
	if err != nil {
		t.Fatalf("New mgrA: %v", err)
	}
	if err := mgrA.RequestFederationSave(ctx, fed, "checkpoint", nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	for _, h := range []core.FederateHandle{1, 2, 3} {
		if err := mgrA.FederateSaveComplete(ctx, fed, h); err != nil {
			t.Fatalf("FederateSaveComplete %d: %v", h, err)
		}
	}
	if got := mgrA.QuerySaveState(fed, "checkpoint"); got != StateSaved {
		t.Fatalf("QuerySaveState pre-restore = %v, want StateSaved", got)
	}

	// --- kill mgr → restart mgr -----------------------------------------
	syncB := mustSyncMgr(t)
	ownB := mustOwnMgr(t)
	momB := mustMomMgr(t)
	ddmB, _, _ := mustDDMMgr(t)
	// Pre-populate dst routing-space tables exactly like src; production
	// CreateFederation would have done this via the FOM hook.
	_, _ = ddmB.LookupRoutingSpace(fed, "default")
	_, _ = ddmB.LookupDimension(fed, rs, "x")

	mgrB, err := New(Options{
		Outbox:      &fakeOutbox{},
		BundleStore: store,
		Members:     func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1, 2, 3} },
		ManagerSnapshots: map[string]ManagerSnapshotter{
			ManagerSnapshotKeySync:      syncB,
			ManagerSnapshotKeyOwnership: ownB,
			ManagerSnapshotKeyMOM:       momB,
			ManagerSnapshotKeyDDM:       ddmB,
		},
	})
	if err != nil {
		t.Fatalf("New mgrB: %v", err)
	}

	// --- restore --------------------------------------------------------
	if err := mgrB.RequestFederationRestore(ctx, fed, "checkpoint"); err != nil {
		t.Fatalf("RequestFederationRestore: %v", err)
	}

	// --- verify per-manager state matches pre-save ----------------------

	// Sync points still pending: phase1 announced with achieved=[1].
	syncSnapA := syncA.Snapshot(fed)
	syncSnapB := syncB.Snapshot(fed)
	if len(syncSnapA) != len(syncSnapB) {
		t.Fatalf("sync snapshot len mismatch: src=%d dst=%d", len(syncSnapA), len(syncSnapB))
	}
	if syncSnapA[0].State != syncSnapB[0].State {
		t.Errorf("sync state mismatch: src=%v dst=%v", syncSnapA[0].State, syncSnapB[0].State)
	}
	if len(syncSnapB[0].AchievedHandles) != 1 || syncSnapB[0].AchievedHandles[0] != 1 {
		t.Errorf("achieved handles after restore = %v, want [1]", syncSnapB[0].AchievedHandles)
	}

	// Ownership still in transfer: pendingDivests = 1 over (100, 1).
	ownSnapB := ownB.Snapshot(fed)
	if ownSnapB.PendingDivestsCount != 1 {
		t.Errorf("PendingDivestsCount after restore = %d, want 1",
			ownSnapB.PendingDivestsCount)
	}
	if ownSnapB.OwnedAttributesCount != 3 {
		t.Errorf("OwnedAttributesCount after restore = %d, want 3",
			ownSnapB.OwnedAttributesCount)
	}

	// MOM counters preserved.
	mAttrs, _ := momB.QueryFederate(fed, 1)
	if mAttrs.UpdatesSent != 2 {
		t.Errorf("federate 1 UpdatesSent after restore = %d, want 2", mAttrs.UpdatesSent)
	}
	if mAttrs.Type != "Sensor" {
		t.Errorf("federate 1 Type after restore = %q, want Sensor", mAttrs.Type)
	}
	mAttrs2, _ := momB.QueryFederate(fed, 2)
	if mAttrs2.InteractionsReceived != 1 {
		t.Errorf("federate 2 InteractionsReceived after restore = %d, want 1",
			mAttrs2.InteractionsReceived)
	}

	// DDM region preserved.
	ddmSnapB := ddmB.Snapshot(fed)
	if ddmSnapB.RegionCount != 1 {
		t.Errorf("RegionCount after restore = %d, want 1", ddmSnapB.RegionCount)
	}
	// Region handle should still resolve.
	if _, ok := ddmB.QueryBounds(fed, rh, dim); !ok {
		t.Errorf("QueryBounds(%d, %d) after restore: not found", rh, dim)
	}
}

// --- test fixtures (local to this E2E test) ---------------------------------

func mustSyncMgr(t *testing.T) *syncpkg.Manager {
	t.Helper()
	m, err := syncpkg.New(syncpkg.Options{Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("syncpkg.New: %v", err)
	}
	return m
}

func mustOwnMgr(t *testing.T) *ownership.Manager {
	t.Helper()
	m, err := ownership.New(ownership.Options{Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("ownership.New: %v", err)
	}
	return m
}

func mustMomMgr(t *testing.T) *mom.Manager {
	t.Helper()
	m, err := mom.New(mom.Options{Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("mom.New: %v", err)
	}
	return m
}

func mustDDMMgr(t *testing.T) (*ddm.Manager, ddm.RoutingSpaceHandle, ddm.DimensionHandle) {
	t.Helper()
	m, err := ddm.New(ddm.Options{Outbox: nopOutbox{}, FOMs: permissiveFOMRepo{}})
	if err != nil {
		t.Fatalf("ddm.New: %v", err)
	}
	return m, 0, 0
}

type nopOutbox struct{}

func (nopOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	return nil
}

type permissiveFOMRepo struct{}

func (permissiveFOMRepo) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return permissiveFOMHandle{}, nil
}
func (permissiveFOMRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return permissiveFOMHandle{}, nil
}

type permissiveFOMHandle struct{}

func (permissiveFOMHandle) IsValid() bool { return true }
func (permissiveFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}
