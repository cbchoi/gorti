package ddm

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestDDMLifecycleInactiveSnapshotIsolation(t *testing.T) {
	m := newPermissiveManager(t)
	regions := ieee2010Regions(t, m, 2, 2)
	state := m.ExportState("additive")
	state.InactiveAssociations = []InactiveAssociationState{{Object: 9, Attribute: 1, Federate: 2, Regions: slices.Clone(regions)}}
	m.ImportState("additive", state)
	state.InactiveAssociations[0].Regions[0] = 999
	if len(m.PublisherRegionsFor("additive", 9, 1)) != 0 {
		t.Fatal("restore activated inactive association")
	}
	saved := m.ExportState("additive")
	restored := newPermissiveManager(t)
	restored.ImportState("additive", saved)
	if !reflect.DeepEqual(saved, restored.ExportState("additive")) {
		t.Fatal("inactive state did not round trip")
	}
	restored.OwnershipChanged("additive", 9, map[core.AttributeHandle]core.FederateHandle{1: 1}, map[core.AttributeHandle]core.FederateHandle{1: 2})
	if got := restored.PublisherRegionsFor("additive", 9, 1); !slices.Equal(got, regions) {
		t.Fatalf("restored activation=%v", got)
	}
	if !reflect.DeepEqual(saved, m.ExportState("additive")) {
		t.Fatal("restore/activation aliased source")
	}
	restored.OwnershipChanged("additive", 9, map[core.AttributeHandle]core.FederateHandle{1: 2}, map[core.AttributeHandle]core.FederateHandle{1: 0})
	restored.OwnershipChanged("additive", 9, map[core.AttributeHandle]core.FederateHandle{1: 0}, map[core.AttributeHandle]core.FederateHandle{1: 2})
	if len(restored.PublisherRegionsFor("additive", 9, 1)) != 0 {
		t.Fatal("reacquisition resurrected restored association")
	}
}

func TestDDMLifecycleAdmissionRollbackDoesNotUndoOwnership(t *testing.T) {
	m := newPermissiveManager(t)
	r1 := ieee2010Regions(t, m, 1, 2)
	r2 := ieee2010Regions(t, m, 2, 1)
	ctx := context.Background()
	if err := m.AssociateRegionsForUpdates(ctx, "additive", 1, 9, map[core.AttributeHandle][]RegionHandle{1: r1[:1]}); err != nil {
		t.Fatal(err)
	}
	state := m.ExportState("additive")
	state.InactiveAssociations = []InactiveAssociationState{{Object: 9, Attribute: 1, Federate: 2, Regions: r2}}
	m.ImportState("additive", state)
	failure := errors.New("admission failed")
	m.SetUpdateRateHooks(func(context.Context, core.FederationName, bool) error {
		m.OwnershipChanged("additive", 9, map[core.AttributeHandle]core.FederateHandle{1: 1}, map[core.AttributeHandle]core.FederateHandle{1: 2})
		return failure
	}, nil, nil)
	if err := m.AssociateRegionsForUpdates(ctx, "additive", 1, 9, map[core.AttributeHandle][]RegionHandle{1: r1[1:]}); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if got := m.PublisherRegionsFor("additive", 9, 1); !slices.Equal(got, r2) {
		t.Fatalf("admission rollback reverted ownership routing: %v", got)
	}
	if len(m.ExportState("additive").InactiveAssociations) != 0 {
		t.Fatal("rollback resurrected consumed inactive association")
	}
}

func TestDDMLifecycleOwnershipAfterRollbackBeforeFinish(t *testing.T) {
	m := newPermissiveManager(t)
	r1 := ieee2010Regions(t, m, 1, 2)
	r2 := ieee2010Regions(t, m, 2, 1)
	if err := m.AssociateRegionsForUpdates(context.Background(), "additive", 1, 9, map[core.AttributeHandle][]RegionHandle{1: r1[:1]}); err != nil {
		t.Fatal(err)
	}
	state := m.ExportState("additive")
	state.InactiveAssociations = []InactiveAssociationState{{Object: 9, Attribute: 1, Federate: 2, Regions: r2}}
	m.ImportState("additive", state)

	m.mu.Lock()
	st := m.fed["additive"]
	rollback := m.associationRateRollbackLocked("additive", st, 9)
	st.objPubs[9][1] = slices.Clone(r1)
	m.mu.Unlock()

	// Schedule ownership after rollback unlocks but before finishRateMutation's deferred cleanup.
	rollback()
	m.OwnershipChanged("additive", 9, map[core.AttributeHandle]core.FederateHandle{1: 1}, map[core.AttributeHandle]core.FederateHandle{1: 2})
	if got := m.PublisherRegionsFor("additive", 9, 1); !slices.Equal(got, r2) {
		t.Fatalf("post-rollback ownership activation lost regions: %v, want %v", got, r2)
	}
	if len(m.ExportState("additive").InactiveAssociations) != 0 {
		t.Fatal("post-rollback activation left inactive association")
	}
	m.mu.Lock()
	projection := m.associationRollback
	m.mu.Unlock()
	if projection != nil {
		t.Fatal("rollback retained its projection after releasing the state lock")
	}
}
