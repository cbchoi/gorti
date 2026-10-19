package server

import (
	"context"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

// updateRateBridge serializes post-commit object/owner notifications with DDM
// projections. It never holds a service lock while calling another service.
type updateRateBridge struct {
	mu           sync.Mutex
	foms         core.FOMRepository
	declarations *declaration.Manager
	objects      updateRateObjects
	regions      *ddm.Manager
	owners       *ownership.Manager
}

type updateRateObjects interface {
	RefreshDiscoveries(context.Context, core.FederationName)
	RegisterWithRegions(context.Context, core.FederationName, core.FederateHandle, core.ObjectClassHandle, string, map[core.AttributeHandle][]core.DDMRegionHandleCore) (core.ObjectHandle, string, error)
}

func newUpdateRateBridge(foms core.FOMRepository, declarations *declaration.Manager, objects updateRateObjects, regions *ddm.Manager, owners *ownership.Manager) *updateRateBridge {
	b := &updateRateBridge{foms: foms, declarations: declarations, objects: objects, regions: regions, owners: owners}
	regions.SetUpdateRateHooks(b.refreshLocked, b.objectInterest, b.guard)
	regions.SetOnRegionStateChanged(objects.RefreshDiscoveries)
	owners.SetOnOwnershipChanged(func(ctx context.Context, fed core.FederationName, obj core.ObjectHandle) {
		b.ObjectChanged(ctx, fed, obj)
		objects.RefreshDiscoveries(ctx, fed)
	})
	return b
}

func (b *updateRateBridge) ObjectChanged(ctx context.Context, fed core.FederationName, obj core.ObjectHandle) {
	b.mu.Lock()
	defer b.mu.Unlock()
	model, err := b.foms.Get(ctx, fed)
	if err != nil {
		return
	}
	enumerator, ok := model.(interface {
		AttributeHandles(core.ObjectClassHandle) []core.AttributeHandle
	})
	if !ok {
		return
	}
	var instance *object.InstanceState
	instances := objectInstances(b.objects, fed)
	for i := range instances {
		if instances[i].Handle == obj {
			instance = &instances[i]
			break
		}
	}
	if instance == nil {
		_ = b.declarations.RemoveObjectUpdateRate(ctx, fed, obj)
		return
	}
	owners := map[core.AttributeHandle]core.FederateHandle{}
	for _, attr := range enumerator.AttributeHandles(instance.Class) {
		owners[attr] = 0
	}
	if current := b.owners.ExportState(fed); current != nil {
		for _, entry := range current.Owners {
			if entry.Object == obj {
				owners[entry.Attribute] = entry.Owner
			}
		}
	}
	projected := b.declarations.ExportState(fed)
	projected.UpdateRateObjects = slices.DeleteFunc(projected.UpdateRateObjects, func(e declaration.UpdateRateObjectState) bool { return e.Object == obj })
	entry := declaration.UpdateRateObjectState{Object: obj, Class: instance.Class}
	for attr, owner := range owners {
		entry.Owners = append(entry.Owners, declaration.UpdateRateOwnerState{Attribute: attr, Owner: owner})
	}
	projected.UpdateRateObjects = append(projected.UpdateRateObjects, entry)
	rates := regionalRateDemands(model, projected.UpdateRateObjects, b.regions.ExportState(fed))
	// Owner mutation is already committed; declaration keeps failed advice pending.
	_ = b.declarations.SyncObjectUpdateRateState(ctx, fed, obj, instance.Class, owners, rates)
}

func (b *updateRateBridge) ObjectDeleted(ctx context.Context, fed core.FederationName, obj core.ObjectHandle) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_ = b.declarations.RemoveObjectUpdateRate(ctx, fed, obj)
}

func (b *updateRateBridge) FederateResigned(fed core.FederationName, h core.FederateHandle) {
	// DDM removes participant regions and references before its mandatory refresh.
	b.regions.OnFederateResign(fed, h)
}

func (b *updateRateBridge) Refresh(ctx context.Context, fed core.FederationName) error {
	return b.refreshMode(ctx, fed, false)
}

func (b *updateRateBridge) refreshMode(ctx context.Context, fed core.FederationName, mandatory bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.refreshLocked(ctx, fed, mandatory)
}

func (b *updateRateBridge) guard() func() { b.mu.Lock(); return b.mu.Unlock }

func (b *updateRateBridge) refreshLocked(ctx context.Context, fed core.FederationName, mandatory bool) error {
	model, err := b.foms.Get(ctx, fed)
	if err != nil {
		return err
	}
	state := b.declarations.ExportState(fed)
	rates := regionalRateDemands(model, state.UpdateRateObjects, b.regions.ExportState(fed))
	if mandatory {
		return b.declarations.SyncRegionalUpdateRateSubscriptions(ctx, fed, rates)
	}
	return b.declarations.SetRegionalUpdateRateSubscriptions(ctx, fed, rates)
}

func (b *updateRateBridge) objectInterest(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) error {
	model, err := b.foms.Get(ctx, fed)
	if err != nil {
		return err
	}
	state := b.declarations.ExportState(fed)
	rates := regionalRateDemands(model, state.UpdateRateObjects, b.regions.ExportState(fed))
	return b.declarations.SetRegionalObjectInterestAndUpdateRates(ctx, fed, sub, cls, attrs, rates)
}

func regionalRateDemands(model core.FOMHandle, objects []declaration.UpdateRateObjectState, state *ddm.ManagerState) []core.RegionalUpdateRateSubscription {
	if state == nil {
		return nil
	}
	regions := map[ddm.RegionHandle]ddm.RegionState{}
	for _, r := range state.Regions {
		regions[r.Handle] = r
	}
	type target struct {
		obj  core.ObjectHandle
		attr core.AttributeHandle
	}
	associations := map[target][]ddm.RegionHandle{}
	for _, p := range state.ObjectPublications {
		associations[target{p.Object, p.Attribute}] = p.Regions
	}
	names, named := model.(core.FOMHandleNameLookup)
	hierarchy, inherited := model.(core.FOMClassAncestry)
	seen := map[core.RegionalUpdateRateSubscription]struct{}{}
	var result []core.RegionalUpdateRateSubscription
	for _, obj := range objects {
		for _, row := range state.ObjectSubscriptions {
			attr := row.Attribute
			if row.ObjectClass != obj.Class {
				if !named || !inherited || !slices.Contains(hierarchy.ObjectClassAncestors(obj.Class), row.ObjectClass) {
					continue
				}
				name, ok := names.AttributeName(row.ObjectClass, row.Attribute)
				if !ok {
					continue
				}
				var found bool
				attr, found = model.LookupAttribute(obj.Class, name)
				if !found {
					continue
				}
			}
			if !slices.ContainsFunc(obj.Owners, func(owner declaration.UpdateRateOwnerState) bool { return owner.Attribute == attr }) {
				continue
			}
			if !regionalRateOverlap(associations[target{obj.Object, attr}], row.Regions, regions) {
				continue
			}
			entry := core.RegionalUpdateRateSubscription{Object: obj.Object, Attribute: attr, Subscriber: row.Subscriber, Options: core.SubscriptionOptions{Active: !row.Passive, UpdateRateDesignator: row.UpdateRateDesignator}}
			if _, ok := seen[entry]; !ok {
				seen[entry] = struct{}{}
				result = append(result, entry)
			}
		}
	}
	return result
}

func regionalRateOverlap(publishers, subscribers []ddm.RegionHandle, regions map[ddm.RegionHandle]ddm.RegionState) bool {
	if len(subscribers) == 0 {
		return false
	}
	for _, sub := range subscribers {
		sr, ok := regions[sub]
		if !ok {
			continue
		}
		if len(publishers) == 0 {
			return true
		} // existing default-region routing policy
		for _, pub := range publishers {
			pr, ok := regions[pub]
			if !ok {
				continue
			}
			if pr.RoutingSpace != sr.RoutingSpace {
				continue
			}
			ranges := map[ddm.DimensionHandle]core.DDMRange{}
			for _, r := range pr.Committed {
				ranges[r.Dimension] = core.DDMRange{Lower: r.Lower, Upper: r.Upper}
			}
			overlap := true
			for _, r := range sr.Committed {
				if p, common := ranges[r.Dimension]; common && !p.Overlap(core.DDMRange{Lower: r.Lower, Upper: r.Upper}) {
					overlap = false
					break
				}
			}
			if overlap {
				return true
			}
		}
	}
	return false
}
