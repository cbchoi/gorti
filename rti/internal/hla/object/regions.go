package object

import (
	"context"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var _ core.RegionAssociationValidator = (*Registry)(nil)

type attributeOwnerLease interface {
	PrepareAttributeOwners(core.FederationName, core.ObjectHandle, []core.AttributeHandle) (map[core.AttributeHandle]core.FederateHandle, func())
}

func (r *RegistryOf[T]) PrepareRegionAssociation(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, func(), error) {
	st := r.stateFor(fed)
	st.mu.Lock()
	release := sync.OnceFunc(st.mu.Unlock)
	valid := false
	defer func() {
		if !valid {
			release()
		}
	}()
	inst, err := r.transportationObject(st, fed, caller, obj, attrs)
	if err != nil {
		return 0, nil, err
	}
	// The legacy DDM validator cannot return normalized attribute handles.
	// Reject ancestor-coordinate mutation until its storage boundary consumes
	// mapped handles; equal numeric offsets can denote different attributes.
	if len(attrs) != 0 && st.knownClass(caller, inst) != inst.cls {
		return 0, nil, core.ErrInvalidRegionContext
	}
	fom, err := r.transportationFOM(ctx, fed)
	if err != nil {
		return 0, nil, err
	}
	// DDM expands a legacy clear-all request to the currently associated
	// attributes, then rechecks that set while this object lease is retained.
	for _, attr := range attrs {
		if err := transportationAttributeDefined(fom, inst.cls, attr); err != nil {
			return 0, nil, err
		}
	}
	var owners map[core.AttributeHandle]core.FederateHandle
	if snapshots, ok := r.opts.Ownership.(attributeOwnerLease); ok {
		var unlock func()
		owners, unlock = snapshots.PrepareAttributeOwners(fed, obj, attrs)
		objectUnlock := release
		release = sync.OnceFunc(func() { unlock(); objectUnlock() })
	}
	for _, attr := range attrs {
		owned := false
		switch {
		case owners != nil:
			owned = owners[attr] == caller
		case r.opts.Ownership != nil:
			owned = r.opts.Ownership.IsOwnedBy(fed, caller, obj, attr)
		default:
			owned = inst.owner == caller && slices.Contains(r.opts.Declarations.PublishersFor(ctx, fed, inst.cls, attr), caller)
		}
		if !owned {
			return 0, nil, core.ErrAttributeNotOwned
		}
	}
	valid = true
	return inst.cls, release, nil
}

func (r *RegistryOf[T]) SendInteractionWithRegions(ctx context.Context, fed core.FederationName, caller core.FederateHandle, cls core.InteractionClassHandle, params map[core.ParameterHandle][]byte, ts *T, regions []core.DDMRegionHandleCore) error {
	return r.SendInteractionWithRegionsMetadata(ctx, fed, caller, cls, params, ts, regions, core.ObjectMessageOptions{})
}

func (r *RegistryOf[T]) SendInteractionWithRegionsMetadata(ctx context.Context, fed core.FederationName, caller core.FederateHandle, cls core.InteractionClassHandle, params map[core.ParameterHandle][]byte, ts *T, regions []core.DDMRegionHandleCore, options core.ObjectMessageOptions) error {
	routing, ok := r.opts.RegistrationRegions.(core.RegionRouting)
	if !ok {
		return core.ErrInvalidRegionContext
	}
	// Region errors precede any sequence allocation, journaling, MOM dispatch,
	// or callback reservation. This is a call-local snapshot, not publication.
	var subs []core.FederateHandle
	var copies []core.ConveyedRegionCopy
	var overlapsDefault bool
	var err error
	if snapshot, ok := r.opts.RegistrationRegions.(interactionSnapshotRouting); ok {
		subs, copies, overlapsDefault, err = snapshot.InteractionDeliverySnapshot(ctx, fed, caller, cls, regions)
	} else {
		subs, err = routing.InteractionSubscribersWithRegions(ctx, fed, caller, cls, regions)
		overlapsDefault = r.interactionRegionsOverlapDefault(ctx, fed, cls, regions)
	}
	if err != nil {
		return err
	}
	if overlapsDefault {
		subs = append(subs, r.ordinaryInteractionSubscribers(ctx, fed, cls)...)
	}
	slices.Sort(subs)
	subs = slices.Compact(subs)
	return r.sendInteractionTo(ctx, fed, caller, cls, params, ts, options.RetractionHandle, subs, true, messageMetadata{tag: slices.Clone(options.Tag), regions: slices.Clone(regions), conveyed: slices.Clone(copies)})
}

func (r *RegistryOf[T]) interactionRegionsOverlapDefault(ctx context.Context, fed core.FederationName, cls core.InteractionClassHandle, regions []core.DDMRegionHandleCore) bool {
	if matcher, ok := r.opts.RegistrationRegions.(interface {
		InteractionRegionsOverlapDefaultRegion(context.Context, core.FederationName, core.InteractionClassHandle, []core.DDMRegionHandleCore) bool
	}); ok {
		return matcher.InteractionRegionsOverlapDefaultRegion(ctx, fed, cls, regions)
	}
	return true
}

func (r *RegistryOf[T]) publisherRegionsOverlapDefault(fed core.FederationName, cls core.ObjectClassHandle, attr core.AttributeHandle, regions []DDMRegionHandle) bool {
	if len(regions) == 0 {
		return true
	}
	if matcher, ok := r.opts.RegistrationRegions.(interface {
		PublisherRegionsOverlapDefaultRegion(core.FederationName, core.ObjectClassHandle, core.AttributeHandle, []core.DDMRegionHandleCore) bool
	}); ok {
		handles := make([]core.DDMRegionHandleCore, len(regions))
		for i, region := range regions {
			handles[i] = core.DDMRegionHandleCore(region)
		}
		return matcher.PublisherRegionsOverlapDefaultRegion(fed, cls, attr, handles)
	}
	return true
}

func (r *RegistryOf[T]) RequestAttributeValueUpdateWithRegions(ctx context.Context, fed core.FederationName, caller core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, regions []core.DDMRegionHandleCore, tag []byte) error {
	return r.requestValueUpdates(ctx, fed, caller, 0, cls, attrs, regions, tag, true)
}
