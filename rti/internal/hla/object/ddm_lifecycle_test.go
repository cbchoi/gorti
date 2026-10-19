package object

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
)

func TestDDMLifecyclePreacquisitionActivationAndLoss(t *testing.T) {
	f := newAssociationValidationFixture(t)
	ctx := context.Background()
	groups := []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{1}, Regions: []ddm.RegionHandle{f.foreign}}}
	if err := f.regions.AssociateRegionGroupsForUpdates(ctx, "fed", 2, f.object, groups); err != nil {
		t.Fatalf("preacquisition association rejected: %v", err)
	}
	if got := f.regions.PublisherRegionsFor("fed", f.object, 1); !slices.Equal(got, []ddm.RegionHandle{f.first}) {
		t.Fatalf("inactive association changed current owner's routing: %v", got)
	}
	f.transferFirst(t)
	if got := f.regions.PublisherRegionsFor("fed", f.object, 1); !slices.Equal(got, []ddm.RegionHandle{f.foreign}) {
		t.Fatalf("acquisition did not activate acquiring federate's regions: %v", got)
	}
	if err := f.regions.DeleteRegion(ctx, "fed", 2, f.foreign); !errors.Is(err, core.ErrRegionInUse) {
		t.Fatalf("active region deletion: %v", err)
	}
	if err := f.regions.DeleteRegion(ctx, "fed", 1, f.first); err != nil {
		t.Fatalf("former owner's association survived ownership loss: %v", err)
	}
	if err := f.owners.UnconditionalDivest(ctx, "fed", 2, f.object, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if err := f.owners.Acquire(ctx, "fed", 1, f.object, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.regions.PublisherRegionsFor("fed", f.object, 1); len(got) != 0 {
		t.Fatalf("reacquisition resurrected an old association: %v", got)
	}
	if got := f.regions.PublisherRegionsFor("fed", f.object, 2); !slices.Equal(got, []ddm.RegionHandle{f.second}) {
		t.Fatalf("ownership transition changed unrelated attribute: %v", got)
	}
}

func TestDDMLifecycleInactiveRemovalAndValidation(t *testing.T) {
	f := newAssociationValidationFixture(t)
	ctx := context.Background()
	groups := []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{1}, Regions: []ddm.RegionHandle{f.foreign}}}
	if err := f.regions.AssociateRegionGroupsForUpdates(ctx, "fed", 2, f.object, groups); err != nil {
		t.Fatal(err)
	}
	before := f.regions.ExportState("fed")
	for _, bad := range []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{999}}, {Regions: []ddm.RegionHandle{f.first}}, {Regions: []ddm.RegionHandle{999}}} {
		for _, call := range []func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeRegionGroup) error{f.regions.AssociateRegionGroupsForUpdates, f.regions.UnassociateRegionGroupsForUpdates} {
			if err := call(ctx, "fed", 2, f.object, append(slices.Clone(groups), bad)); err == nil {
				t.Fatal("invalid later pair accepted")
			}
			if !reflect.DeepEqual(before, f.regions.ExportState("fed")) {
				t.Fatal("invalid later pair partially changed state")
			}
		}
	}
	if err := f.regions.UnassociateRegionGroupsForUpdates(ctx, "fed", 2, f.object, groups); err != nil {
		t.Fatal(err)
	}
	if got := f.regions.PublisherRegionsFor("fed", f.object, 1); !slices.Equal(got, []ddm.RegionHandle{f.first}) {
		t.Fatalf("nonowner removal changed active routing: %v", got)
	}
	f.transferFirst(t)
	if got := f.regions.PublisherRegionsFor("fed", f.object, 1); len(got) != 0 {
		t.Fatalf("removed inactive association activated: %v", got)
	}
}

func TestDDMLifecycleInactiveRegionDeletionAndResignation(t *testing.T) {
	for _, resign := range []bool{false, true} {
		t.Run(map[bool]string{false: "delete-region", true: "resign"}[resign], func(t *testing.T) {
			f := newAssociationValidationFixture(t)
			ctx := context.Background()
			if err := f.regions.AssociateRegionsForUpdates(ctx, "fed", 2, f.object, map[core.AttributeHandle][]ddm.RegionHandle{1: {f.foreign}}); err != nil {
				t.Fatal(err)
			}
			if resign {
				f.regions.OnFederateResign("fed", 2)
			} else if err := f.regions.DeleteRegion(ctx, "fed", 2, f.foreign); err != nil {
				t.Fatalf("inactive region counted as in use: %v", err)
			}
			for _, row := range f.regions.ExportState("fed").ObjectPublications {
				if slices.Contains(row.Regions, f.foreign) {
					t.Fatal("removed region remains associated")
				}
			}
			if got := f.regions.PublisherRegionsFor("fed", f.object, 1); !slices.Equal(got, []ddm.RegionHandle{f.first}) {
				t.Fatalf("cleanup changed another member's routing: %v", got)
			}
		})
	}
}

func TestDDMLifecycleKnownAncestorPinsAndStoresCanonical(t *testing.T) {
	r, decl, owners, obj := knownOwnershipPreparationFixture(t)
	ctx := context.Background()
	m, region := roRegions(t, r)
	pub := region(2, 0, 10)
	if err := decl.UnpublishObjectClassAttributes(ctx, "fed", 2, 9, []core.AttributeHandle{101}); err != nil {
		t.Fatal(err)
	}
	input := []core.AttributeHandle{101}
	cls, actual, snapshot, release, err := r.PrepareKnownRegionAssociation(ctx, "fed", 2, obj, input)
	if err != nil {
		t.Fatal(err)
	}
	if cls != 1 || !slices.Equal(actual, []core.AttributeHandle{1}) || snapshot[1] != 1 || !slices.Equal(input, []core.AttributeHandle{101}) {
		release()
		t.Fatalf("prepared class=%d attrs=%v owners=%v", cls, actual, snapshot)
	}
	if r.stateFor("fed").mu.TryLock() {
		r.stateFor("fed").mu.Unlock()
		release()
		t.Fatal("object knowledge was not pinned")
	}
	release()
	release()
	if err := m.AssociateRegionGroupsForUpdates(ctx, "fed", 2, obj, []core.AttributeRegionGroup{{Attributes: input, Regions: []ddm.RegionHandle{pub}}}); err != nil {
		t.Fatalf("nonpublished ancestor association rejected: %v", err)
	}
	state := m.ExportState("fed")
	if len(state.InactiveAssociations) != 1 || state.InactiveAssociations[0].Attribute != 1 || state.InactiveAssociations[0].Federate != 2 || len(m.PublisherRegionsFor("fed", obj, 1)) != 0 {
		t.Fatalf("wrong inactive coordinates: %+v", state.InactiveAssociations)
	}
	before := m.ExportState("fed")
	if err := m.AssociateRegionGroupsForUpdates(ctx, "fed", 2, obj, []core.AttributeRegionGroup{{Attributes: []core.AttributeHandle{101, 2}, Regions: []ddm.RegionHandle{pub}}}); !errors.Is(err, core.ErrAttributeNotFound) {
		t.Fatalf("child-only attribute visible at parent: %v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("later unavailable known attribute partially committed")
	}
	if err := decl.PublishObjectClassAttributes(ctx, "fed", 2, 9, []core.AttributeHandle{101}); err != nil {
		t.Fatal(err)
	}
	if err := owners.UnconditionalDivest(ctx, "fed", 1, obj, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if err := owners.Acquire(ctx, "fed", 2, obj, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	if got := m.PublisherRegionsFor("fed", obj, 1); !slices.Equal(got, []ddm.RegionHandle{pub}) {
		t.Fatalf("ancestor association did not activate canonically: %v", got)
	}
	if len(m.PublisherRegionsFor("fed", obj, 101)) != 0 || !owners.IsOwnedBy("fed", 1, obj, 2) {
		t.Fatal("canonical mapping changed an unrelated attribute")
	}
}

func TestDDMLifecycleLocalDeleteAndLegacyClearAreCallerScoped(t *testing.T) {
	for _, mode := range []string{"local-delete", "legacy-clear", "legacy-replace-clear"} {
		t.Run(mode, func(t *testing.T) {
			f := newAssociationValidationFixture(t)
			ctx := context.Background()
			if err := f.regions.AssociateRegionsForUpdates(ctx, "fed", 2, f.object, map[core.AttributeHandle][]ddm.RegionHandle{1: {f.foreign}}); err != nil {
				t.Fatal(err)
			}
			var err error
			switch mode {
			case "local-delete":
				err = f.registry.LocalDelete(ctx, "fed", 2, f.object)
			case "legacy-clear":
				err = f.regions.UnassociateRegionsForUpdates(ctx, "fed", 2, f.object, nil)
			case "legacy-replace-clear":
				err = f.regions.AssociateRegionsWithObjectInstance(ctx, "fed", 2, f.object, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(f.regions.ExportState("fed").InactiveAssociations) != 0 {
				t.Fatal("caller associations survived cleanup")
			}
			if got := f.regions.PublisherRegionsFor("fed", f.object, 1); !slices.Equal(got, []ddm.RegionHandle{f.first}) {
				t.Fatalf("caller cleanup removed another owner's associations: %v", got)
			}
		})
	}
}

func TestDDMLifecycleResignTransferAndAbort(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "abort", true: testCommit}[commit], func(t *testing.T) {
			f := newAssociationValidationFixture(t)
			ctx := context.Background()
			if err := f.regions.AssociateRegionsForUpdates(ctx, "fed", 2, f.object, map[core.AttributeHandle][]ddm.RegionHandle{1: {f.foreign}}); err != nil {
				t.Fatal(err)
			}
			if err := f.owners.Acquire(ctx, "fed", 2, f.object, []core.AttributeHandle{1}, nil); err != nil {
				t.Fatal(err)
			}
			before := f.regions.ExportState("fed")
			objectPlan := f.registry.PrepareResignDeletions(ctx, "fed", 1, false)
			ownerPlan, err := f.owners.PrepareResign("fed", 1, core.ResignActionUnconditionallyDivestAttributes, objectPlan.Objects())
			if err != nil {
				objectPlan.Release()
				t.Fatal(err)
			}
			if !commit {
				ownerPlan.Release()
				objectPlan.Release()
				if !reflect.DeepEqual(before, f.regions.ExportState("fed")) {
					t.Fatal("aborted resignation changed associations")
				}
				return
			}
			ownerPlan.Commit()
			objectPlan.CommitState()
			if got := f.regions.PublisherRegionsFor("fed", f.object, 1); !slices.Equal(got, []ddm.RegionHandle{f.foreign}) {
				t.Fatalf("resign transfer not activated before callbacks: %v", got)
			}
			if len(f.regions.PublisherRegionsFor("fed", f.object, 2)) != 0 {
				t.Fatal("resigned owner's untransferred associations survived")
			}
			f.regions.OnFederateResign("fed", 1)
			if _, exists := f.regions.QueryBounds("fed", f.first, 1); exists {
				t.Fatal("resignation left departed member's region")
			}
		})
	}
}

func TestDDMLifecycleConcurrentOwnershipSurvivesAdmissionRollback(t *testing.T) {
	for _, reacquire := range []bool{false, true} {
		t.Run(map[bool]string{false: "transfer", true: "transfer-away-and-back"}[reacquire], func(t *testing.T) {
			f := newAssociationValidationFixture(t)
			ctx := context.Background()
			if err := f.regions.AssociateRegionsForUpdates(ctx, "fed", 2, f.object, map[core.AttributeHandle][]ddm.RegionHandle{1: {f.foreign}}); err != nil {
				t.Fatal(err)
			}
			entered, admission := make(chan struct{}), make(chan struct{})
			finishAdmission := sync.OnceFunc(func() { close(admission) })
			defer finishAdmission()
			failure := errors.New("rate admission failed")
			f.regions.SetUpdateRateHooks(func(context.Context, core.FederationName, bool) error { close(entered); <-admission; return failure }, nil, nil)
			changed := make(chan error, 1)
			go func() {
				changed <- f.regions.AssociateRegionsForUpdates(ctx, "fed", 1, f.object, map[core.AttributeHandle][]ddm.RegionHandle{1: {f.spare}})
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("association admission not reached")
			}
			transferred := make(chan error, 1)
			go func() {
				if err := f.owners.UnconditionalDivest(ctx, "fed", 1, f.object, []core.AttributeHandle{1}); err != nil {
					transferred <- err
					return
				}
				if err := f.owners.Acquire(ctx, "fed", 2, f.object, []core.AttributeHandle{1}, nil); err != nil {
					transferred <- err
					return
				}
				if reacquire {
					if err := f.owners.UnconditionalDivest(ctx, "fed", 2, f.object, []core.AttributeHandle{1}); err != nil {
						transferred <- err
						return
					}
					if err := f.owners.Acquire(ctx, "fed", 1, f.object, []core.AttributeHandle{1}, nil); err != nil {
						transferred <- err
						return
					}
				}
				transferred <- nil
			}()
			select {
			case err := <-transferred:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("ownership blocked by rate admission")
			}
			finishAdmission()
			select {
			case err := <-changed:
				if !errors.Is(err, failure) {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("association rollback did not finish")
			}
			wantOwner, wantRegions := core.FederateHandle(2), []ddm.RegionHandle{f.foreign}
			if reacquire {
				wantOwner, wantRegions = 1, nil
			}
			if !f.owners.IsOwnedBy("fed", wantOwner, f.object, 1) || !slices.Equal(f.regions.PublisherRegionsFor("fed", f.object, 1), wantRegions) {
				t.Fatalf("rollback reversed ownership routing: owner=%d regions=%v", wantOwner, f.regions.PublisherRegionsFor("fed", f.object, 1))
			}
			if len(f.regions.ExportState("fed").InactiveAssociations) != 0 {
				t.Fatal("rollback resurrected consumed inactive rows")
			}
		})
	}
}
