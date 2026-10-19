package ddm

import (
	"context"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var _ core.ObjectRegionRegistrar = (*Manager)(nil)
var _ core.GroupedObjectRegionRegistrar = (*Manager)(nil)

// PrepareObjectRegistration validates and copies registration bindings while
// keeping the referenced regions stable until the caller releases the result.
// The caller already holds the object-state mutex; this path only locks DDM.
func (m *Manager) PrepareObjectRegistration(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	cls core.ObjectClassHandle,
	bindings map[core.AttributeHandle][]core.DDMRegionHandleCore,
) (core.ObjectRegionRegistration, error) {
	return m.prepareObjectRegistration(ctx, fed, owner, cls, bindingGroups(bindings), false)
}

func (m *Manager) PrepareObjectRegistrationGroups(ctx context.Context, fed core.FederationName, owner core.FederateHandle, cls core.ObjectClassHandle, groups []core.AttributeRegionGroup) (core.ObjectRegionRegistration, error) {
	return m.prepareObjectRegistration(ctx, fed, owner, cls, groups, true)
}

func (m *Manager) prepareObjectRegistration(ctx context.Context, fed core.FederationName, owner core.FederateHandle, cls core.ObjectClassHandle, groups []core.AttributeRegionGroup, allowEmpty bool) (core.ObjectRegionRegistration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Resolve immutable FOM context before taking the DDM lock. No object,
	// ownership, federation lifecycle, or delivery callbacks are needed here.
	h, err := m.opts.FOMs.Get(ctx, fed)
	if err != nil {
		return nil, fmt.Errorf("ddm: prepare object registration in %q: %w", fed, err)
	}
	if isNilInterface(h) || !h.IsValid() {
		return nil, fmt.Errorf("ddm: prepare object registration in %q without a valid FOM: %w", fed, core.ErrInvalidRegionContext)
	}
	if names, ok := h.(interface {
		ObjectClassName(core.ObjectClassHandle) (string, bool)
	}); ok {
		if _, found := names.ObjectClassName(cls); !found {
			return nil, fmt.Errorf("ddm: prepare object class %d in %q: %w", cls, fed, core.ErrObjectClassNotFound)
		}
	}
	dimensions, ok := h.(core.FOMAttributeDimensions)
	attrs := groupAttributes(groups)
	if !ok && (len(attrs) != 0 || !allowEmpty) {
		return nil, fmt.Errorf("ddm: prepare object registration in %q without attribute dimensions: %w", fed, core.ErrInvalidRegionContext)
	}
	copied := make(map[core.AttributeHandle][]RegionHandle, len(attrs))
	required, err := registrationDimensions(dimensions, attrs, cls, fed)
	if err != nil {
		return nil, err
	}
	if err := copyRegistrationGroups(copied, groups, allowEmpty); err != nil {
		return nil, err
	}
	// Stage a missing federation's state privately so an uncommitted release
	// never creates visible state. Existing regions are always read under mu.
	initial := newFederationDDMState()
	if enumerator, ok := h.(DimensionEnumerator); ok {
		initial.populateFromFOM(enumerator.Dimensions())
	}
	return m.lockObjectRegistration(ctx, fed, owner, groups, required, copied, initial)
}

func registrationDimensions(dimensions core.FOMAttributeDimensions, attrs []core.AttributeHandle, cls core.ObjectClassHandle, fed core.FederationName) (map[core.AttributeHandle][]DimensionHandle, error) {
	required := make(map[core.AttributeHandle][]DimensionHandle, len(attrs))
	for _, attr := range attrs {
		dims, found := dimensions.AttributeDimensions(cls, attr)
		if attr == core.InvalidAttributeHandle || !found {
			return nil, fmt.Errorf("ddm: prepare attribute %d of class %d in %q: %w", attr, cls, fed, core.ErrAttributeNotFound)
		}
		required[attr] = dimensionSet(dims)
	}
	return required, nil
}

func copyRegistrationGroups(copied map[core.AttributeHandle][]RegionHandle, groups []core.AttributeRegionGroup, allowEmpty bool) error {
	for _, group := range groups {
		if len(group.Regions) == 0 {
			if !allowEmpty && len(group.Attributes) != 0 {
				return core.ErrInvalidRegionContext
			}
			continue
		}
		for _, attr := range group.Attributes {
			copied[attr] = append(copied[attr], group.Regions...)
		}
	}
	for attr, regions := range copied {
		slices.Sort(regions)
		copied[attr] = slices.Compact(regions)
	}
	return nil
}

func (m *Manager) lockObjectRegistration(ctx context.Context, fed core.FederationName, owner core.FederateHandle, groups []core.AttributeRegionGroup, required map[core.AttributeHandle][]DimensionHandle, copied map[core.AttributeHandle][]RegionHandle, initial *federationDDMState) (core.ObjectRegionRegistration, error) {
	m.mu.Lock()
	prepared := false
	defer func() {
		if !prepared {
			m.mu.Unlock()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	st := m.fed[fed]
	if st == nil {
		st = initial
	}
	if err := validateGroups(st, owner, groups, required); err != nil {
		return nil, err
	}
	prepared = true
	return &objectRegionRegistration{manager: m, fed: fed, state: st, bindings: copied}, nil
}

func dimensionSet(dims []DimensionHandle) []DimensionHandle {
	set := slices.Clone(dims)
	slices.Sort(set)
	return slices.Compact(set)
}

type objectRegionRegistration struct {
	manager   *Manager
	fed       core.FederationName
	state     *federationDDMState
	bindings  map[core.AttributeHandle][]RegionHandle
	committed bool
	released  bool
}

func (r *objectRegionRegistration) Commit(obj core.ObjectHandle) {
	if r.released || r.committed {
		return
	}
	if len(r.bindings) != 0 {
		r.state.objPubs[obj] = r.bindings
		r.manager.fed[r.fed] = r.state
	}
	r.committed = true
}

func (r *objectRegionRegistration) Release() {
	if !r.released {
		r.released = true
		r.manager.mu.Unlock()
	}
}

// ForgetObject removes a deleted object's publisher associations. Other
// objects and subscriptions retain their own uses of the same regions.
func (m *Manager) ForgetObject(fed core.FederationName, obj core.ObjectHandle) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st := m.fed[fed]; st != nil {
		delete(st.objPubs, obj)
		delete(st.inactivePubs, obj)
		if shadow := m.rollbackStateLocked(fed, obj, st); shadow != nil {
			delete(shadow.objPubs, obj)
			delete(shadow.inactivePubs, obj)
		}
	}
}
