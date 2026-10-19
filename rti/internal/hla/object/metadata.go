package object

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type messageMetadata struct {
	tag      []byte
	regions  []core.DDMRegionHandleCore
	conveyed []core.ConveyedRegionCopy
}

func (r *RegistryOf[T]) UpdateAttributesWithMetadata(ctx context.Context, fed core.FederationName, producer core.FederateHandle, obj core.ObjectHandle, attrs map[core.AttributeHandle][]byte, ts *T, options core.ObjectMessageOptions) error {
	return r.updateAttributes(ctx, fed, producer, obj, attrs, ts, options.RetractionHandle, messageMetadata{tag: slices.Clone(options.Tag)})
}

func (r *RegistryOf[T]) SendInteractionWithMetadata(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.InteractionClassHandle, params map[core.ParameterHandle][]byte, ts *T, options core.ObjectMessageOptions) error {
	return r.sendInteractionTo(ctx, fed, producer, cls, params, ts, options.RetractionHandle, nil, false, messageMetadata{tag: slices.Clone(options.Tag)})
}

func (r *RegistryOf[T]) DeleteWithMetadata(ctx context.Context, fed core.FederationName, producer core.FederateHandle, obj core.ObjectHandle, ts *T, options core.ObjectMessageOptions) error {
	return r.deleteWithMetadata(ctx, fed, producer, obj, ts, options.Tag, options.RetractionHandle)
}

func metadataForOf[T core.LogicalTimeScalar](producer core.FederateHandle, ts *T, transport core.TransportType) *core.ObjectCallbackMetadata {
	m := &core.ObjectCallbackMetadata{SentOrder: core.OrderReceive, ReceivedOrder: core.OrderReceive, Transportation: transport}
	if producer != internalMOMProducer {
		m.Producer = producer
	}
	if ts != nil {
		m.SentOrder = core.OrderTimestamp
		m.ReceivedOrder = core.OrderUnspecified
	}
	return m
}

func (e *CallbackEventOf[T]) SetReceivedOrder(order core.OrderType) {
	if e != nil && e.Metadata != nil {
		e.Metadata.ReceivedOrder = order
	}
}

type attributeDeliveryOf[T core.LogicalTimeScalar] struct {
	subscriber        core.FederateHandle
	attributes        map[uint64][]byte
	time              *T
	transport         core.TransportType
	regions           []core.DDMRegionHandleCore
	conveyed          []core.ConveyedRegionCopy
	attributeConveyed map[core.AttributeHandle][]core.ConveyedRegionCopy
}

func (r *RegistryOf[T]) attributeDeliveries(ctx context.Context, fed core.FederationName, producer core.FederateHandle, inst *objectInstance, wire map[uint64][]byte, attrs []core.AttributeHandle, subs []core.FederateHandle, perAttr map[core.AttributeHandle][]core.FederateHandle, ts *T, captured ...map[core.AttributeHandle]core.AttributeRegionDeliverySnapshot) ([]attributeDeliveryOf[T], bool) {
	return r.attributeDeliveriesObserved(ctx, fed, producer, inst, wire, attrs, subs, perAttr, ts, nil, captured...)
}

type attributeDeliveryKey struct {
	timestamp bool
	transport core.TransportType
}

func (r *RegistryOf[T]) attributeDeliveriesObserved(ctx context.Context, fed core.FederationName, producer core.FederateHandle, inst *objectInstance, wire map[uint64][]byte, attrs []core.AttributeHandle, subs []core.FederateHandle, perAttr map[core.AttributeHandle][]core.FederateHandle, ts *T, observe func(core.TransportType), captured ...map[core.AttributeHandle]core.AttributeRegionDeliverySnapshot) ([]attributeDeliveryOf[T], bool) {
	keys, regionSets, timestamped := r.classifyAttributeDeliveries(ctx, fed, inst, attrs, ts, observe, captured...)
	_, reservableMessages := r.opts.TSOGate.(core.ReservableTSODeliveryGateOf[T])
	var out []attributeDeliveryOf[T]
	for _, sub := range subs {
		if sub == producer {
			continue
		}
		out = appendSubscriberAttributeDeliveries(out, sub, wire, attrs, perAttr, ts, keys, regionSets, reservableMessages, captured...)
	}
	for i := range out {
		slices.Sort(out[i].regions)
		out[i].regions = slices.Compact(out[i].regions)
	}
	return out, timestamped
}

func (r *RegistryOf[T]) attributeTransportation(ctx context.Context, fed core.FederationName, inst *objectInstance, attr core.AttributeHandle) core.TransportType {
	if tt := r.AttributeTransportType(fed, inst.handle, attr); tt != 0 {
		return tt
	}
	if r.opts.FOMs != nil {
		if fom, err := r.opts.FOMs.Get(ctx, fed); err == nil {
			if lookup, ok := fom.(core.FOMTransportationLookup); ok {
				tt, _ := lookup.TransportationForAttribute(inst.cls, attr)
				return tt
			}
		}
	}
	return 0
}

func (r *RegistryOf[T]) interactionTransportation(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.InteractionClassHandle) core.TransportType {
	if tt := r.InteractionTransportType(fed, producer, cls); tt != 0 {
		return tt
	}
	st := r.stateFor(fed)
	st.managementClassMu.RLock()
	classification, cached := st.managementClasses[cls]
	st.managementClassMu.RUnlock()
	if cached {
		if lookup, ok := classification.fom.(core.FOMTransportationLookup); ok {
			tt, _ := lookup.TransportationForInteraction(cls)
			return tt
		}
		return 0
	}
	if r.opts.FOMs != nil {
		if fom, err := r.opts.FOMs.Get(ctx, fed); err == nil {
			if lookup, ok := fom.(core.FOMTransportationLookup); ok {
				tt, _ := lookup.TransportationForInteraction(cls)
				return tt
			}
		}
	}
	return 0
}

func (r *RegistryOf[T]) classifyAttributeDeliveries(ctx context.Context, fed core.FederationName, inst *objectInstance, attrs []core.AttributeHandle, ts *T, observe func(core.TransportType), captured ...map[core.AttributeHandle]core.AttributeRegionDeliverySnapshot) (map[core.AttributeHandle]attributeDeliveryKey, map[core.AttributeHandle][]core.DDMRegionHandleCore, bool) {
	keys := make(map[core.AttributeHandle]attributeDeliveryKey, len(attrs))
	var regionSets map[core.AttributeHandle][]core.DDMRegionHandleCore
	if r.opts.DDM != nil {
		regionSets = make(map[core.AttributeHandle][]core.DDMRegionHandleCore, len(attrs))
	}
	timestamped := false
	for _, attr := range attrs {
		keys[attr] = attributeDeliveryKey{r.attributeDeliveryTime(fed, inst, attr, ts) != nil, r.attributeTransportation(ctx, fed, inst, attr)}
		if observe != nil {
			observe(keys[attr].transport)
		}
		timestamped = timestamped || keys[attr].timestamp
		if r.opts.DDM != nil {
			var regions []core.DDMRegionHandleCore
			if len(captured) > 0 && captured[0] != nil {
				regions = slices.Clone(captured[0][attr].Regions)
			} else {
				for _, region := range r.opts.DDM.PublisherRegionsFor(fed, inst.handle, attr) {
					regions = append(regions, core.DDMRegionHandleCore(region))
				}
			}
			slices.Sort(regions)
			regionSets[attr] = slices.Compact(regions)
		}
	}
	return keys, regionSets, timestamped
}

func appendSubscriberAttributeDeliveries[T core.LogicalTimeScalar](out []attributeDeliveryOf[T], sub core.FederateHandle, wire map[uint64][]byte, attrs []core.AttributeHandle, perAttr map[core.AttributeHandle][]core.FederateHandle, ts *T, keys map[core.AttributeHandle]attributeDeliveryKey, regionSets map[core.AttributeHandle][]core.DDMRegionHandleCore, reservableMessages bool, captured ...map[core.AttributeHandle]core.AttributeRegionDeliverySnapshot) []attributeDeliveryOf[T] {
	groups := make(map[attributeDeliveryKey][]int)
	for _, attr := range attrs {
		if perAttr != nil && !slices.Contains(perAttr[attr], sub) {
			continue
		}
		k := keys[attr]
		// Queued RO callbacks must retain one region set per attribute group
		// for release-time filtering. Preserve legacy and TSO callback grouping.
		splitRegions := reservableMessages && !k.timestamp
		i := -1
		for _, candidate := range groups[k] {
			if !splitRegions || slices.Equal(out[candidate].regions, regionSets[attr]) {
				i = candidate
				break
			}
		}
		if i < 0 {
			i = len(out)
			groups[k] = append(groups[k], i)
			part := attributeDeliveryOf[T]{subscriber: sub, attributes: make(map[uint64][]byte), transport: k.transport}
			if splitRegions {
				part.regions = regionSets[attr]
			}
			if k.timestamp {
				part.time = copiedLogicalTimeOf[T](ts)
			}
			out = append(out, part)
		}
		out[i].attributes[uint64(attr)] = wire[uint64(attr)]
		out[i].addAttributeConveyedRegions(attr, captured...)
		if !splitRegions {
			out[i].regions = append(out[i].regions, regionSets[attr]...)
		}
	}
	return out
}

func (p *attributeDeliveryOf[T]) addAttributeConveyedRegions(attr core.AttributeHandle, captured ...map[core.AttributeHandle]core.AttributeRegionDeliverySnapshot) {
	if len(captured) > 0 && captured[0] != nil && captured[0][attr].Copies != nil {
		copies := captured[0][attr].Copies
		p.conveyed = appendConveyedCopies(p.conveyed, copies)
		if p.attributeConveyed == nil {
			p.attributeConveyed = make(map[core.AttributeHandle][]core.ConveyedRegionCopy)
		}
		p.attributeConveyed[attr] = slices.Clone(copies)
	}
}
