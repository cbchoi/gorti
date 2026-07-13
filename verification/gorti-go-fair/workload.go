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
	objectClass        = "VerifierEntity"
	interactionClass   = "VerifierMessage"
	objectName         = "PitchVerifierEntity"
	sequenceField      = "Sequence"
	payloadField       = "Payload"
	publisherName      = "PitchVerifierPublisher"
	subscriberName     = "PitchVerifierSubscriber"
	readySync          = "VERIFY_READY"
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
	if count < 1 {
		return nil, errors.New("count must be at least 1")
	}
	if uint64(count-1) > uint64(math.MaxInt32) {
		return nil, errors.New("count exceeds HLAinteger32BE sequence range")
	}
	result := make([]encodedIteration, count)
	for index := range result {
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
		result[index] = encodedIteration{
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

type participant struct {
	cfg                    config
	log                    *runLog
	payloads               []encodedIteration
	conn                   *federate.Connection
	fed                    *federate.Federate
	state                  *eventState
	pumpDone               chan struct{}
	interactionClassHandle uint64
	completed              bool
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
	if p.cfg.Role == rolePublisher {
		if err := p.prepareWireHandles(); err != nil {
			return err
		}
	}
	if err := p.declare(ctx); err != nil {
		return err
	}
	if err := p.enableTime(ctx); err != nil {
		return err
	}
	participants, err := p.awaitParticipants(ctx)
	if err != nil {
		return err
	}
	if err := p.state.setParticipants(participants); err != nil {
		return err
	}
	if err := p.synchronize(ctx, readySync, p.cfg.Role == roleSubscriber); err != nil {
		return err
	}

	if p.cfg.Role == rolePublisher {
		if err := p.publish(ctx); err != nil {
			return err
		}
	} else if err := p.subscribe(ctx); err != nil {
		return err
	}

	if err := p.synchronize(ctx, doneSync, p.cfg.Role == rolePublisher); err != nil {
		return err
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
	if p.cfg.Role == roleSubscriber {
		return writeProjection(p.cfg, p.payloads)
	}
	return nil
}

func (p *participant) connectAndJoin(ctx context.Context, fom []byte) error {
	var err error
	if err = p.log.timed("FM", "connect", func() error {
		p.conn, err = federate.Connect(ctx, p.cfg.Address)
		return err
	}); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	if err := p.log.event("FM", "connected", map[string]any{"transport": "tcp"}); err != nil {
		return err
	}
	name := publisherName
	if p.cfg.Role == roleSubscriber {
		name = subscriberName
	}
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
	p.state = newEventState(p.cfg)
	p.state.setSelf(p.fed.Handle())
	p.pumpDone = make(chan struct{})
	go p.pumpEvents()
	return p.log.event("FM", "joined", map[string]any{"federate_type": "GortiGoFair-" + string(p.cfg.Role)})
}

func (p *participant) prepareWireHandles() error {
	objectClassHandle, ok := p.fed.ObjectClassHandle(objectClass)
	if !ok {
		return fmt.Errorf("object class %q not in FOM", objectClass)
	}
	sequenceAttributeHandle, ok := p.fed.AttributeHandle(objectClassHandle, sequenceField)
	if !ok {
		return fmt.Errorf("attribute %q not in object class %q", sequenceField, objectClass)
	}
	payloadAttributeHandle, ok := p.fed.AttributeHandle(objectClassHandle, payloadField)
	if !ok {
		return fmt.Errorf("attribute %q not in object class %q", payloadField, objectClass)
	}
	classHandle, ok := p.fed.InteractionClassHandle(interactionClass)
	if !ok {
		return fmt.Errorf("interaction class %q not in FOM", interactionClass)
	}
	sequenceHandle, ok := p.fed.ParameterHandle(classHandle, sequenceField)
	if !ok {
		return fmt.Errorf("parameter %q not in interaction class %q", sequenceField, interactionClass)
	}
	payloadHandle, ok := p.fed.ParameterHandle(classHandle, payloadField)
	if !ok {
		return fmt.Errorf("parameter %q not in interaction class %q", payloadField, interactionClass)
	}
	p.interactionClassHandle = classHandle
	for index := range p.payloads {
		item := &p.payloads[index]
		item.wireAttrs = map[uint64][]byte{
			sequenceAttributeHandle: item.attributes[sequenceField],
			payloadAttributeHandle:  item.attributes[payloadField],
		}
		item.wireParams = map[uint64][]byte{
			sequenceHandle: item.parameters[sequenceField],
			payloadHandle:  item.parameters[payloadField],
		}
	}
	return nil
}

func (p *participant) declare(ctx context.Context) error {
	if p.cfg.Role == rolePublisher {
		if err := p.log.timed("DM", "publish_object_class_attributes", func() error {
			return p.call(ctx, func(callCtx context.Context) error {
				return p.fed.PublishObjectClassAttributes(callCtx, objectClass, []string{sequenceField, payloadField})
			})
		}); err != nil {
			return err
		}
		if err := p.log.event("DM", "object_published", map[string]any{"class": objectClass}); err != nil {
			return err
		}
		if err := p.log.timed("DM", "publish_interaction_class", func() error {
			return p.call(ctx, func(callCtx context.Context) error {
				return p.fed.PublishInteractionClass(callCtx, interactionClass)
			})
		}); err != nil {
			return err
		}
		return p.log.event("DM", "interaction_published", map[string]any{"class": interactionClass})
	}
	if err := p.log.timed("DM", "subscribe_object_class_attributes", func() error {
		return p.call(ctx, func(callCtx context.Context) error {
			return p.fed.SubscribeObjectClassAttributes(callCtx, objectClass, []string{sequenceField, payloadField})
		})
	}); err != nil {
		return err
	}
	if err := p.log.event("DM", "object_subscribed", map[string]any{"class": objectClass}); err != nil {
		return err
	}
	if err := p.log.timed("DM", "subscribe_interaction_class", func() error {
		return p.call(ctx, func(callCtx context.Context) error {
			return p.fed.SubscribeInteractionClass(callCtx, interactionClass)
		})
	}); err != nil {
		return err
	}
	return p.log.event("DM", "interaction_subscribed", map[string]any{"class": interactionClass})
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
		publisher, publisherOK := byName[publisherName]
		subscriber, subscriberOK := byName[subscriberName]
		if publisherOK && subscriberOK {
			participants := []uint64{publisher, subscriber}
			sort.Slice(participants, func(i, j int) bool { return participants[i] < participants[j] })
			return participants, nil
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-waitCtx.Done():
			return nil, fmt.Errorf("wait for both federates: %w", waitCtx.Err())
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
			"label": label, "participants": 2,
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
	if err := p.log.timed("OM", "reserve_object_instance_name", func() error {
		return p.call(ctx, func(callCtx context.Context) error {
			return p.fed.ReserveObjectInstanceName(callCtx, objectName)
		})
	}); err != nil {
		return err
	}
	if err := p.state.wait(ctx, p.cfg.Timeout, "object instance name reservation", func(s *eventState) bool {
		return s.nameReserved
	}); err != nil {
		return err
	}
	if err := p.log.event("OM", "object_name_reserved", map[string]any{"name": objectName}); err != nil {
		return err
	}
	var objectHandle uint64
	if err := p.log.timed("OM", "register_object_instance", func() error {
		var err error
		callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
		defer cancel()
		objectHandle, err = p.fed.RegisterObjectInstance(callCtx, objectClass, objectName)
		return err
	}); err != nil {
		return err
	}
	if err := p.log.event("OM", "object_registered", map[string]any{
		"class": objectClass, "name": objectName,
	}); err != nil {
		return err
	}
	return p.publishTraffic(ctx, p.fed, objectHandle)
}

func (p *participant) publishTraffic(ctx context.Context, fed trafficFederate, objectHandle uint64) error {
	for _, item := range p.payloads {
		timestamp := item.time
		callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
		err := p.log.benchmarkTimed("OM", "updateAttributeValues", "update_attribute_values", func() error {
			return fed.UpdateAttributeValuesByHandle(callCtx, objectHandle, item.wireAttrs, &timestamp)
		})
		cancel()
		if err != nil {
			return err
		}
		if err := p.log.event("OM", "attributes_updated", map[string]any{
			"index": item.index, "logical_time": int(item.time), "payload": item.attribute,
		}); err != nil {
			return err
		}
		callCtx, cancel = context.WithTimeout(ctx, p.cfg.Timeout)
		err = p.log.benchmarkTimed("OM", "sendInteraction", "send_interaction", func() error {
			return fed.SendInteractionByHandle(
				callCtx, p.interactionClassHandle, item.wireParams, &timestamp,
			)
		})
		cancel()
		if err != nil {
			return err
		}
		if err := p.log.event("OM", "interaction_sent", map[string]any{
			"index": item.index, "logical_time": int(item.time), "payload": item.interaction,
		}); err != nil {
			return err
		}
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
		"logical_time": int(removalTime), "name": objectName,
	}); err != nil {
		return err
	}
	return p.advance(ctx, fed, removalTime, false)
}

func (p *participant) subscribe(ctx context.Context) error {
	sustainedStarted := counterNow()
	lastCompleted := sustainedStarted
	for _, item := range p.payloads {
		if err := p.state.prepareAdvance(item.time, item.index); err != nil {
			return err
		}
		callCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
		batchStarted := counterNow()
		tarStarted := batchStarted
		err := p.fed.TimeAdvanceRequest(callCtx, item.time)
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
				"class": objectClass, "name": objectName,
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
		if err := p.awaitGrant(ctx, item.time); err != nil {
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
	if err := p.advance(ctx, p.fed, removalTime, false); err != nil {
		return err
	}
	if err := p.state.wait(ctx, p.cfg.Timeout, "object removal", func(s *eventState) bool {
		return s.removed
	}); err != nil {
		return err
	}
	return p.log.event("OM", "object_removed", map[string]any{
		"logical_time": int(removalTime), "name": objectName,
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
	if err := p.awaitGrant(ctx, logicalTime); err != nil {
		return err
	}
	p.state.finishAdvance()
	return nil
}

func (p *participant) awaitGrant(ctx context.Context, logicalTime float64) error {
	if err := p.state.wait(ctx, p.cfg.Timeout, fmt.Sprintf("time advance grant %g", logicalTime), func(s *eventState) bool {
		return s.grantSeen
	}); err != nil {
		return err
	}
	return p.log.event("TM", "time_advance_granted", map[string]any{"logical_time": int(logicalTime)})
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
	if p.state.grants() != p.cfg.Count+1 {
		return fmt.Errorf("grant count = %d, want %d", p.state.grants(), p.cfg.Count+1)
	}
	if p.cfg.Role == rolePublisher {
		stats := p.fed.InteractionTransportStats()
		fallbacks := stats.FallbackDisabled + stats.FallbackMetadata + stats.FallbackUnsupported
		if stats.Total != uint64(p.cfg.Count) || stats.StreamSent != uint64(p.cfg.Count) ||
			stats.StreamAcked != uint64(p.cfg.Count) || stats.UnarySent != 0 || stats.UnaryAcked != 0 ||
			stats.OpenAttempts != 1 || stats.OpenSuccesses != 1 || stats.Resets != 0 ||
			stats.Indeterminate != 0 || fallbacks != 0 {
			return fmt.Errorf("interaction transport attestation failed: %+v", stats)
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
