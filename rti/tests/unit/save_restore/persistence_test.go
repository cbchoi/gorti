package save_restore_test

import (
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	"google.golang.org/protobuf/proto"
)

func TestLateProductionDecodeFailurePreservesEarlierManager(t *testing.T) {
	for _, initiallyEmpty := range []bool{false, true} {
		t.Run(map[bool]string{false: "populated", true: "empty"}[initiallyEmpty], func(t *testing.T) {
			s := newStore(t)
			o := &outbox{}
			syncManager, err := syncpkg.New(syncpkg.Options{Outbox: o})
			must(t, err)
			if !initiallyEmpty {
				syncManager.ImportState("fed", &syncpkg.ManagerState{Points: []syncpkg.PointState{{Label: liveStateValue, State: syncpkg.StateAnnounced}}})
			}
			before := syncManager.ExportState("fed")
			wire, err := proto.Marshal(&rtiv1.SyncManagerState{Points: []*rtiv1.SyncPointStateEntry{{Label: savedStateValue}}})
			must(t, err)
			seed(t, s, "bad", map[string][]byte{"a-sync": wire, "z-ownership": {0xff}})
			m, _ := manager(t, s, map[string]savepoint.ManagerSnapshotter{"a-sync": savebundle.Synchronization{Manager: syncManager}, "z-ownership": savebundle.Ownership{}})
			if err := m.RequestFederationRestore(context.Background(), "fed", "bad"); err == nil {
				t.Fatal("malformed later snapshot accepted")
			}
			if after := syncManager.ExportState("fed"); !reflect.DeepEqual(before, after) {
				t.Fatalf("earlier manager changed: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestSaveRestorePreservesTwoPhaseDivestiture(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	o := &outbox{}
	own, err := ownership.New(ownership.Options{Outbox: o})
	must(t, err)
	attrs := []core.AttributeHandle{1}
	own.RegisterInitialOwnership("fed", 1, 7, attrs)
	must(t, own.NegotiatedDivestTwoPhase(ctx, "fed", 1, 7, attrs, []byte("offer")))
	m, _ := manager(t, s, map[string]savepoint.ManagerSnapshotter{"ownership": savebundle.Ownership{Manager: own}})
	beginBoth(t, m, "two-phase")
	must(t, m.FederateSaveComplete(ctx, "fed", 1))
	must(t, m.FederateSaveComplete(ctx, "fed", 2))
	// Clear the pending protocol to prove restoration, not retained live state.
	own.OnFederationDestroyed("fed")
	must(t, m.RequestFederationRestore(ctx, "fed", "two-phase"))
	must(t, m.FederateRestoreComplete(ctx, "fed", 1))
	must(t, m.FederateRestoreComplete(ctx, "fed", 2))
	must(t, own.Acquire(ctx, "fed", 2, 7, attrs, nil))
	if owner, ok := own.QueryOwnership("fed", 7, 1); !ok || owner != 1 {
		t.Fatalf("transfer bypassed confirmation: owner=%d owned=%v", owner, ok)
	}
	must(t, own.ConfirmDivestiture(ctx, "fed", 1, 7, attrs))
	if owner, ok := own.QueryOwnership("fed", 7, 1); !ok || owner != 2 {
		t.Fatalf("confirmed transfer failed: owner=%d owned=%v", owner, ok)
	}
}

func TestLegacyOwnershipSnapshotRemainsReadable(t *testing.T) {
	// Fields 1-4 only: no new two_phase field. Omission retains legacy false.
	wire, err := proto.Marshal(&rtiv1.OwnershipManagerState{Owners: []*rtiv1.OwnershipRecordEntry{{ObjectHandle: 7, AttributeHandle: 1, OwnerFederate: 1}}, PendingDivests: []*rtiv1.PendingDivestEntry{{ObjectHandle: 7, AttributeHandle: 1, OwnerFederate: 1, Tag: []byte("legacy")}}})
	must(t, err)
	own, err := ownership.New(ownership.Options{Outbox: &outbox{}})
	must(t, err)
	must(t, (savebundle.Ownership{Manager: own}).Unmarshal("fed", wire))
	state := own.ExportState("fed")
	if len(state.PendingDivests) != 1 || state.PendingDivests[0].TwoPhase {
		t.Fatalf("legacy interpretation changed: %+v", state)
	}
	must(t, own.Acquire(context.Background(), "fed", 2, 7, []core.AttributeHandle{1}, nil))
	if owner, ok := own.QueryOwnership("fed", 7, 1); !ok || owner != 2 {
		t.Fatal("legacy one-phase continuation changed")
	}
}

func TestRestoreEmptyOwnershipClearsLiveStateButAbsentLegacyEntryPreserves(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	own, err := ownership.New(ownership.Options{Outbox: &outbox{}})
	must(t, err)
	m, _ := manager(t, s, map[string]savepoint.ManagerSnapshotter{"ownership": savebundle.Ownership{Manager: own}})
	beginBoth(t, m, "empty")
	must(t, m.FederateSaveComplete(ctx, "fed", 1))
	must(t, m.FederateSaveComplete(ctx, "fed", 2))
	manifest, err := m.LoadManifest("fed", "empty")
	must(t, err)
	if data, present := manifest.ManagerSnapshots["ownership"]; !present || len(data) != 0 {
		t.Fatalf("empty snapshot was omitted: %+v", manifest.ManagerSnapshots)
	}
	own.RegisterInitialOwnership("fed", 1, 7, []core.AttributeHandle{1})
	must(t, m.RequestFederationRestore(ctx, "fed", "empty"))
	if _, owned := own.QueryOwnership("fed", 7, 1); owned {
		t.Fatal("present empty snapshot retained live ownership")
	}
	must(t, m.FederateRestoreComplete(ctx, "fed", 1))
	must(t, m.FederateRestoreComplete(ctx, "fed", 2))
	seed(t, s, "legacy-absent", nil)
	own.RegisterInitialOwnership("fed", 1, 7, []core.AttributeHandle{1})
	must(t, m.RequestFederationRestore(ctx, "fed", "legacy-absent"))
	if owner, owned := own.QueryOwnership("fed", 7, 1); !owned || owner != 1 {
		t.Fatal("absent legacy entry unexpectedly cleared ownership")
	}
}
