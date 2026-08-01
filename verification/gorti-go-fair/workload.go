package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

const (
	sequenceField      = "Sequence"
	payloadField       = "Payload"
	publisherName      = "CommercialRtiVerifierPublisher"
	subscriberName     = "CommercialRtiVerifierSubscriber"
	readySync          = "VERIFY_READY"
	measureSync        = "VERIFY_MEASURE"
	startSync          = "VERIFY_START"
	doneSync           = "VERIFY_DONE"
	benchmarkLookahead = 1.0
)

type encodedIteration struct {
	index       int
	time        float64
	attribute   string
	interaction string
	attributes  map[string][]byte
	parameters  map[string][]byte
	wireAttrs   map[uint64][]byte
	wireParams  map[uint64][]byte
}

func preencodeWorkload(seed string, count int) ([]encodedIteration, error) {
	return preencodeWorkloadRange(seed, 0, count)
}

func preencodeWorkloadRange(seed string, start, count int) ([]encodedIteration, error) {
	if count < 0 || start < 0 {
		return nil, errors.New("workload range must not be negative")
	}
	if start == 0 && count < 1 {
		return nil, errors.New("count must be at least 1")
	}
	if count > 0 && uint64(start+count-1) > uint64(math.MaxInt32) {
		return nil, errors.New("count exceeds HLAinteger32BE sequence range")
	}
	result := make([]encodedIteration, count)
	for offset := range result {
		index := start + offset
		sequence, err := encodeInteger32BE(index)
		if err != nil {
			return nil, err
		}
		attribute := deterministicPayload(seed, "attribute", index)
		interaction := deterministicPayload(seed, "interaction", index)
		attributeBytes, err := encodeASCIIString(attribute)
		if err != nil {
			return nil, err
		}
		interactionBytes, err := encodeASCIIString(interaction)
		if err != nil {
			return nil, err
		}
		result[offset] = encodedIteration{
			index: index, time: float64(index + 1),
			attribute: attribute, interaction: interaction,
			attributes: map[string][]byte{sequenceField: sequence, payloadField: attributeBytes},
			parameters: map[string][]byte{sequenceField: sequence, payloadField: interactionBytes},
		}
	}
	return result, nil
}

type trafficFederate interface {
	UpdateAttributeValuesByHandle(context.Context, uint64, map[uint64][]byte, *float64) error
	SendInteractionByHandle(context.Context, uint64, map[uint64][]byte, *float64) error
	TimeAdvanceRequest(context.Context, float64) error
	DeleteObjectInstance(context.Context, uint64, []byte, *float64) error
}

type localLRCTrafficFederate interface {
	QueueAttributeValuesByHandle(context.Context, uint64, map[uint64][]byte) (uint64, error)
	QueueInteractionByHandle(context.Context, uint64, map[uint64][]byte) (uint64, error)
	FlushLocalLRC(context.Context) error
}

type participant struct {
	cfg                    config
	log                    *runLog
	payloads               []encodedIteration
	warmupPayloads         []encodedIteration
	conn                   *federate.Connection
	fed                    *federate.Federate
	state                  *eventState
	pumpDone               chan struct{}
	interactionClassHandle uint64
	objectHandle           uint64
	completed              bool
}

type compactObjectReadyActions struct {
	registerPublisher        func(context.Context) error
	awaitSubscriberDiscovery func(context.Context) error
	validate                 func() error
}

type readyPhaseActions struct {
	prepareCompactObject    func(context.Context) error
	synchronize             func(context.Context, string, bool) error
	registerPublisherObject func(context.Context) error
	publishWarmup           func(context.Context) error
	waitForWarmup           func(context.Context) error
}

func (p *participant) run(ctx context.Context, fom []byte) (runErr error) {
	if err := p.connectAndJoin(ctx, fom); err != nil {
		return err
	}
	defer func() {
		if err := p.shutdown(); runErr == nil {
			runErr = err
		}
	}()
	if err := p.prepareWireHandles(); err != nil {
		return err
	}
	if err := p.declare(ctx); err != nil {
		return err
	}
	if !p.cfg.ReceiveOrder {
		if err := p.enableTime(ctx); err != nil {
			return err
		}
	}
	participants, err := p.awaitParticipants(ctx)
	if err != nil {
		return err
	}
	if err := p.state.setParticipants(participants); err != nil {
		return err
	}
	if err := p.coordinateReadyAndWarmup(ctx); err != nil {
		return err
	}

	if p.cfg.Role == rolePublisher && p.objectHandle == 0 && !p.cfg.TMAdvanceOnly {
		if err := p.registerPublisherObject(ctx); err != nil {
			return err
		}
	}
	if p.cfg.ReceiveOrder && p.cfg.devstoneMode() {
		if err := p.synchronize(ctx, startSync, p.cfg.registersSynchronization(startSync)); err != nil {
			return err
		}
	}
	if !p.cfg.ReceiveOrder {
		if p.cfg.Role == rolePublisher && !p.cfg.TMAdvanceOnly {
			if err := p.publishTraffic(ctx, p.fed, p.objectHandle); err != nil {
				return err
			}
		}
		if err := p.synchronize(ctx, measureSync, p.cfg.registersSynchronization(measureSync)); err != nil {
			return err
		}
		if p.cfg.TMAdvanceOnly {
			if err := p.advanceOnly(ctx, p.fed); err != nil {
				return err
			}
		} else if p.cfg.Role == rolePublisher {
			if err := p.advancePublishedTimestampOrderTraffic(ctx, p.fed, p.objectHandle); err != nil {
				return err
			}
		} else if err := p.subscribe(ctx); err != nil {
			return err
		}
	} else if p.cfg.Role == rolePublisher {
		if err := p.publishTraffic(ctx, p.fed, p.objectHandle); err != nil {
			return err
		}
	} else if err := p.subscribe(ctx); err != nil {
		return err
	}

	if err := p.synchronize(ctx, doneSync, p.cfg.registersSynchronization(doneSync)); err != nil {
		return err
	}
	if p.cfg.ReceiveOrder {
		if err := p.completeReceiveOrderRemoval(ctx); err != nil {
			return err
		}
	}
	if err := p.log.event("FM", "phase", map[string]any{"phase": "do", "status": "complete"}); err != nil {
		return err
	}
	if err := p.log.event("FM", "phase", map[string]any{"phase": "review", "status": "start"}); err != nil {
		return err
	}
	if err := p.review(); err != nil {
		return err
	}
	if err := p.log.event("FM", "phase", map[string]any{
		"count": p.cfg.Count, "phase": "review", "status": "complete",
	}); err != nil {
		return err
	}
	p.completed = true
	if p.cfg.Role == roleSubscriber && !p.cfg.CompactSummary {
		return writeProjection(p.cfg, p.payloads)
	}
	return nil
}

func (p *participant) advanceOnly(ctx context.Context, fed trafficFederate) error {
	for logicalTime := 1; logicalTime <= p.cfg.Count; logicalTime++ {
		if err := p.advance(ctx, fed, float64(logicalTime), true); err != nil {
			return err
		}
	}
	return nil
}

func (p *participant) coordinateReadyAndWarmup(ctx context.Context) error {
	return executeReadyPhase(ctx, p.cfg, readyPhaseActions{
		prepareCompactObject:    p.prepareCompactReceiveOrderReady,
		synchronize:             p.synchronize,
		registerPublisherObject: p.registerPublisherObject,
		publishWarmup:           p.publishWarmup,
		waitForWarmup:           p.waitForWarmup,
	})
}

func executeReadyPhase(ctx context.Context, cfg config, actions readyPhaseActions) error {
	if err := actions.prepareCompactObject(ctx); err != nil {
		return err
	}
	if err := actions.synchronize(ctx, readySync, cfg.registersSynchronization(readySync)); err != nil {
		return err
	}
	if cfg.OperationWarmup > 0 {
		if cfg.Role == rolePublisher {
			if !cfg.compactReceiveOrderWorkload() {
				if err := actions.registerPublisherObject(ctx); err != nil {
					return err
				}
			}
			if err := actions.publishWarmup(ctx); err != nil {
				return err
			}
		} else if err := actions.waitForWarmup(ctx); err != nil {
			return err
		}
		return actions.synchronize(ctx, measureSync, cfg.registersSynchronization(measureSync))
	}
	if cfg.ReceiveOrder && cfg.devstoneMode() {
		return actions.synchronize(ctx, measureSync, cfg.registersSynchronization(measureSync))
	}
	return nil
}

func (p *participant) prepareCompactReceiveOrderReady(ctx context.Context) error {
	return executeCompactObjectReady(ctx, p.cfg, compactObjectReadyActions{
		registerPublisher:        p.registerPublisherObject,
		awaitSubscriberDiscovery: p.awaitPublisherObjectDiscovery,
		validate:                 p.validateCompactReceiveOrderReady,
	})
}

func executeCompactObjectReady(ctx context.Context, cfg config, actions compactObjectReadyActions) error {
	if !cfg.compactReceiveOrderWorkload() {
		return nil
	}
	if cfg.Role == rolePublisher {
		if err := actions.registerPublisher(ctx); err != nil {
			return err
		}
	} else if err := actions.awaitSubscriberDiscovery(ctx); err != nil {
		return err
	}
	return actions.validate()
}

func (p *participant) awaitPublisherObjectDiscovery(ctx context.Context) error {
	return p.state.wait(ctx, p.cfg.Timeout, "publisher object discovery before "+readySync, func(s *eventState) bool {
		return s.validatedObjectDiscoveryLocked()
	})
}

func (p *participant) validateCompactReceiveOrderReady() error {
	if !p.cfg.compactReceiveOrderWorkload() {
		return nil
	}
	if p.cfg.Role == rolePublisher {
		if p.objectHandle == 0 {
			return fmt.Errorf("publisher object must be registered before %s", readySync)
		}
		return nil
	}
	if !p.state.validatedObjectDiscovery() {
		return fmt.Errorf("subscriber must discover the publisher object before %s", readySync)
	}
	return nil
}

func (p *participant) connectAndJoin(ctx context.Context, fom []byte) error {
	var err error
	receiveOrderTransport := federate.ReceiveOrderTransportConfirmed
	if p.cfg.LocalLRC {
		receiveOrderTransport = federate.ReceiveOrderTransportLocalLRC
	}
	callbackRepresentation := federate.CallbackRepresentationNames
	if p.cfg.CallbackRepresentation == "handles" {
		callbackRepresentation = federate.CallbackRepresentationHandles
	}
	if err = p.log.timed("FM", "connect", func() error {
		p.conn, err = federate.ConnectWithOptions(ctx, p.cfg.Address, federate.ConnectOptions{
			LocalLRCQueueCapacity:  p.cfg.LocalLRCQueueCapacity,
			LocalLRCAckEvery:       uint32(p.cfg.LocalLRCAckEvery),
			LocalLRCBatchSize:      uint32(p.cfg.LocalLRCBatchSize),
			CallbackRepresentation: callbackRepresentation,
			ReceiveOrderTransport:  receiveOrderTransport,
		})
		return err
	}); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	if err := p.log.event("FM", "connected", map[string]any{"transport": "tcp"}); err != nil {
		return err
	}
	name := p.cfg.federateName()
	spec := federate.FederationSpec{
		Name:       p.cfg.Federation,
		FOMModules: []federate.FOMModule{{Path: p.cfg.FOMPath, XML: fom}},
		Seed:       federationSeed(p.cfg.Seed),
	}
	if err := p.log.timed("FM", "join_federation_execution", func() error {
		callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
		defer cancel()
		p.fed, err = p.conn.JoinFederation(callCtx, spec, name)
		return err
	}); err != nil {
		return fmt.Errorf("join federation: %w", err)
	}
	p.state, err = newEventStateWithPayloads(p.cfg, p.payloads)
	if err != nil {
		return err
	}
	p.state.setSelf(p.fed.Handle())
	p.pumpDone = make(chan struct{})
	go p.pumpEvents()
	return p.log.event("FM", "joined", map[string]any{"federate_type": "GortiGoFair-" + string(p.cfg.Role)})
}

func (p *participant) prepareWireHandles() error {
	objectClassHandle, ok := p.fed.ObjectClassHandle(p.cfg.ObjectClass)
	if !ok {
		return fmt.Errorf("object class %q not in FOM", p.cfg.ObjectClass)
	}
	sequenceAttributeHandle, ok := p.fed.AttributeHandle(objectClassHandle, sequenceField)
	if !ok {
		return fmt.Errorf("attribute %q not in object class %q", sequenceField, p.cfg.ObjectClass)
	}
	payloadAttributeHandle, ok := p.fed.AttributeHandle(objectClassHandle, payloadField)
	if !ok {
		return fmt.Errorf("attribute %q not in object class %q", payloadField, p.cfg.ObjectClass)
	}
	classHandle, ok := p.fed.InteractionClassHandle(p.cfg.InteractionClass)
	if !ok {
		return fmt.Errorf("interaction class %q not in FOM", p.cfg.InteractionClass)
	}
	sequenceHandle, ok := p.fed.ParameterHandle(classHandle, sequenceField)
	if !ok {
		return fmt.Errorf("parameter %q not in interaction class %q", sequenceField, p.cfg.InteractionClass)
	}
	payloadHandle, ok := p.fed.ParameterHandle(classHandle, payloadField)
	if !ok {
		return fmt.Errorf("parameter %q not in interaction class %q", payloadField, p.cfg.InteractionClass)
	}
	p.interactionClassHandle = classHandle
	p.state.setDeliveryHandles(callbackHandleSet{
		objectClass:         objectClassHandle,
		attributeSequence:   sequenceAttributeHandle,
		attributePayload:    payloadAttributeHandle,
		interactionClass:    classHandle,
		interactionSequence: sequenceHandle,
		interactionPayload:  payloadHandle,
	})
	if p.cfg.Role != rolePublisher {
		return nil
	}
	for _, workload := range [][]encodedIteration{p.payloads, p.warmupPayloads} {
		for index := range workload {
			item := &workload[index]
			item.wireAttrs = map[uint64][]byte{
				sequenceAttributeHandle: item.attributes[sequenceField],
				payloadAttributeHandle:  item.attributes[payloadField],
			}
			item.wireParams = map[uint64][]byte{
				sequenceHandle: item.parameters[sequenceField],
				payloadHandle:  item.parameters[payloadField],
			}
		}
	}
	return nil
}

func (p *participant) declare(ctx context.Context) error {
	if p.cfg.Role == rolePublisher {
		if err := p.log.timed("DM", "publish_object_class_attributes", func() error {
			return p.call(ctx, func(callCtx context.Context) error {
				return p.fed.PublishObjectClassAttributes(callCtx, p.cfg.ObjectClass, []string{sequenceField, payloadField})
			})
		}); err != nil {
			return err
		}
		if err := p.log.event("DM", "object_published", map[string]any{"class": p.cfg.ObjectClass}); err != nil {
			return err
		}
		if err := p.log.timed("DM", "publish_interaction_class", func() error {
			return p.call(ctx, func(callCtx context.Context) error {
				return p.fed.PublishInteractionClass(callCtx, p.cfg.InteractionClass)
			})
		}); err != nil {
			return err
		}
		return p.log.event("DM", "interaction_published", map[string]any{"class": p.cfg.InteractionClass})
	}
	if err := p.log.timed("DM", "subscribe_object_class_attributes", func() error {
		return p.call(ctx, func(callCtx context.Context) error {
			return p.fed.SubscribeObjectClassAttributes(callCtx, p.cfg.ObjectClass, []string{sequenceField, payloadField})
		})
	}); err != nil {
		return err
	}
	if err := p.log.event("DM", "object_subscribed", map[string]any{"class": p.cfg.ObjectClass}); err != nil {
		return err
	}
	if err := p.log.timed("DM", "subscribe_interaction_class", func() error {
		return p.call(ctx, func(callCtx context.Context) error {
			return p.fed.SubscribeInteractionClass(callCtx, p.cfg.InteractionClass)
		})
	}); err != nil {
		return err
	}
	return p.log.event("DM", "interaction_subscribed", map[string]any{"class": p.cfg.InteractionClass})
}

func (p *participant) enableTime(ctx context.Context) error {
	if err := p.log.timed("TM", "enable_time_regulation", func() error {
		return p.call(ctx, func(callCtx context.Context) error {
			return p.fed.EnableTimeRegulation(callCtx, benchmarkLookahead)
		})
	}); err != nil {
		return err
	}
	if err := p.log.event("TM", "time_regulation_enabled", map[string]any{"lookahead": 1}); err != nil {
		return err
	}
	if err := p.log.timed("TM", "enable_time_constrained", func() error {
		return p.call(ctx, p.fed.EnableTimeConstrained)
	}); err != nil {
		return err
	}
	return p.log.event("TM", "time_constrained_enabled", map[string]any{})
}

func (p *participant) awaitParticipants(ctx context.Context) ([]uint64, error) {
	waitCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()
	for {
		members, err := p.fed.ListFederationMembers(waitCtx)
		if err != nil {
			return nil, err
		}
		byName := make(map[string]uint64, len(members))
		for _, member := range members {
			byName[member.Name] = member.Handle
		}
		participants := make([]uint64, 0, p.cfg.ParticipantCount)
		publisher, publisherOK := byName[publisherName]
		if publisherOK {
			participants = append(participants, publisher)
		}
		for index := 1; index < p.cfg.ParticipantCount; index++ {
			handle, ok := byName[p.cfg.subscriberFederateName(index)]
			if ok {
				participants = append(participants, handle)
			}
		}
		if len(participants) == p.cfg.ParticipantCount {
			sort.Slice(participants, func(i, j int) bool { return participants[i] < participants[j] })
			return participants, nil
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-waitCtx.Done():
			return nil, fmt.Errorf("wait for %d federates: %w", p.cfg.ParticipantCount, waitCtx.Err())
		}
	}
}

func (p *participant) synchronize(ctx context.Context, label string, registrar bool) error {
	if registrar {
		if err := p.log.timed("FM", "register_synchronization_point", func() error {
			return p.call(ctx, func(callCtx context.Context) error {
				return p.fed.RegisterFederationSynchronizationPoint(
					callCtx, label, nil, p.state.participantHandles(),
				)
			})
		}); err != nil {
			return err
		}
		if err := p.log.event("FM", "synchronization_registered", map[string]any{
			"label": label, "participants": p.cfg.ParticipantCount,
		}); err != nil {
			return err
		}
	}
	if err := p.state.wait(ctx, p.cfg.Timeout, "synchronization announced "+label, func(s *eventState) bool {
		return s.announced[label]
	}); err != nil {
		return err
	}
	if err := p.log.event("FM", "synchronization_announced", map[string]any{"label": label}); err != nil {
		return err
	}
	if label == startSync && p.cfg.Role == roleSubscriber {
		if err := p.state.armReceiveOrderBatch(counterNow()); err != nil {
			return err
		}
	}
	if err := p.log.timed("FM", "synchronization_point_achieved", func() error {
		return p.call(ctx, func(callCtx context.Context) error {
			return p.fed.SynchronizationPointAchieved(callCtx, label, true)
		})
	}); err != nil {
		return err
	}
	if err := p.log.event("FM", "synchronization_achieved", map[string]any{"label": label}); err != nil {
		return err
	}
	if err := p.state.wait(ctx, p.cfg.Timeout, "federation synchronized "+label, func(s *eventState) bool {
		return s.synchronized[label]
	}); err != nil {
		return err
	}
	return p.log.event("FM", "federation_synchronized", map[string]any{"label": label})
}

func (p *participant) publish(ctx context.Context) error {
	if err := p.registerPublisherObject(ctx); err != nil {
		return err
	}
	return p.publishTraffic(ctx, p.fed, p.objectHandle)
}

func (p *participant) registerPublisherObject(ctx context.Context) error {
	if err := p.log.timed("OM", "reserve_object_instance_name", func() error {
		return p.call(ctx, func(callCtx context.Context) error {
			return p.fed.ReserveObjectInstanceName(callCtx, p.cfg.ObjectName)
		})
	}); err != nil {
		return err
	}
	if err := p.state.wait(ctx, p.cfg.Timeout, "object instance name reservation", func(s *eventState) bool {
		return s.nameReserved
	}); err != nil {
		return err
	}
	if err := p.log.event("OM", "object_name_reserved", map[string]any{"name": p.cfg.ObjectName}); err != nil {
		return err
	}
	var objectHandle uint64
	if err := p.log.timed("OM", "register_object_instance", func() error {
		var err error
		callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
		defer cancel()
		objectHandle, err = p.fed.RegisterObjectInstance(callCtx, p.cfg.ObjectClass, p.cfg.ObjectName)
		return err
	}); err != nil {
		return err
	}
	if err := p.log.event("OM", "object_registered", map[string]any{
		"class": p.cfg.ObjectClass, "name": p.cfg.ObjectName,
	}); err != nil {
		return err
	}
	p.objectHandle = objectHandle
	return nil
}

func (p *participant) publishWarmup(ctx context.Context) error {
	for _, item := range p.warmupPayloads {
		callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
		err := p.fed.UpdateAttributeValuesByHandle(callCtx, p.objectHandle, item.wireAttrs, nil)
		cancel()
		if err != nil {
			return err
		}
		callCtx, cancel = context.WithTimeout(ctx, p.cfg.Timeout)
		err = p.fed.SendInteractionByHandle(callCtx, p.interactionClassHandle, item.wireParams, nil)
		cancel()
		if err != nil {
			return err
		}
	}
	if p.cfg.LocalLRC {
		callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
		defer cancel()
		return p.fed.FlushLocalLRC(callCtx)
	}
	return nil
}

func (p *participant) waitForWarmup(ctx context.Context) error {
	return p.state.wait(ctx, p.cfg.Timeout, "operation warmup callbacks", func(s *eventState) bool {
		return s.warmupComplete()
	})
}

func (p *participant) publishTraffic(ctx context.Context, fed trafficFederate, objectHandle uint64) error {
	if p.cfg.LocalLRC {
		local, ok := fed.(localLRCTrafficFederate)
		if !ok {
			return errors.New("LocalLRC workload requires queued transport support")
		}
		return p.publishLocalLRCTraffic(ctx, local, objectHandle)
	}
	for _, item := range p.payloads {
		var timestamp *float64
		if !p.cfg.ReceiveOrder {
			value := item.time
			timestamp = &value
		}
		callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
		err := p.log.benchmarkTimed("OM", "updateAttributeValues", "update_attribute_values", func() error {
			return fed.UpdateAttributeValuesByHandle(callCtx, objectHandle, item.wireAttrs, timestamp)
		})
		cancel()
		if err != nil {
			return err
		}
		if !p.cfg.CompactSummary {
			attributeEvent := map[string]any{"index": item.index, "payload": item.attribute}
			if p.cfg.ReceiveOrder {
				attributeEvent["order"] = "receive"
			} else {
				attributeEvent["logical_time"] = int(item.time)
			}
			if err := p.log.event("OM", "attributes_updated", attributeEvent); err != nil {
				return err
			}
		}
		callCtx, cancel = context.WithTimeout(ctx, p.cfg.Timeout)
		err = p.log.benchmarkTimed("OM", "sendInteraction", "send_interaction", func() error {
			return fed.SendInteractionByHandle(
				callCtx, p.interactionClassHandle, item.wireParams, timestamp,
			)
		})
		cancel()
		if err != nil {
			return err
		}
		if !p.cfg.CompactSummary {
			interactionEvent := map[string]any{"index": item.index, "payload": item.interaction}
			if p.cfg.ReceiveOrder {
				interactionEvent["order"] = "receive"
			} else {
				interactionEvent["logical_time"] = int(item.time)
			}
			if err := p.log.event("OM", "interaction_sent", interactionEvent); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *participant) advancePublishedTimestampOrderTraffic(
	ctx context.Context,
	fed trafficFederate,
	objectHandle uint64,
) error {
	for _, item := range p.payloads {
		if err := p.advance(ctx, fed, item.time, true); err != nil {
			return err
		}
	}
	removalTime := float64(p.cfg.Count + 1)
	if err := p.log.timed("OM", "delete_object_instance", func() error {
		return p.call(ctx, func(callCtx context.Context) error {
			return fed.DeleteObjectInstance(callCtx, objectHandle, nil, &removalTime)
		})
	}); err != nil {
		return err
	}
	if err := p.log.event("OM", "object_deleted", map[string]any{
		"logical_time": int(removalTime), "name": p.cfg.ObjectName,
	}); err != nil {
		return err
	}
	return p.advance(ctx, fed, removalTime, false)
}

func (p *participant) publishLocalLRCTraffic(
	ctx context.Context,
	fed localLRCTrafficFederate,
	objectHandle uint64,
) error {
	for _, item := range p.payloads {
		callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
		err := p.log.benchmarkTimed("OM", "queueAttributeValues", "queue_attribute_values", func() error {
			_, queueErr := fed.QueueAttributeValuesByHandle(callCtx, objectHandle, item.wireAttrs)
			return queueErr
		})
		cancel()
		if err != nil {
			return err
		}
		if !p.cfg.CompactSummary {
			if err := p.log.event("OM", "attributes_queued", map[string]any{
				"index": item.index, "order": "receive", "payload": item.attribute,
			}); err != nil {
				return err
			}
		}

		callCtx, cancel = context.WithTimeout(ctx, p.cfg.Timeout)
		err = p.log.benchmarkTimed("OM", "queueInteraction", "queue_interaction", func() error {
			_, queueErr := fed.QueueInteractionByHandle(callCtx, p.interactionClassHandle, item.wireParams)
			return queueErr
		})
		cancel()
		if err != nil {
			return err
		}
		if !p.cfg.CompactSummary {
			if err := p.log.event("OM", "interaction_queued", map[string]any{
				"index": item.index, "order": "receive", "payload": item.interaction,
			}); err != nil {
				return err
			}
		}
	}
	if err := p.log.benchmarkTimed("OM", "flushLocalLRC", "flush_local_lrc", func() error {
		callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
		defer cancel()
		return fed.FlushLocalLRC(callCtx)
	}); err != nil {
		return err
	}
	return p.log.event("OM", "local_lrc_flushed", map[string]any{
		"committed_operations": p.cfg.Count * 2,
	})
}

func (p *participant) subscribe(ctx context.Context) error {
	if p.cfg.ReceiveOrder {
		return p.subscribeReceiveOrder(ctx)
	}
	return p.subscribeTimestampOrder(ctx, p.fed)
}

func (p *participant) subscribeTimestampOrder(ctx context.Context, fed trafficFederate) error {
	sustainedStarted := counterNow()
	lastCompleted := sustainedStarted
	for _, item := range p.payloads {
		if err := p.state.prepareAdvance(item.time, item.index); err != nil {
			return err
		}
		callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
		batchStarted := counterNow()
		tarStarted := batchStarted
		err := fed.TimeAdvanceRequest(callCtx, item.time)
		cancel()
		tarDuration := counterElapsed(tarStarted)
		if metricErr := p.log.metric("TM", "call_latency.time_advance_request", "nanoseconds", float64(tarDuration)); metricErr != nil {
			return metricErr
		}
		if sampleErr := p.log.sample("timeAdvanceRequest", tarDuration, "TM", "call"); sampleErr != nil {
			return sampleErr
		}
		if err != nil {
			return err
		}
		if err := p.log.event("TM", "time_advance_requested", map[string]any{"logical_time": int(item.time)}); err != nil {
			return err
		}
		if err := p.state.wait(ctx, p.cfg.Timeout, fmt.Sprintf("delivery batch %d", item.index), func(s *eventState) bool {
			return s.reflections[item.index].accepted && s.interactions[item.index].accepted
		}); err != nil {
			return err
		}
		reflection, interaction := p.state.observations(item.index)
		batchCompleted := reflection.receivedAt
		if interaction.receivedAt.After(batchCompleted) {
			batchCompleted = interaction.receivedAt
		}
		lastCompleted = batchCompleted
		if err := p.log.metric("OM", "completed_delivery_batch_latency", "nanoseconds", float64(counterBetween(batchStarted, batchCompleted))); err != nil {
			return err
		}
		if err := p.log.sample("completed_delivery_batch_latency", counterBetween(batchStarted, batchCompleted), "OM", "delivery"); err != nil {
			return err
		}
		if item.index == 0 {
			if err := p.log.event("OM", "object_discovered", map[string]any{
				"class": p.cfg.ObjectClass, "name": p.cfg.ObjectName,
			}); err != nil {
				return err
			}
		}
		if err := p.log.event("OM", "attributes_reflected", map[string]any{
			"index": item.index, "logical_time": int(item.time), "payload": item.attribute,
		}); err != nil {
			return err
		}
		if err := p.log.event("OM", "interaction_received", map[string]any{
			"index": item.index, "logical_time": int(item.time), "payload": item.interaction,
		}); err != nil {
			return err
		}
		if err := p.awaitGrant(ctx, item.time, tarStarted, true); err != nil {
			return err
		}
		p.state.finishAdvance()
	}
	duration := counterBetween(sustainedStarted, lastCompleted)
	if duration < 1 {
		duration = 1
	}
	if err := p.log.metric("OM", "sustained_throughput", "deliveries_per_second",
		float64(p.cfg.Count*2)*1e9/float64(duration)); err != nil {
		return err
	}

	removalTime := float64(p.cfg.Count + 1)
	if err := p.advance(ctx, fed, removalTime, false); err != nil {
		return err
	}
	if err := p.state.wait(ctx, p.cfg.Timeout, "object removal", func(s *eventState) bool {
		return s.removed
	}); err != nil {
		return err
	}
	return p.log.event("OM", "object_removed", map[string]any{
		"logical_time": int(removalTime), "name": p.cfg.ObjectName,
	})
}

func (p *participant) subscribeReceiveOrder(ctx context.Context) error {
	batchStarted, err := p.state.receiveOrderBatchStart(counterNow())
	if err != nil {
		return err
	}
	lastCompleted := batchStarted
	for _, item := range p.payloads {
		if err := p.state.wait(ctx, p.cfg.Timeout, fmt.Sprintf("receive-order batch %d", item.index), func(s *eventState) bool {
			return s.reflections[item.index].accepted && s.interactions[item.index].accepted
		}); err != nil {
			return err
		}
		reflection, interaction := p.state.observations(item.index)
		if reflection.receivedAt.After(lastCompleted) {
			lastCompleted = reflection.receivedAt
		}
		if interaction.receivedAt.After(lastCompleted) {
			lastCompleted = interaction.receivedAt
		}
		if !p.cfg.CompactSummary {
			if item.index == 0 {
				if err := p.log.event("OM", "object_discovered", map[string]any{
					"class": p.cfg.ObjectClass, "name": p.cfg.ObjectName,
				}); err != nil {
					return err
				}
			}
			if err := p.log.event("OM", "attributes_reflected", map[string]any{
				"index": item.index, "order": "receive", "payload": item.attribute,
			}); err != nil {
				return err
			}
			if err := p.log.event("OM", "interaction_received", map[string]any{
				"index": item.index, "order": "receive", "payload": item.interaction,
			}); err != nil {
				return err
			}
		}
	}
	duration := counterBetween(batchStarted, lastCompleted)
	if duration < 1 {
		duration = 1
	}
	if err := p.log.metric("OM", "completed_receive_order_batch", "nanoseconds", float64(duration)); err != nil {
		return err
	}
	if err := p.log.sample("completedReceiveOrderBatch", duration, "OM", "delivery"); err != nil {
		return err
	}
	return p.log.metric("OM", "sustained_throughput", "deliveries_per_second",
		float64(p.cfg.Count*2)*1e9/float64(duration))
}

func (p *participant) completeReceiveOrderRemoval(ctx context.Context) error {
	if p.cfg.Role == rolePublisher {
		if err := p.log.timed("OM", "delete_object_instance", func() error {
			return p.call(ctx, func(callCtx context.Context) error {
				return p.fed.DeleteObjectInstance(callCtx, p.objectHandle, nil, nil)
			})
		}); err != nil {
			return err
		}
		return p.log.event("OM", "object_deleted", map[string]any{
			"name": p.cfg.ObjectName, "order": "receive",
		})
	}
	if err := p.state.wait(ctx, p.cfg.Timeout, "object removal", func(s *eventState) bool {
		return s.removed
	}); err != nil {
		return err
	}
	return p.log.event("OM", "object_removed", map[string]any{
		"name": p.cfg.ObjectName, "order": "receive",
	})
}

func (p *participant) advance(ctx context.Context, fed trafficFederate, logicalTime float64, sample bool) error {
	if err := p.state.prepareAdvance(logicalTime, -1); err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	started := counterNow()
	err := fed.TimeAdvanceRequest(callCtx, logicalTime)
	duration := counterElapsed(started)
	cancel()
	if metricErr := p.log.metric("TM", "call_latency.time_advance_request", "nanoseconds", float64(duration)); metricErr != nil {
		return metricErr
	}
	if sample {
		if sampleErr := p.log.sample("timeAdvanceRequest", duration, "TM", "call"); sampleErr != nil {
			return sampleErr
		}
	}
	if err != nil {
		return err
	}
	if err := p.log.event("TM", "time_advance_requested", map[string]any{"logical_time": int(logicalTime)}); err != nil {
		return err
	}
	if err := p.awaitGrant(ctx, logicalTime, started, sample); err != nil {
		return err
	}
	p.state.finishAdvance()
	return nil
}

func (p *participant) awaitGrant(
	ctx context.Context,
	logicalTime float64,
	started counterStamp,
	sample bool,
) error {
	if err := p.state.wait(ctx, p.cfg.Timeout, fmt.Sprintf("time advance grant %g", logicalTime), func(s *eventState) bool {
		return s.pending && s.pendingTime == logicalTime && s.grantSeen
	}); err != nil {
		return err
	}
	grantLatency := counterElapsed(started)
	if sample {
		if err := p.log.grantBoundarySample(grantLatency); err != nil {
			return err
		}
	}
	return p.log.event("TM", "time_advance_granted", map[string]any{"logical_time": int(logicalTime)})
}

func (log *runLog) grantBoundarySample(durationNS int64) error {
	if durationNS < 0 {
		return errors.New("sample duration must be non-negative")
	}
	if log.compact {
		return nil
	}
	log.samples.mu.Lock()
	defer log.samples.mu.Unlock()
	record := struct {
		Sequence   uint64            `json:"sequence"`
		Operation  string            `json:"operation"`
		DurationNS int64             `json:"duration_ns"`
		Dimensions map[string]string `json:"dimensions"`
	}{
		Sequence:   log.samples.seq,
		Operation:  "timeAdvanceGrantLatency",
		DurationNS: durationNS,
		Dimensions: map[string]string{"boundary": "grant", "service": "TM"},
	}
	if err := log.samples.encoder.Encode(record); err != nil {
		return err
	}
	log.samples.seq++
	return nil
}

func (p *participant) review() error {
	if p.cfg.Role == roleSubscriber {
		accounting := p.state.accounting()
		if accounting.Delivered != accounting.Expected || accounting.Rejected != 0 ||
			accounting.Dropped != 0 || accounting.Unexpected != 0 ||
			accounting.Duplicates != 0 || accounting.Invalid != 0 {
			return fmt.Errorf("incomplete delivery accounting: %+v", accounting)
		}
	}
	expectedGrants := p.cfg.Count + 1
	if p.cfg.TMAdvanceOnly {
		expectedGrants = p.cfg.Count
	}
	if p.cfg.ReceiveOrder {
		expectedGrants = 0
	}
	if p.state.grants() != expectedGrants {
		return fmt.Errorf("grant count = %d, want %d", p.state.grants(), expectedGrants)
	}
	if p.cfg.Role == rolePublisher {
		if p.cfg.TMAdvanceOnly {
			return p.state.failure()
		}
		if p.cfg.LocalLRC {
			stats := p.fed.LocalLRCStats()
			expected := uint64((p.cfg.Count + p.cfg.OperationWarmup) * 2)
			expectedBatchSize := p.cfg.LocalLRCBatchSize
			if expectedBatchSize > p.cfg.LocalLRCQueueCapacity {
				expectedBatchSize = p.cfg.LocalLRCQueueCapacity
			}
			if stats.Enqueued != expected || stats.Sent != expected || stats.Acked != expected ||
				stats.Failures != 0 || stats.RequestedBatchSize != uint32(p.cfg.LocalLRCBatchSize) ||
				stats.PeerBatchLimit != uint32(p.cfg.LocalLRCBatchSize) ||
				stats.BatchSize != expectedBatchSize || stats.MaxFrameOperations == 0 ||
				stats.MaxFrameOperations > uint64(expectedBatchSize) || stats.OperationFrames >= expected {
				return fmt.Errorf("LocalLRC transport attestation failed: %+v", stats)
			}
			return p.state.failure()
		}
		stats := p.fed.ConfirmedObjectTransportStats()
		fallbacks := stats.FallbackDisabled + stats.FallbackMetadata + stats.FallbackUnsupported
		expected := uint64((p.cfg.Count + p.cfg.OperationWarmup) * 2)
		if stats.Total != expected || stats.StreamSent != expected ||
			stats.StreamAcked != expected || stats.UnarySent != 0 || stats.UnaryAcked != 0 ||
			stats.OpenAttempts != 1 || stats.OpenSuccesses != 1 || stats.Resets != 0 ||
			stats.Indeterminate != 0 || fallbacks != 0 {
			return fmt.Errorf("confirmed object transport attestation failed: %+v", stats)
		}
	}
	return p.state.failure()
}

func (p *participant) call(ctx context.Context, call func(context.Context) error) error {
	callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()
	return call(callCtx)
}

func (p *participant) pumpEvents() {
	defer close(p.pumpDone)
	for event := range p.fed.Events() {
		p.state.accept(event, counterNow())
	}
	p.state.streamClosed()
}

func (p *participant) shutdown() error {
	if p.state != nil {
		p.state.beginClosing()
	}
	if !p.completed {
		p.bestEffortShutdown()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if p.cfg.Role == rolePublisher && p.fed != nil {
		for {
			members, err := p.fed.ListFederationMembers(ctx)
			if err != nil {
				return fmt.Errorf("wait for peer resignation: %w", err)
			}
			if len(members) <= 1 {
				break
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("wait for peer resignation: %w", ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
		if err := p.log.event("FM", "peer_resigned", map[string]any{"peer": "subscriber"}); err != nil {
			return err
		}
	}
	if p.fed != nil {
		if err := p.fed.ResignWithAction(ctx, federate.ResignActionDeleteThenDivest); err != nil {
			return fmt.Errorf("resign federation: %w", err)
		}
		if err := p.log.event("FM", "resigned", map[string]any{}); err != nil {
			return err
		}
	}
	if p.pumpDone != nil {
		<-p.pumpDone
	}
	if p.cfg.Role == rolePublisher && p.conn != nil {
		if err := p.conn.DestroyFederation(ctx, p.cfg.Federation); err != nil {
			return fmt.Errorf("destroy federation: %w", err)
		}
		if err := p.log.event("FM", "federation_destroyed", map[string]any{}); err != nil {
			return err
		}
	}
	if p.conn != nil {
		if err := p.conn.Close(); err != nil {
			return fmt.Errorf("disconnect: %w", err)
		}
	}
	return p.log.event("FM", "disconnected", map[string]any{})
}

func (p *participant) bestEffortShutdown() {
	if p.fed != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = p.fed.ResignWithAction(ctx, federate.ResignActionCancelThenDelete)
		cancel()
	}
	if p.pumpDone != nil {
		<-p.pumpDone
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
}
