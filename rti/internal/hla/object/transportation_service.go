package object

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func validTransportation(tt core.TransportType) bool {
	return tt == core.TransportTypeReliable || tt == core.TransportTypeBestEffort
}

// transportationObject must be called with st.mu held. Registration,
// discovery, or ownership of the requested attribute establishes knowledge.
func (r *RegistryOf[T]) transportationObject(st *federationStateOf[T], fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (*objectInstance, error) {
	inst := st.instances[obj]
	if inst == nil {
		return nil, core.ErrObjectHandleInvalid
	}
	if _, known := st.discovered[discoverKey{sub: caller, obj: obj}]; known {
		return inst, nil
	}
	if r.opts.Ownership != nil {
		for _, attr := range attrs {
			if r.opts.Ownership.IsOwnedBy(fed, caller, obj, attr) {
				return inst, nil
			}
		}
	}
	return nil, core.ErrObjectHandleInvalid
}

func (r *RegistryOf[T]) transportationFOM(ctx context.Context, fed core.FederationName) (core.FOMHandle, error) {
	fom, err := r.opts.FOMs.Get(ctx, fed)
	if err != nil {
		return nil, err
	}
	if fom == nil || !fom.IsValid() {
		return nil, fmt.Errorf("transportation: federation FOM unavailable")
	}
	return fom, nil
}

func transportationAttributeDefined(fom core.FOMHandle, cls core.ObjectClassHandle, attr core.AttributeHandle) error {
	names, ok := fom.(interface {
		AttributeName(core.ObjectClassHandle, core.AttributeHandle) (string, bool)
	})
	if !ok {
		return fmt.Errorf("transportation: FOM attribute lookup unavailable")
	}
	if _, ok := names.AttributeName(cls, attr); !ok {
		return core.ErrAttributeNotFound
	}
	return nil
}

func transportationInteractionDefined(fom core.FOMHandle, cls core.InteractionClassHandle) error {
	names, ok := fom.(interface {
		InteractionClassName(core.InteractionClassHandle) (string, bool)
	})
	if !ok {
		return fmt.Errorf("transportation: FOM interaction lookup unavailable")
	}
	if _, ok := names.InteractionClassName(cls); !ok {
		return core.ErrInteractionClassNotFound
	}
	return nil
}

func (r *RegistryOf[T]) RequestAttributeTransportationTypeChange(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, tt core.TransportType) error {
	if !validTransportation(tt) {
		return core.ErrTransportTypeUnspecified
	}
	st := r.stateFor(fed)
	st.mu.Lock()
	defer st.mu.Unlock()
	actual, err := r.validateAttributeTransportation(ctx, st, core.TransportationScope{Member: core.MemberIdentity{Federation: fed, Handle: caller}}, obj, attrs, tt)
	if err != nil {
		return err
	}
	r.transports.mu.Lock()
	defer r.transports.mu.Unlock()
	for _, attr := range actual {
		r.transports.attr[attrTransportKey{fed: fed, obj: obj, attr: attr}] = tt
	}
	return nil
}

func (r *RegistryOf[T]) QueryAttributeTransportationType(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attr core.AttributeHandle) (core.TransportType, error) {
	st := r.stateFor(fed)
	st.mu.Lock()
	defer st.mu.Unlock()
	inst, err := r.transportationObject(st, fed, caller, obj, []core.AttributeHandle{attr})
	if err != nil {
		return 0, err
	}
	fom, err := r.transportationFOM(ctx, fed)
	if err != nil {
		return 0, err
	}
	actual, err := r.knownAttributes(ctx, fed, st, caller, inst, []core.AttributeHandle{attr})
	if err != nil {
		return 0, err
	}
	attr = actual[0]
	owned := true
	if r.opts.Ownership != nil {
		ownership, ok := r.opts.Ownership.(ownershipQuerySnapshot)
		if !ok {
			return 0, fmt.Errorf("transportation: current-owner lookup unavailable")
		}
		_, owned = ownership.QueryOwnership(fed, obj, attr)
	}
	if owned {
		if tt := r.AttributeTransportType(fed, obj, attr); tt != core.TransportTypeUnspecified {
			return tt, nil
		}
	}
	if defaults, ok := fom.(core.FOMTransportationLookup); ok {
		if tt, found := defaults.TransportationForAttribute(inst.cls, attr); found && validTransportation(tt) {
			return tt, nil
		}
	}
	return 0, fmt.Errorf("transportation: FOM attribute transportation unavailable")
}

func (r *RegistryOf[T]) RequestInteractionTransportationTypeChange(ctx context.Context, fed core.FederationName, caller core.FederateHandle, cls core.InteractionClassHandle, tt core.TransportType) error {
	if !validTransportation(tt) {
		return core.ErrTransportTypeUnspecified
	}
	fom, err := r.transportationFOM(ctx, fed)
	if err != nil {
		return err
	}
	if err := transportationInteractionDefined(fom, cls); err != nil {
		return err
	}
	if !r.producerPublishesInteraction(ctx, fed, caller, cls) {
		return core.ErrInteractionClassNotPublished
	}
	r.transports.mu.Lock()
	defer r.transports.mu.Unlock()
	r.transports.inter[interTransportKey{fed: fed, publisher: caller, cls: cls}] = tt
	return nil
}

func (r *RegistryOf[T]) QueryInteractionTransportationType(ctx context.Context, fed core.FederationName, _ core.FederateHandle, target core.FederateHandle, cls core.InteractionClassHandle) (core.TransportType, error) {
	fom, err := r.transportationFOM(ctx, fed)
	if err != nil {
		return 0, err
	}
	if err := transportationInteractionDefined(fom, cls); err != nil {
		return 0, err
	}
	if r.producerPublishesInteraction(ctx, fed, target, cls) {
		if tt := r.InteractionTransportType(fed, target, cls); tt != core.TransportTypeUnspecified {
			return tt, nil
		}
	}
	if defaults, ok := fom.(core.FOMTransportationLookup); ok {
		if tt, found := defaults.TransportationForInteraction(cls); found && validTransportation(tt) {
			return tt, nil
		}
	}
	return 0, fmt.Errorf("transportation: FOM interaction transportation unavailable")
}

var _ core.TransportationManagement = (*Registry)(nil)
