package object

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func (r *RegistryOf[T]) ChangeAttributeOrderType(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, order core.OrderType) error {
	if order != core.OrderReceive && order != core.OrderTimestamp {
		return fmt.Errorf("invalid order type: %d", order)
	}
	st := r.stateFor(fed)
	st.mu.Lock()
	defer st.mu.Unlock()
	actual, err := r.validateOwnedKnownAttributes(ctx, st, fed, caller, obj, attrs)
	if err != nil {
		return err
	}
	if st.attributeOrders == nil {
		st.attributeOrders = make(map[core.ObjectHandle]map[core.AttributeHandle]core.OrderType)
	}
	if st.attributeOrders[obj] == nil {
		st.attributeOrders[obj] = make(map[core.AttributeHandle]core.OrderType)
	}
	for _, attr := range actual {
		st.attributeOrders[obj][attr] = order
	}
	return nil
}

func (r *RegistryOf[T]) ChangeInteractionOrderType(ctx context.Context, fed core.FederationName, caller core.FederateHandle, cls core.InteractionClassHandle, order core.OrderType) error {
	if order != core.OrderReceive && order != core.OrderTimestamp {
		return fmt.Errorf("invalid order type: %d", order)
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
	st := r.stateFor(fed)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.interactionOrders == nil {
		st.interactionOrders = make(map[core.FederateHandle]map[core.InteractionClassHandle]core.OrderType)
	}
	if st.interactionOrders[caller] == nil {
		st.interactionOrders[caller] = make(map[core.InteractionClassHandle]core.OrderType)
	}
	st.interactionOrders[caller][cls] = order
	return nil
}

func (r *RegistryOf[T]) attributeDeliveryTime(fed core.FederationName, inst *objectInstance, attr core.AttributeHandle, ts *T) *T {
	if ts == nil {
		return nil
	}
	st := r.stateFor(fed)
	st.mu.Lock()
	order := st.attributeOrders[inst.handle][attr]
	st.mu.Unlock()
	if order == core.OrderReceive {
		return nil
	}
	if order == core.OrderTimestamp {
		return ts
	}
	return r.deliveryTimestampForAttributes(fed, inst.cls, []core.AttributeHandle{attr}, ts)
}

func (r *RegistryOf[T]) interactionDeliveryTime(fed core.FederationName, producer core.FederateHandle, cls core.InteractionClassHandle, ts *T) *T {
	if ts == nil {
		return nil
	}
	st := r.stateFor(fed)
	st.mu.Lock()
	order := st.interactionOrders[producer][cls]
	st.mu.Unlock()
	if order == core.OrderReceive {
		return nil
	}
	if order == core.OrderTimestamp {
		return ts
	}
	return r.deliveryTimestampForInteraction(fed, cls, ts)
}

func (r *RegistryOf[T]) deletionDeliveryTime(ctx context.Context, fed core.FederationName, inst *objectInstance, ts *T) *T {
	if ts == nil {
		return nil
	}
	if fom, err := r.opts.FOMs.Get(ctx, fed); err == nil && fom != nil {
		if attr, ok := fom.LookupAttribute(inst.cls, "HLAprivilegeToDeleteObject"); ok {
			return r.attributeDeliveryTime(fed, inst, attr, ts)
		}
	}
	// Legacy embedded FOMs without the privilege attribute retain the default.
	return ts
}
