package object

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type updateRequestTarget struct {
	object core.ObjectHandle
	attrs  []core.AttributeHandle
}

func requestAttributes(fom core.FOMHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) error {
	if cls == 0 {
		return core.ErrObjectClassNotFound
	}
	if names, ok := fom.(core.FOMHandleNameLookup); ok {
		if _, found := names.ObjectClassName(cls); !found {
			return core.ErrObjectClassNotFound
		}
		for _, attr := range attrs {
			if _, found := names.AttributeName(cls, attr); !found {
				return core.ErrAttributeNotFound
			}
		}
	}
	for _, attr := range attrs {
		if attr == 0 {
			return core.ErrAttributeNotFound
		}
	}
	return nil
}

// Handles in this repository are class-relative. Resolve inherited requests
// by name in the instance class instead of reusing numeric offsets.
func instanceRequestAttributes(fom core.FOMHandle, requested, actual core.ObjectClassHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, bool) {
	if requested == actual {
		return slices.Clone(attrs), true
	}
	names, ok := fom.(core.FOMHandleNameLookup)
	if !ok {
		return nil, false
	}
	parent, found := names.ObjectClassName(requested)
	child, exists := names.ObjectClassName(actual)
	if !found || !exists || !strings.HasPrefix(child, parent+".") {
		return nil, false
	}
	result := make([]core.AttributeHandle, 0, len(attrs))
	for _, attr := range attrs {
		name, found := names.AttributeName(requested, attr)
		if !found {
			return nil, false
		}
		resolved, found := fom.LookupAttribute(actual, name)
		if !found {
			return nil, false
		}
		result = append(result, resolved)
	}
	return result, true
}

func (r *RegistryOf[T]) requestValueUpdates(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, regions []core.DDMRegionHandleCore, tag []byte, scoped bool) error {
	return r.requestValueUpdateGroups(ctx, fed, caller, obj, cls, attrs, []core.AttributeRegionGroup{{Attributes: attrs, Regions: regions}}, tag, scoped, false)
}

// RequestAttributeValueUpdateWithRegionGroups admits the whole collection before
// selecting callbacks. It never decomposes a public request into separate sends.
func (r *RegistryOf[T]) RequestAttributeValueUpdateWithRegionGroups(ctx context.Context, fed core.FederationName, caller core.FederateHandle, cls core.ObjectClassHandle, groups []core.AttributeRegionGroup, tag []byte) error {
	var attrs []core.AttributeHandle
	for _, group := range groups {
		attrs = append(attrs, group.Attributes...)
	}
	return r.requestValueUpdateGroups(ctx, fed, caller, 0, cls, attrs, groups, tag, true, true)
}

func (r *RegistryOf[T]) requestValueUpdateGroups(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, groups []core.AttributeRegionGroup, tag []byte, scoped, atomic bool) error {
	if caller == core.InvalidFederateHandle {
		return core.ErrFederateNotJoined
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fom, err := r.opts.FOMs.Get(ctx, fed)
	if err != nil && !errors.Is(err, core.ErrFederationNotFound) {
		return err
	}
	attrs = slices.Clone(attrs)
	slices.Sort(attrs)
	attrs = slices.Compact(attrs)
	st := r.stateFor(fed)
	st.mu.Lock()
	deliveries, err := r.prepareValueUpdateDeliveriesLocked(ctx, fed, caller, st, fom, obj, cls, attrs, groups, tag, scoped, atomic)
	if err != nil {
		return err
	}
	if len(deliveries) == 0 {
		return nil
	}
	deliveries, err = r.refreshMOMUpdateRequests(ctx, fed, deliveries)
	if err != nil {
		return err
	}
	if len(deliveries) == 0 {
		return nil
	}
	if reservable, ok := r.opts.Outbox.(core.ReservableOutbox); ok {
		reservation, err := reservable.Reserve(ctx, fed, deliveries)
		if err != nil {
			return err
		}
		defer reservation.Release()
		return reservation.Commit()
	}
	if atomic && len(deliveries) > 1 {
		return fmt.Errorf("object: atomic grouped update request requires reservable delivery")
	}
	var failures []error
	for _, delivery := range deliveries {
		if err := r.opts.Outbox.Send(ctx, fed, delivery.Recipient, delivery.Event); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

type legacyUpdateRequest struct{ core.RegionUpdateRequest }

func (r legacyUpdateRequest) Matches(obj core.ObjectHandle, _ core.AttributeHandle, actual core.AttributeHandle) bool {
	return r.RegionUpdateRequest.Matches(obj, actual)
}

func (r *RegistryOf[T]) prepareValueUpdateDeliveriesLocked(ctx context.Context, fed core.FederationName, caller core.FederateHandle, st *federationStateOf[T], fom core.FOMHandle, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, groups []core.AttributeRegionGroup, tag []byte, scoped, atomic bool) ([]core.OutboxDelivery, error) {
	defer st.mu.Unlock()
	if obj != 0 {
		inst, err := r.transportationObject(st, fed, caller, obj, attrs)
		if err != nil {
			return nil, err
		}
		cls = st.knownClass(caller, inst)
	}
	if err := requestAttributes(fom, cls, attrs); err != nil {
		return nil, err
	}
	filter, err := r.prepareUpdateRequestFilter(ctx, fed, caller, cls, attrs, groups, scoped, atomic)
	if err != nil {
		return nil, err
	}
	targets := selectUpdateRequestTargets(fom, st, obj, cls, attrs, filter)
	// Never acquire ownership locks while retaining the DDM read lease.
	if filter != nil {
		filter.Release()
	}
	ownership, hasOwnerQuery := r.opts.Ownership.(ownershipQuerySnapshot)
	if r.opts.Ownership != nil && !hasOwnerQuery {
		return nil, fmt.Errorf("object: current attribute owner lookup unavailable")
	}
	var deliveries []core.OutboxDelivery
	for _, target := range targets {
		byOwner := r.updateRequestOwners(fed, caller, st, target, ownership, hasOwnerQuery, scoped)
		var owners []core.FederateHandle
		for owner := range byOwner {
			owners = append(owners, owner)
		}
		slices.Sort(owners)
		for _, owner := range owners {
			inst := st.instances[target.object]
			knownAttrs := r.projectAttributes(ctx, fed, inst.cls, st.knownClass(owner, inst), byOwner[owner])
			evt := &CallbackEventOf[T]{Kind: CallbackProvideUpdate, Object: target.object,
				AttributeHandles: knownAttrs, UserSuppliedTag: slices.Clone(tag)}
			deliveries = append(deliveries, core.OutboxDelivery{Recipient: owner, Event: evt})
		}
	}
	seq := st.nextOutboundSeqRangeLocked(len(deliveries))
	for _, delivery := range deliveries {
		delivery.Event.(*CallbackEventOf[T]).Sequence = seq
		seq++
	}
	return deliveries, nil
}

func (r *RegistryOf[T]) prepareUpdateRequestFilter(ctx context.Context, fed core.FederationName, caller core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, groups []core.AttributeRegionGroup, scoped, atomic bool) (core.GroupedRegionUpdateRequest, error) {
	var filter core.GroupedRegionUpdateRequest
	if scoped {
		var err error
		if routing, ok := r.opts.RegistrationRegions.(core.GroupedRegionRouting); ok {
			filter, err = routing.PrepareRegionUpdateGroups(ctx, fed, caller, cls, groups)
		} else if routing, ok := r.opts.RegistrationRegions.(core.RegionRouting); ok && !atomic {
			var legacy core.RegionUpdateRequest
			legacy, err = routing.PrepareRegionUpdateRequest(ctx, fed, caller, cls, attrs, groups[0].Regions)
			if err == nil {
				filter = legacyUpdateRequest{legacy}
			}
		} else {
			return nil, core.ErrInvalidRegionContext
		}
		if err != nil {
			return nil, err
		}
	}
	return filter, nil
}

func selectUpdateRequestTargets[T core.LogicalTimeScalar](fom core.FOMHandle, st *federationStateOf[T], obj core.ObjectHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, filter core.GroupedRegionUpdateRequest) []updateRequestTarget {
	var handles []core.ObjectHandle
	if obj != 0 {
		handles = []core.ObjectHandle{obj}
	} else {
		for handle := range st.instances {
			handles = append(handles, handle)
		}
		slices.Sort(handles)
	}
	var targets []updateRequestTarget
	for _, handle := range handles {
		inst := st.instances[handle]
		mapped, matches := instanceRequestAttributes(fom, cls, inst.cls, attrs)
		if !matches {
			continue
		}
		selected := mapped[:0]
		for i, attr := range mapped {
			if filter == nil || filter.Matches(handle, attrs[i], attr) {
				selected = append(selected, attr)
			}
		}
		if len(selected) != 0 {
			targets = append(targets, updateRequestTarget{handle, selected})
		}
	}
	return targets
}

func (r *RegistryOf[T]) updateRequestOwners(fed core.FederationName, caller core.FederateHandle, st *federationStateOf[T], target updateRequestTarget, ownership ownershipQuerySnapshot, hasOwnerQuery, scoped bool) map[core.FederateHandle][]core.AttributeHandle {
	byOwner := make(map[core.FederateHandle][]core.AttributeHandle)
	var snapshot map[core.AttributeHandle]core.FederateHandle
	if snapshots, ok := r.opts.Ownership.(attributeOwnerLease); ok {
		owners, release := snapshots.PrepareAttributeOwners(fed, target.object, target.attrs)
		snapshot = owners
		release()
	}
	for _, attr := range target.attrs {
		owner, owned := st.instances[target.object].owner, true
		if snapshot != nil {
			owner, owned = snapshot[attr]
		} else if hasOwnerQuery {
			owner, owned = ownership.QueryOwnership(fed, target.object, attr)
		}
		if owned && owner != 0 && (!scoped || owner != caller) {
			byOwner[owner] = append(byOwner[owner], attr)
		}
	}
	return byOwner
}

func (r *RegistryOf[T]) refreshMOMUpdateRequests(ctx context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) ([]core.OutboxDelivery, error) {
	// MOM's predefined attributes belong to an internal producer without a
	// callback stream. Refresh them outside the object-state lock.
	var participantDeliveries []core.OutboxDelivery
	for _, delivery := range deliveries {
		if delivery.Recipient != internalMOMProducer {
			participantDeliveries = append(participantDeliveries, delivery)
			continue
		}
		refresh, ok := r.opts.ManagementDispatch.(interface {
			RequestMOMAttributeUpdate(context.Context, core.FederationName, core.ObjectHandle, []core.AttributeHandle) error
		})
		if !ok {
			return nil, fmt.Errorf("object: MOM attribute refresh unavailable")
		}
		event := delivery.Event.(*CallbackEventOf[T])
		if err := refresh.RequestMOMAttributeUpdate(ctx, fed, event.Object, event.AttributeHandles); err != nil {
			return nil, err
		}
	}
	return participantDeliveries, nil
}
