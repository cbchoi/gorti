package object

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type momPointAttributeRouting interface {
	SubscribersForObjectAttributePoint(context.Context, core.FederationName, core.ObjectClassHandle, core.AttributeHandle, core.DDMDimensionHandle, uint64) ([]core.FederateHandle, error)
}

var _ core.MOMPointAttributeDelivery = (*Registry)(nil)
var _ core.MOMPointAttributeDelivery = (*IntegerRegistry)(nil)

func (r *RegistryOf[T]) PublishMOMAttributePoint(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs map[core.AttributeHandle][]byte, point core.MOMAttributePoint) error {
	return r.deliverMOMAttributePoint(ctx, fed, 0, obj, cls, attrs, point)
}

func (r *RegistryOf[T]) ReflectMOMAttributePoint(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs map[core.AttributeHandle][]byte, point core.MOMAttributePoint) error {
	if recipient == 0 {
		return core.ErrFederateNotJoined
	}
	return r.deliverMOMAttributePoint(ctx, fed, recipient, obj, cls, attrs, point)
}

func (r *RegistryOf[T]) momPointSubscribers(ctx context.Context, fed core.FederationName, cls core.ObjectClassHandle, attr core.AttributeHandle, point core.MOMAttributePoint) ([]core.FederateHandle, error) {
	subs := slices.Clone(r.objectSubscribers(ctx, fed, cls, []core.AttributeHandle{attr}))
	routing, ok := r.opts.RegistrationRegions.(momPointAttributeRouting)
	if !ok {
		routing, ok = r.opts.DDM.(momPointAttributeRouting)
	}
	if ok {
		regional, err := routing.SubscribersForObjectAttributePoint(ctx, fed, cls, attr, point.Dimension, point.Coordinate)
		if err != nil {
			return nil, err
		}
		subs = append(subs, regional...)
	} else if r.opts.DDM != nil {
		return nil, fmt.Errorf("object: MOM point routing unavailable")
	}
	slices.Sort(subs)
	return slices.Compact(subs), nil
}

func (r *RegistryOf[T]) deliverMOMAttributePoint(ctx context.Context, fed core.FederationName, target core.FederateHandle, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs map[core.AttributeHandle][]byte, point core.MOMAttributePoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if point.Subject == 0 || point.Subject == internalMOMProducer || point.Dimension == 0 || point.Coordinate != uint64(point.Subject)&math.MaxUint32 {
		return core.ErrInvalidRegionContext
	}
	if _, err := r.opts.FOMs.Get(ctx, fed); err != nil {
		return err
	}
	st := r.stateFor(fed)
	st.mu.Lock()
	inst := st.instances[obj]
	st.mu.Unlock()
	if inst == nil {
		return core.ErrObjectNotFound
	}
	inst.deliveryMu.Lock()
	p := &preparedAttributeFanoutOf[T]{instance: inst, class: cls, registry: r, state: st, fed: fed, producer: internalMOMProducer, deliveryLocked: true}
	defer p.abort()
	actuals, err := p.validateMOMPointObject(ctx, cls, attrs)
	if err != nil {
		return err
	}
	classes := r.recipientClasses(ctx, fed, inst, actuals...)
	byRecipient, perAttr, err := r.routeMOMPointAttributes(ctx, fed, target, cls, attrs, actuals, classes, point)
	if err != nil {
		return err
	}
	recipients := make([]core.FederateHandle, 0, len(byRecipient))
	for recipient := range byRecipient {
		recipients = append(recipients, recipient)
	}
	slices.Sort(recipients)
	p.prepareMOMPointKnowledge(target, actuals, perAttr, recipients, classes)
	parts := p.momPointParts(ctx, recipients, byRecipient)
	seq := p.appendMOMPointAdvisories(ctx, classes, len(parts))
	messages := p.momPointMessages(ctx, seq, parts, classes, point)
	if err := p.reserveMOMPointCallbacks(ctx, messages); err != nil {
		return err
	}
	return p.commit(ctx)
}

// FilterMOMAttributePointMessage is composed before the general queued-message
// filter. A portable MOM point never falls through to default-region matching.
func (r *RegistryOf[T]) FilterMOMAttributePointMessage(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, event core.OutboundEvent) (core.OutboundEvent, bool) {
	e, ok := event.(*CallbackEventOf[T])
	if !ok || e == nil || e.Kind != CallbackReflect || e.Metadata == nil || e.Metadata.MOMAttributePoint == nil {
		return event, false
	}
	point := *e.Metadata.MOMAttributePoint
	if point.Subject == 0 || point.Subject == internalMOMProducer || point.Dimension == 0 || point.Coordinate != uint64(point.Subject)&math.MaxUint32 {
		return nil, true
	}
	attrs := make(map[uint64][]byte, len(e.Attributes))
	for handle, value := range e.Attributes {
		subs, err := r.momPointSubscribers(ctx, fed, e.ObjectClass, core.AttributeHandle(handle), point)
		if err != nil {
			return nil, true
		}
		if slices.Contains(subs, recipient) {
			attrs[handle] = value
		}
	}
	if len(attrs) == 0 {
		return nil, true
	}
	filtered := *e
	filtered.Attributes = attrs
	return &filtered, true
}

func (p *preparedAttributeFanoutOf[T]) validateMOMPointObject(ctx context.Context, cls core.ObjectClassHandle, attrs map[core.AttributeHandle][]byte) ([]core.AttributeHandle, error) {
	r, st, inst, fed, obj := p.registry, p.state, p.instance, p.fed, p.instance.handle
	st.mu.Lock()
	live := st.instances[obj] == inst
	_, deleting := st.deletions[obj]
	st.mu.Unlock()
	if !live {
		return nil, core.ErrObjectNotFound
	}
	if deleting {
		return nil, core.ErrObjectDeletionPending
	}
	if inst.cls != cls {
		return nil, core.ErrObjectClassNotFound
	}
	actuals := make([]core.AttributeHandle, 0, len(attrs))
	for attr := range attrs {
		if !slices.Contains(r.classAttributes(ctx, fed, cls), attr) {
			return nil, core.ErrAttributeNotFound
		}
		actuals = append(actuals, attr)
	}
	slices.Sort(actuals)
	return actuals, nil
}

func (r *RegistryOf[T]) routeMOMPointAttributes(ctx context.Context, fed core.FederationName, target core.FederateHandle, cls core.ObjectClassHandle, attrs map[core.AttributeHandle][]byte, actuals []core.AttributeHandle, classes map[core.FederateHandle]core.ObjectClassHandle, point core.MOMAttributePoint) (map[core.FederateHandle]map[core.AttributeHandle][]byte, map[core.AttributeHandle][]core.FederateHandle, error) {
	perAttr := make(map[core.AttributeHandle][]core.FederateHandle, len(actuals))
	byRecipient := make(map[core.FederateHandle]map[core.AttributeHandle][]byte)
	for _, actual := range actuals {
		for recipient, known := range classes {
			if recipient == internalMOMProducer || target != 0 && recipient != target {
				continue
			}
			mapped, found := r.attributeAtClass(ctx, fed, cls, known, actual)
			if !found {
				continue
			}
			subs, err := r.momPointSubscribers(ctx, fed, known, mapped, point)
			if err != nil {
				return nil, nil, err
			}
			if !slices.Contains(subs, recipient) {
				continue
			}
			if byRecipient[recipient] == nil {
				byRecipient[recipient] = make(map[core.AttributeHandle][]byte)
			}
			byRecipient[recipient][actual] = slices.Clone(attrs[actual])
			perAttr[actual] = append(perAttr[actual], recipient)
		}
	}
	return byRecipient, perAttr, nil
}

func (p *preparedAttributeFanoutOf[T]) prepareMOMPointKnowledge(target core.FederateHandle, actuals []core.AttributeHandle, perAttr map[core.AttributeHandle][]core.FederateHandle, recipients []core.FederateHandle, classes map[core.FederateHandle]core.ObjectClassHandle) {
	st, obj := p.state, p.instance.handle
	p.discoveryClasses = make(map[discoverKey]core.ObjectClassHandle)
	st.mu.Lock()
	for _, recipient := range recipients {
		key := discoverKey{sub: recipient, obj: obj}
		if st.discovered[key] == 0 {
			p.discoveries = append(p.discoveries, key)
			p.discoveryClasses[key] = classes[recipient]
		}
	}
	if target == 0 {
		p.scope = prepareAttributeScopeLockedOf(st, internalMOMProducer, obj, actuals, perAttr)
	}
	st.mu.Unlock()
}

func (p *preparedAttributeFanoutOf[T]) momPointParts(ctx context.Context, recipients []core.FederateHandle, byRecipient map[core.FederateHandle]map[core.AttributeHandle][]byte) []attributeDeliveryOf[T] {
	r, fed, inst := p.registry, p.fed, p.instance
	var parts []attributeDeliveryOf[T]
	for _, recipient := range recipients {
		wire := make(map[uint64][]byte, len(byRecipient[recipient]))
		selected := make([]core.AttributeHandle, 0, len(wire))
		for attr, value := range byRecipient[recipient] {
			wire[uint64(attr)] = value
			selected = append(selected, attr)
		}
		slices.Sort(selected)
		partsForRecipient, _ := r.attributeDeliveries(ctx, fed, internalMOMProducer, inst, wire, selected, []core.FederateHandle{recipient}, nil, nil)
		parts = append(parts, partsForRecipient...)
	}
	return parts
}

func (p *preparedAttributeFanoutOf[T]) appendMOMPointAdvisories(ctx context.Context, classes map[core.FederateHandle]core.ObjectClassHandle, nParts int) uint64 {
	r, fed, st, inst, obj, cls := p.registry, p.fed, p.state, p.instance, p.instance.handle, p.class
	advisories := make(map[core.FederateHandle]bool)
	nSeq := len(p.discoveries) + nParts
	if p.scope != nil {
		for _, group := range []map[core.FederateHandle][]core.AttributeHandle{p.scope.newlyOut, p.scope.newlyIn} {
			for h := range group {
				advisories[h] = r.attributeScopeAdvisoryEnabled(fed, h)
				if advisories[h] {
					nSeq++
				}
			}
		}
	}
	st.mu.Lock()
	seq := st.nextOutboundSeqRangeLocked(nSeq)
	st.mu.Unlock()
	for _, key := range p.discoveries {
		p.appendImmediate(core.OutboxDelivery{Recipient: key.sub, Event: &CallbackEventOf[T]{Sequence: seq, Kind: CallbackDiscover, Object: obj, ObjectClass: classes[key.sub], ObjectName: inst.name, Metadata: metadataForOf[T](internalMOMProducer, nil, 0)}})
		seq++
	}
	if p.scope != nil {
		for i, group := range []map[core.FederateHandle][]core.AttributeHandle{p.scope.newlyOut, p.scope.newlyIn} {
			kind := CallbackAttributesOutOfScope
			if i == 1 {
				kind = CallbackAttributesInScope
			}
			for _, h := range sortedAdvisoryRecipients(group) {
				if !advisories[h] {
					continue
				}
				p.appendImmediate(core.OutboxDelivery{Recipient: h, Event: &CallbackEventOf[T]{Sequence: seq, Kind: kind, Object: obj, AttributeHandles: r.projectAttributes(ctx, fed, cls, classes[h], group[h])}})
				seq++
			}
		}
	}
	return seq
}

func (p *preparedAttributeFanoutOf[T]) momPointMessages(ctx context.Context, seq uint64, parts []attributeDeliveryOf[T], classes map[core.FederateHandle]core.ObjectClassHandle, point core.MOMAttributePoint) []core.MessageDeliveryOf[T] {
	r, fed, obj, cls := p.registry, p.fed, p.instance.handle, p.class
	messages := make([]core.MessageDeliveryOf[T], 0, len(parts))
	for _, part := range parts {
		projected := make(map[uint64][]byte, len(part.attributes))
		for actual, value := range part.attributes {
			if mapped, ok := r.attributeAtClass(ctx, fed, cls, classes[part.subscriber], core.AttributeHandle(actual)); ok {
				projected[uint64(mapped)] = value
			}
		}
		event := newReflectEventOf[T](seq, obj, classes[part.subscriber], projected, nil)
		event.Metadata = metadataForOf[T](internalMOMProducer, nil, part.transport)
		pointCopy := point
		event.Metadata.MOMAttributePoint = &pointCopy
		messages = append(messages, messageDeliveryOf[T](part.subscriber, internalMOMProducer, nil, event, 0))
		p.reflectSubs = append(p.reflectSubs, part.subscriber)
		seq++
	}
	return messages
}

func (p *preparedAttributeFanoutOf[T]) reserveMOMPointCallbacks(ctx context.Context, messages []core.MessageDeliveryOf[T]) error {
	r, fed := p.registry, p.fed
	if _, ok := r.opts.TSOGate.(core.ReservableTSODeliveryGateOf[T]); ok {
		var err error
		p.tsoReservation, err = r.reserveOutgoingTSO(fed, internalMOMProducer, nil, messages)
		if err != nil {
			return err
		}
		if p.tsoReservation == nil {
			return fmt.Errorf("object: MOM point time reservation is nil")
		}
		for _, message := range p.tsoReservation.Immediate() {
			filtered, _ := r.FilterMOMAttributePointMessage(ctx, fed, message.Recipient, message.Event)
			if filtered != nil {
				p.appendImmediate(core.OutboxDelivery{Recipient: message.Recipient, Event: filtered})
			}
		}
	} else {
		for _, message := range messages {
			filtered, _ := r.FilterMOMAttributePointMessage(ctx, fed, message.Recipient, message.Event)
			if filtered != nil {
				p.appendImmediate(core.OutboxDelivery{Recipient: message.Recipient, Event: filtered})
			}
		}
	}
	if len(p.immediate) > 0 {
		reservable, ok := r.opts.Outbox.(core.ReservableOutbox)
		if !ok {
			return fmt.Errorf("object: MOM point callbacks require reservable outbox")
		}
		var err error
		p.reservation, err = reservable.Reserve(ctx, fed, p.immediate)
		if err != nil {
			return err
		}
		if p.reservation == nil {
			return fmt.Errorf("object: MOM point outbox reservation is nil")
		}
	}
	return nil
}
