package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

type observation struct {
	accepted   bool
	receivedAt counterStamp
}

type callbackHandleSet struct {
	configured          bool
	objectClass         uint64
	attributeSequence   uint64
	attributePayload    uint64
	interactionClass    uint64
	interactionSequence uint64
	interactionPayload  uint64
}

type deliveryAccounting struct {
	Expected   int
	Delivered  int
	Rejected   int
	Dropped    int
	Unexpected int
	Duplicates int
	Invalid    int
}

type eventState struct {
	mu   sync.Mutex
	cond *sync.Cond
	cfg  config

	self                     uint64
	participants             []uint64
	announced                map[string]bool
	announcementParticipants map[string][]uint64
	synchronized             map[string]bool
	pendingTime              float64
	pending                  bool
	grantSeen                bool
	grantCount               int
	activeBatch              int
	receiveOrderBatchArmed   bool
	receiveOrderBatchStarted counterStamp

	objectHandle             uint64
	discovered               bool
	nameReserved             bool
	removed                  bool
	reflections              []observation
	interactions             []observation
	warmupReflections        []bool
	warmupInteractions       []bool
	rejected                 map[string]bool
	unexpected               int
	duplicates               int
	invalid                  int
	expectedAttributes       []string
	expectedInteractions     []string
	callbackHandles          callbackHandleSet
	attributeArrivalDigest   hash.Hash
	interactionArrivalDigest hash.Hash
	callbackTraceDigest      hash.Hash
	nextCallbackOrdinal      int
	failureErr               error
	closing                  bool
}

func newEventState(cfg config) *eventState {
	expectedAttributes := make([]string, cfg.Count)
	expectedInteractions := make([]string, cfg.Count)
	for index := 0; index < cfg.Count; index++ {
		expectedAttributes[index] = deterministicPayload(cfg.Seed, "attribute", index)
		expectedInteractions[index] = deterministicPayload(cfg.Seed, "interaction", index)
	}
	return newEventStateWithExpectedPayloads(cfg, expectedAttributes, expectedInteractions)
}

func newEventStateWithPayloads(cfg config, payloads []encodedIteration) (*eventState, error) {
	if len(payloads) != cfg.Count {
		return nil, fmt.Errorf("payload count = %d, want %d", len(payloads), cfg.Count)
	}
	expectedAttributes := make([]string, cfg.Count)
	expectedInteractions := make([]string, cfg.Count)
	for ordinal, payload := range payloads {
		if payload.index != ordinal {
			return nil, fmt.Errorf("payload order error at ordinal %d: index=%d", ordinal, payload.index)
		}
		if !isLowerHexPayload(payload.attribute) || !isLowerHexPayload(payload.interaction) {
			return nil, fmt.Errorf("payload %d must contain 8 bytes encoded as lowercase hexadecimal", ordinal)
		}
		expectedAttributes[ordinal] = payload.attribute
		expectedInteractions[ordinal] = payload.interaction
	}
	return newEventStateWithExpectedPayloads(cfg, expectedAttributes, expectedInteractions), nil
}

func newEventStateWithExpectedPayloads(
	cfg config,
	expectedAttributes []string,
	expectedInteractions []string,
) *eventState {
	if strings.TrimSpace(cfg.ObjectClass) == "" {
		cfg.ObjectClass = objectClass
	}
	if strings.TrimSpace(cfg.InteractionClass) == "" {
		cfg.InteractionClass = interactionClass
	}
	if strings.TrimSpace(cfg.ObjectName) == "" {
		cfg.ObjectName = objectName
	}
	state := &eventState{
		cfg: cfg, activeBatch: -1,
		announced:                make(map[string]bool),
		announcementParticipants: make(map[string][]uint64),
		synchronized:             make(map[string]bool),
		reflections:              make([]observation, cfg.Count), interactions: make([]observation, cfg.Count),
		warmupReflections:        make([]bool, cfg.OperationWarmup),
		warmupInteractions:       make([]bool, cfg.OperationWarmup),
		rejected:                 make(map[string]bool),
		expectedAttributes:       append([]string(nil), expectedAttributes...),
		expectedInteractions:     append([]string(nil), expectedInteractions...),
		attributeArrivalDigest:   sha256.New(),
		interactionArrivalDigest: sha256.New(),
		callbackTraceDigest:      sha256.New(),
	}
	state.cond = sync.NewCond(&state.mu)
	return state
}

func isLowerHexPayload(value string) bool {
	if len(value) != 16 {
		return false
	}
	for index := range value {
		if (value[index] < '0' || value[index] > '9') && (value[index] < 'a' || value[index] > 'f') {
			return false
		}
	}
	return true
}

func (s *eventState) notifyLocked() {
	s.cond.Broadcast()
}

func (s *eventState) setSelf(handle uint64) {
	s.mu.Lock()
	s.self = handle
	s.mu.Unlock()
}

func (s *eventState) setDeliveryHandles(handles callbackHandleSet) {
	s.mu.Lock()
	handles.configured = true
	s.callbackHandles = handles
	s.mu.Unlock()
}

func (s *eventState) setParticipants(handles []uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.participants = append([]uint64(nil), handles...)
	for label, announced := range s.announcementParticipants {
		if err := s.validateAnnouncementParticipants(label, announced); err != nil {
			s.failureErr = err
			s.notifyLocked()
			return err
		}
	}
	return nil
}

func (s *eventState) participantHandles() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uint64(nil), s.participants...)
}

func (s *eventState) accept(event federate.Event, receivedAt counterStamp) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failureErr != nil {
		return
	}
	var err error
	switch value := event.(type) {
	case federate.SynchronizationPointAnnounced:
		err = s.acceptAnnouncement(value)
	case federate.FederationSynchronized:
		err = s.acceptSynchronized(value)
	case federate.TimeAdvanceGrant:
		err = s.acceptGrant(value)
	case federate.DiscoverObjectInstance:
		err = s.acceptDiscovery(value)
	case federate.ObjectInstanceNameReservationSucceeded:
		err = s.acceptNameReservation(value.ObjectName, true)
	case federate.ObjectInstanceNameReservationFailed:
		err = s.acceptNameReservation(value.ObjectName, false)
	case federate.ReflectAttributeValues:
		err = s.acceptReflection(value, receivedAt)
	case federate.ReflectAttributeValuesByHandle:
		err = s.acceptReflectionByHandle(value, receivedAt)
	case federate.ReceiveInteraction:
		err = s.acceptInteraction(value, receivedAt)
	case federate.ReceiveInteractionByHandle:
		err = s.acceptInteractionByHandle(value, receivedAt)
	case federate.RemoveObjectInstance:
		err = s.acceptRemoval(value)
	case federate.FederationHalted:
		err = fmt.Errorf("federation halted: %s", value.Reason)
	default:
		err = fmt.Errorf("unexpected callback type %T", event)
	}
	if err != nil {
		s.failureErr = err
	}
	s.notifyLocked()
}

func (s *eventState) acceptNameReservation(name string, succeeded bool) error {
	if s.cfg.Role != rolePublisher {
		return errors.New("subscriber received object name reservation callback")
	}
	if name != s.cfg.ObjectName {
		return fmt.Errorf("object name reservation callback = %q, want %q", name, s.cfg.ObjectName)
	}
	if s.nameReserved {
		return errors.New("duplicate object name reservation callback")
	}
	if !succeeded {
		return fmt.Errorf("object name reservation failed for %q", name)
	}
	s.nameReserved = true
	return nil
}

func (s *eventState) acceptAnnouncement(event federate.SynchronizationPointAnnounced) error {
	if !s.expectedSynchronizationLabel(event.Label) {
		return fmt.Errorf("unexpected synchronization announcement %q", event.Label)
	}
	if s.announced[event.Label] {
		return fmt.Errorf("duplicate synchronization announcement %q", event.Label)
	}
	got := append([]uint64(nil), event.RequiredFederates...)
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	if len(event.Tag) != 0 {
		return fmt.Errorf("synchronization %s tag must be empty", event.Label)
	}
	s.announcementParticipants[event.Label] = got
	// The announcement stream can win the race with ListFederationMembers.
	// Defer participant validation until setParticipants when that happens.
	if len(s.participants) != 0 {
		if err := s.validateAnnouncementParticipants(event.Label, got); err != nil {
			return err
		}
	}
	s.announced[event.Label] = true
	return nil
}

func (s *eventState) validateAnnouncementParticipants(label string, got []uint64) error {
	if len(got) != len(s.participants) {
		return fmt.Errorf("synchronization %s participant count = %d, want %d", label, len(got), len(s.participants))
	}
	for index := range got {
		if got[index] != s.participants[index] {
			return fmt.Errorf("synchronization %s participants = %v, want %v", label, got, s.participants)
		}
	}
	return nil
}

func (s *eventState) acceptSynchronized(event federate.FederationSynchronized) error {
	if !s.expectedSynchronizationLabel(event.Label) {
		return fmt.Errorf("unexpected federation synchronized label %q", event.Label)
	}
	if !s.announced[event.Label] {
		return fmt.Errorf("federation synchronized %s arrived before announcement", event.Label)
	}
	if s.synchronized[event.Label] {
		return fmt.Errorf("duplicate federation synchronized %q", event.Label)
	}
	if len(event.FailedToSync) != 0 {
		return fmt.Errorf("federation synchronized %s failures = %v", event.Label, event.FailedToSync)
	}
	s.synchronized[event.Label] = true
	return nil
}

func (s *eventState) expectedSynchronizationLabel(label string) bool {
	return label == readySync || label == doneSync ||
		(label == measureSync && (s.cfg.OperationWarmup > 0 || !s.cfg.ReceiveOrder || s.cfg.devstoneMode())) ||
		(label == startSync && s.cfg.ReceiveOrder && s.cfg.devstoneMode())
}

func (s *eventState) acceptGrant(event federate.TimeAdvanceGrant) error {
	if !s.pending {
		return fmt.Errorf("unsolicited time advance grant %g", event.Time)
	}
	if s.grantSeen {
		return fmt.Errorf("duplicate time advance grant %g", event.Time)
	}
	if event.Time != s.pendingTime {
		return fmt.Errorf("time advance grant = %g, want %g", event.Time, s.pendingTime)
	}
	if s.cfg.Role == roleSubscriber && s.activeBatch >= 0 {
		index := s.activeBatch
		if !s.cfg.AllowGrantBeforeCallbacks &&
			(!s.reflections[index].accepted || !s.interactions[index].accepted) {
			return fmt.Errorf("time advance grant %g arrived before delivery batch %d completed", event.Time, index)
		}
	}
	s.grantSeen = true
	s.grantCount++
	return nil
}

func (s *eventState) acceptDiscovery(event federate.DiscoverObjectInstance) error {
	if s.cfg.Role != roleSubscriber {
		return errors.New("publisher received object discovery")
	}
	if s.discovered {
		return errors.New("duplicate object discovery")
	}
	if event.ClassName != s.cfg.ObjectClass || event.ObjectName != s.cfg.ObjectName || event.ObjectHandle == 0 {
		return fmt.Errorf("unexpected object discovery: %+v", event)
	}
	s.discovered = true
	s.objectHandle = event.ObjectHandle
	return nil
}

func (s *eventState) acceptReflection(event federate.ReflectAttributeValues, receivedAt counterStamp) error {
	index, err := s.deliveryIdentity("attribute", event.Attributes)
	if err != nil {
		s.rejectLocked("")
		return err
	}
	return s.acceptReflectionValue(
		index,
		event.ObjectHandle,
		event.ClassName == s.cfg.ObjectClass,
		event.Attributes[payloadField],
		event.Timestamp,
		receivedAt,
	)
}

func (s *eventState) acceptReflectionByHandle(
	event federate.ReflectAttributeValuesByHandle,
	receivedAt counterStamp,
) error {
	handles := s.callbackHandles
	if !handles.configured {
		s.rejectLocked("")
		return errors.New("attribute callback handles are not configured")
	}
	index, err := s.deliveryIdentityByHandle(
		"attribute",
		event.Attributes,
		handles.attributeSequence,
		handles.attributePayload,
	)
	if err != nil {
		s.rejectLocked("")
		return err
	}
	return s.acceptReflectionValue(
		index,
		event.ObjectHandle,
		event.ObjectClassHandle == handles.objectClass,
		event.Attributes[handles.attributePayload],
		event.Timestamp,
		receivedAt,
	)
}

func (s *eventState) acceptReflectionValue(
	index int,
	objectHandle uint64,
	classMatches bool,
	payloadBytes []byte,
	timestamp *float64,
	receivedAt counterStamp,
) error {
	warmup := index >= s.cfg.Count
	if warmup && s.warmupReflections[index-s.cfg.Count] {
		s.duplicates++
		return fmt.Errorf("duplicate warmup attribute reflection %d", index)
	}
	if !warmup && s.reflections[index].accepted {
		s.duplicates++
		return fmt.Errorf("duplicate attribute reflection %d", index)
	}
	if !warmup && s.cfg.ReceiveOrder && s.cfg.devstoneMode() && !s.receiveOrderBatchArmed {
		s.rejectLocked(deliverySlot("attribute", index))
		return fmt.Errorf("attribute reflection %d arrived before VERIFY_START timing arm", index)
	}
	if !warmup && s.cfg.ReceiveOrder && s.cfg.devstoneMode() && receivedAt < s.receiveOrderBatchStarted {
		s.rejectLocked(deliverySlot("attribute", index))
		return fmt.Errorf("attribute reflection %d timestamp precedes the timing arm", index)
	}
	if s.cfg.Role != roleSubscriber || !s.discovered ||
		objectHandle != s.objectHandle || !classMatches {
		s.rejectLocked(deliverySlot("attribute", index))
		return fmt.Errorf("attribute reflection %d has unexpected object identity", index)
	}
	if !s.cfg.ReceiveOrder && s.activeBatch != index {
		s.rejectLocked(deliverySlot("attribute", index))
		return fmt.Errorf("attribute reflection %d arrived during batch %d", index, s.activeBatch)
	}
	if (s.cfg.ReceiveOrder && timestamp != nil) ||
		(!s.cfg.ReceiveOrder && (timestamp == nil || *timestamp != float64(index+1))) {
		s.rejectLocked(deliverySlot("attribute", index))
		return fmt.Errorf("attribute reflection %d timestamp = %v", index, timestamp)
	}
	payload, err := decodeASCIIString(payloadBytes)
	var expectedPayload string
	if warmup {
		expectedPayload = deterministicPayload(s.cfg.Seed, "attribute", index)
	} else {
		expectedPayload = s.expectedAttributes[index]
	}
	if err != nil || payload != expectedPayload {
		s.rejectLocked(deliverySlot("attribute", index))
		return fmt.Errorf("attribute reflection %d payload mismatch", index)
	}
	if warmup {
		s.warmupReflections[index-s.cfg.Count] = true
	} else {
		if err := s.acceptCallbackTraceLocked("attribute", index, payload); err != nil {
			s.rejectLocked(deliverySlot("attribute", index))
			return err
		}
		s.reflections[index] = observation{accepted: true, receivedAt: receivedAt}
		updateArrivalDigest(s.attributeArrivalDigest, index, payload)
	}
	return nil
}

func (s *eventState) acceptInteraction(event federate.ReceiveInteraction, receivedAt counterStamp) error {
	index, err := s.deliveryIdentity("interaction", event.Parameters)
	if err != nil {
		s.rejectLocked("")
		return err
	}
	return s.acceptInteractionValue(
		index,
		event.ClassName == s.cfg.InteractionClass,
		event.Parameters[payloadField],
		event.Timestamp,
		receivedAt,
	)
}

func (s *eventState) acceptInteractionByHandle(
	event federate.ReceiveInteractionByHandle,
	receivedAt counterStamp,
) error {
	handles := s.callbackHandles
	if !handles.configured {
		s.rejectLocked("")
		return errors.New("interaction callback handles are not configured")
	}
	index, err := s.deliveryIdentityByHandle(
		"interaction",
		event.Parameters,
		handles.interactionSequence,
		handles.interactionPayload,
	)
	if err != nil {
		s.rejectLocked("")
		return err
	}
	return s.acceptInteractionValue(
		index,
		event.InteractionClassHandle == handles.interactionClass,
		event.Parameters[handles.interactionPayload],
		event.Timestamp,
		receivedAt,
	)
}

func (s *eventState) acceptInteractionValue(
	index int,
	classMatches bool,
	payloadBytes []byte,
	timestamp *float64,
	receivedAt counterStamp,
) error {
	warmup := index >= s.cfg.Count
	if warmup && s.warmupInteractions[index-s.cfg.Count] {
		s.duplicates++
		return fmt.Errorf("duplicate warmup interaction %d", index)
	}
	if !warmup && s.interactions[index].accepted {
		s.duplicates++
		return fmt.Errorf("duplicate interaction %d", index)
	}
	if !warmup && s.cfg.ReceiveOrder && s.cfg.devstoneMode() && !s.receiveOrderBatchArmed {
		s.rejectLocked(deliverySlot("interaction", index))
		return fmt.Errorf("interaction %d arrived before VERIFY_START timing arm", index)
	}
	if !warmup && s.cfg.ReceiveOrder && s.cfg.devstoneMode() && receivedAt < s.receiveOrderBatchStarted {
		s.rejectLocked(deliverySlot("interaction", index))
		return fmt.Errorf("interaction %d timestamp precedes the timing arm", index)
	}
	if s.cfg.Role != roleSubscriber || !classMatches {
		s.rejectLocked(deliverySlot("interaction", index))
		return fmt.Errorf("interaction %d has unexpected class", index)
	}
	if !s.cfg.ReceiveOrder && s.activeBatch != index {
		s.rejectLocked(deliverySlot("interaction", index))
		return fmt.Errorf("interaction %d arrived during batch %d", index, s.activeBatch)
	}
	if (s.cfg.ReceiveOrder && timestamp != nil) ||
		(!s.cfg.ReceiveOrder && (timestamp == nil || *timestamp != float64(index+1))) {
		s.rejectLocked(deliverySlot("interaction", index))
		return fmt.Errorf("interaction %d timestamp = %v", index, timestamp)
	}
	payload, err := decodeASCIIString(payloadBytes)
	var expectedPayload string
	if warmup {
		expectedPayload = deterministicPayload(s.cfg.Seed, "interaction", index)
	} else {
		expectedPayload = s.expectedInteractions[index]
	}
	if err != nil || payload != expectedPayload {
		s.rejectLocked(deliverySlot("interaction", index))
		return fmt.Errorf("interaction %d payload mismatch", index)
	}
	if warmup {
		s.warmupInteractions[index-s.cfg.Count] = true
	} else {
		if err := s.acceptCallbackTraceLocked("interaction", index, payload); err != nil {
			s.rejectLocked(deliverySlot("interaction", index))
			return err
		}
		s.interactions[index] = observation{accepted: true, receivedAt: receivedAt}
		updateArrivalDigest(s.interactionArrivalDigest, index, payload)
	}
	return nil
}

func updateArrivalDigest(digest hash.Hash, index int, payload string) {
	var material [4 + 16]byte
	binary.BigEndian.PutUint32(material[:4], uint32(index))
	copy(material[4:], payload)
	_, _ = digest.Write(material[:])
}

func (s *eventState) acceptCallbackTraceLocked(channel string, index int, payload string) error {
	if s.cfg.ReceiveOrder && s.cfg.devstoneMode() {
		expectedChannel := "attribute"
		if s.nextCallbackOrdinal%2 != 0 {
			expectedChannel = "interaction"
		}
		expectedIndex := s.nextCallbackOrdinal / 2
		if channel != expectedChannel || index != expectedIndex {
			return fmt.Errorf(
				"callback trace entry %s:%d, want %s:%d",
				channel, index, expectedChannel, expectedIndex,
			)
		}
	}
	marker := byte('A')
	if channel == "interaction" {
		marker = 'I'
	}
	var material [1 + 4 + 16]byte
	material[0] = marker
	binary.BigEndian.PutUint32(material[1:5], uint32(index))
	copy(material[5:], payload)
	_, _ = s.callbackTraceDigest.Write(material[:])
	s.nextCallbackOrdinal++
	return nil
}

func (s *eventState) deliveryIdentity(channel string, values map[string][]byte) (int, error) {
	if len(values) != 2 {
		return -1, fmt.Errorf("%s field count = %d, want 2", channel, len(values))
	}
	sequence, ok := values[sequenceField]
	if !ok {
		return -1, fmt.Errorf("%s is missing %s", channel, sequenceField)
	}
	if _, ok := values[payloadField]; !ok {
		return -1, fmt.Errorf("%s is missing %s", channel, payloadField)
	}
	index, err := decodeInteger32BE(sequence)
	if err != nil || index < 0 || index >= s.cfg.Count+s.cfg.OperationWarmup {
		return -1, fmt.Errorf("%s sequence is invalid", channel)
	}
	return index, nil
}

func (s *eventState) deliveryIdentityByHandle(
	channel string,
	values map[uint64][]byte,
	sequenceHandle uint64,
	payloadHandle uint64,
) (int, error) {
	if len(values) != 2 {
		return -1, fmt.Errorf("%s field count = %d, want 2", channel, len(values))
	}
	sequence, ok := values[sequenceHandle]
	if !ok {
		return -1, fmt.Errorf("%s is missing sequence handle %d", channel, sequenceHandle)
	}
	if _, ok := values[payloadHandle]; !ok {
		return -1, fmt.Errorf("%s is missing payload handle %d", channel, payloadHandle)
	}
	index, err := decodeInteger32BE(sequence)
	if err != nil || index < 0 || index >= s.cfg.Count+s.cfg.OperationWarmup {
		return -1, fmt.Errorf("%s sequence is invalid", channel)
	}
	return index, nil
}

func deliverySlot(channel string, index int) string {
	return fmt.Sprintf("%s:%d", channel, index)
}

func (s *eventState) warmupComplete() bool {
	for index := range s.warmupReflections {
		if !s.warmupReflections[index] || !s.warmupInteractions[index] {
			return false
		}
	}
	return true
}

func (s *eventState) acceptRemoval(event federate.RemoveObjectInstance) error {
	if s.cfg.Role != roleSubscriber || !s.discovered || event.ObjectHandle != s.objectHandle {
		return errors.New("object removal has unexpected identity")
	}
	if s.removed {
		return errors.New("duplicate object removal")
	}
	expected := float64(s.cfg.Count + 1)
	invalidTimestamp := (s.cfg.ReceiveOrder && event.Timestamp != nil) ||
		(!s.cfg.ReceiveOrder && (event.Timestamp == nil || *event.Timestamp != expected))
	if invalidTimestamp || len(event.Tag) != 0 {
		return fmt.Errorf("object removal timestamp/tag mismatch: %+v", event)
	}
	s.removed = true
	return nil
}

func (s *eventState) rejectLocked(slot string) {
	s.invalid++
	if slot == "" || s.rejected[slot] {
		s.unexpected++
		return
	}
	s.rejected[slot] = true
}

func (s *eventState) prepareAdvance(logicalTime float64, batch int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failureErr != nil {
		return s.failureErr
	}
	if s.pending {
		return errors.New("time advance already pending")
	}
	s.pending = true
	s.pendingTime = logicalTime
	s.grantSeen = false
	s.activeBatch = batch
	return nil
}

func (s *eventState) finishAdvance() {
	s.mu.Lock()
	s.pending = false
	s.grantSeen = false
	s.activeBatch = -1
	s.mu.Unlock()
}

func (s *eventState) observations(index int) (observation, observation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reflections[index], s.interactions[index]
}

func (s *eventState) armReceiveOrderBatch(started counterStamp) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Role != roleSubscriber || !s.cfg.ReceiveOrder || !s.cfg.devstoneMode() {
		return errors.New("receive-order batch timing arm requires a DEVStone subscriber")
	}
	if s.receiveOrderBatchArmed {
		return errors.New("receive-order batch timing is already armed")
	}
	for index := range s.reflections {
		if s.reflections[index].accepted || s.interactions[index].accepted {
			return fmt.Errorf("cannot arm receive-order timing after callback %d", index)
		}
	}
	s.receiveOrderBatchArmed = true
	s.receiveOrderBatchStarted = started
	return nil
}

func (s *eventState) receiveOrderBatchStart(fallback counterStamp) (counterStamp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cfg.devstoneMode() {
		return fallback, nil
	}
	if !s.receiveOrderBatchArmed {
		return 0, errors.New("receive-order batch timing was not armed before VERIFY_START")
	}
	return s.receiveOrderBatchStarted, nil
}

func (s *eventState) wait(
	ctx context.Context, timeout time.Duration, description string, predicate func(*eventState) bool,
) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stopWakeup := context.AfterFunc(waitCtx, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	defer stopWakeup()

	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if s.failureErr != nil {
			return s.failureErr
		}
		if predicate(s) {
			return nil
		}
		if waitCtx.Err() != nil {
			return fmt.Errorf("wait for %s: %w", description, waitCtx.Err())
		}
		s.cond.Wait()
	}
}

func (s *eventState) accounting() deliveryAccounting {
	s.mu.Lock()
	defer s.mu.Unlock()
	delivered := 0
	for index := range s.reflections {
		if s.reflections[index].accepted {
			delivered++
		}
		if s.interactions[index].accepted {
			delivered++
		}
	}
	expected := 0
	if s.cfg.Role == roleSubscriber && !s.cfg.TMAdvanceOnly {
		expected = 2 * s.cfg.Count
	}
	rejected := len(s.rejected)
	dropped := expected - delivered - rejected
	if dropped < 0 {
		dropped = 0
	}
	return deliveryAccounting{
		Expected: expected, Delivered: delivered, Rejected: rejected,
		Dropped: dropped, Unexpected: s.unexpected,
		Duplicates: s.duplicates, Invalid: s.invalid,
	}
}

func (s *eventState) callbackCounts() (attributes int, interactions int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.reflections {
		if s.reflections[index].accepted {
			attributes++
		}
		if s.interactions[index].accepted {
			interactions++
		}
	}
	return attributes, interactions
}

func (s *eventState) callbackDigests() (attribute string, interaction string, trace string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return hex.EncodeToString(s.attributeArrivalDigest.Sum(nil)),
		hex.EncodeToString(s.interactionArrivalDigest.Sum(nil)),
		hex.EncodeToString(s.callbackTraceDigest.Sum(nil))
}

func (s *eventState) validatedObjectDiscovery() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.validatedObjectDiscoveryLocked()
}

func (s *eventState) validatedObjectDiscoveryLocked() bool {
	return s.cfg.Role == roleSubscriber && s.discovered && s.objectHandle != 0
}

func (s *eventState) synchronizationStatus() (ready, measure, start, done bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.synchronized[readySync], s.synchronized[measureSync],
		s.synchronized[startSync], s.synchronized[doneSync]
}

func (s *eventState) failure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failureErr
}

func (s *eventState) grants() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.grantCount
}

func (s *eventState) beginClosing() {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
}

func (s *eventState) streamClosed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closing && s.failureErr == nil {
		s.failureErr = errors.New("federate event stream closed unexpectedly")
		s.notifyLocked()
	}
}
