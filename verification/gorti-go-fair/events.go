package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

type observation struct {
	accepted   bool
	receivedAt counterStamp
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
	mu     sync.Mutex
	change chan struct{}
	cfg    config

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

	objectHandle uint64
	discovered   bool
	nameReserved bool
	removed      bool
	reflections  []observation
	interactions []observation
	rejected     map[string]bool
	unexpected   int
	duplicates   int
	invalid      int
	failureErr   error
	closing      bool
}

func newEventState(cfg config) *eventState {
	return &eventState{
		cfg: cfg, change: make(chan struct{}), activeBatch: -1,
		announced:                make(map[string]bool),
		announcementParticipants: make(map[string][]uint64),
		synchronized:             make(map[string]bool),
		reflections:              make([]observation, cfg.Count), interactions: make([]observation, cfg.Count),
		rejected: make(map[string]bool),
	}
}

func (s *eventState) notifyLocked() {
	close(s.change)
	s.change = make(chan struct{})
}

func (s *eventState) setSelf(handle uint64) {
	s.mu.Lock()
	s.self = handle
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
	case federate.ReceiveInteraction:
		err = s.acceptInteraction(value, receivedAt)
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
	if name != objectName {
		return fmt.Errorf("object name reservation callback = %q, want %q", name, objectName)
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
	if event.Label != readySync && event.Label != doneSync {
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
	if event.Label != readySync && event.Label != doneSync {
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
		if !s.reflections[index].accepted || !s.interactions[index].accepted {
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
	if event.ClassName != objectClass || event.ObjectName != objectName || event.ObjectHandle == 0 {
		return fmt.Errorf("unexpected object discovery: %+v", event)
	}
	s.discovered = true
	s.objectHandle = event.ObjectHandle
	return nil
}

func (s *eventState) acceptReflection(event federate.ReflectAttributeValues, receivedAt counterStamp) error {
	index, slot, err := s.deliveryIdentity("attribute", event.Attributes)
	if err != nil {
		s.rejectLocked(slot)
		return err
	}
	if s.reflections[index].accepted {
		s.duplicates++
		return fmt.Errorf("duplicate attribute reflection %d", index)
	}
	if s.cfg.Role != roleSubscriber || !s.discovered || event.ObjectHandle != s.objectHandle || event.ClassName != objectClass {
		s.rejectLocked(slot)
		return fmt.Errorf("attribute reflection %d has unexpected object identity", index)
	}
	if s.activeBatch != index {
		s.rejectLocked(slot)
		return fmt.Errorf("attribute reflection %d arrived during batch %d", index, s.activeBatch)
	}
	if event.Timestamp == nil || *event.Timestamp != float64(index+1) {
		s.rejectLocked(slot)
		return fmt.Errorf("attribute reflection %d timestamp = %v", index, event.Timestamp)
	}
	payload, err := decodeASCIIString(event.Attributes[payloadField])
	if err != nil || payload != deterministicPayload(s.cfg.Seed, "attribute", index) {
		s.rejectLocked(slot)
		return fmt.Errorf("attribute reflection %d payload mismatch", index)
	}
	s.reflections[index] = observation{accepted: true, receivedAt: receivedAt}
	return nil
}

func (s *eventState) acceptInteraction(event federate.ReceiveInteraction, receivedAt counterStamp) error {
	index, slot, err := s.deliveryIdentity("interaction", event.Parameters)
	if err != nil {
		s.rejectLocked(slot)
		return err
	}
	if s.interactions[index].accepted {
		s.duplicates++
		return fmt.Errorf("duplicate interaction %d", index)
	}
	if s.cfg.Role != roleSubscriber || event.ClassName != interactionClass {
		s.rejectLocked(slot)
		return fmt.Errorf("interaction %d class = %q, want %q", index, event.ClassName, interactionClass)
	}
	if s.activeBatch != index {
		s.rejectLocked(slot)
		return fmt.Errorf("interaction %d arrived during batch %d", index, s.activeBatch)
	}
	if event.Timestamp == nil || *event.Timestamp != float64(index+1) {
		s.rejectLocked(slot)
		return fmt.Errorf("interaction %d timestamp = %v", index, event.Timestamp)
	}
	payload, err := decodeASCIIString(event.Parameters[payloadField])
	if err != nil || payload != deterministicPayload(s.cfg.Seed, "interaction", index) {
		s.rejectLocked(slot)
		return fmt.Errorf("interaction %d payload mismatch", index)
	}
	s.interactions[index] = observation{accepted: true, receivedAt: receivedAt}
	return nil
}

func (s *eventState) deliveryIdentity(channel string, values map[string][]byte) (int, string, error) {
	if len(values) != 2 {
		return -1, "", fmt.Errorf("%s field count = %d, want 2", channel, len(values))
	}
	sequence, ok := values[sequenceField]
	if !ok {
		return -1, "", fmt.Errorf("%s is missing %s", channel, sequenceField)
	}
	if _, ok := values[payloadField]; !ok {
		return -1, "", fmt.Errorf("%s is missing %s", channel, payloadField)
	}
	index, err := decodeInteger32BE(sequence)
	if err != nil || index < 0 || index >= s.cfg.Count {
		return -1, "", fmt.Errorf("%s sequence is invalid", channel)
	}
	return index, fmt.Sprintf("%s:%d", channel, index), nil
}

func (s *eventState) acceptRemoval(event federate.RemoveObjectInstance) error {
	if s.cfg.Role != roleSubscriber || !s.discovered || event.ObjectHandle != s.objectHandle {
		return errors.New("object removal has unexpected identity")
	}
	if s.removed {
		return errors.New("duplicate object removal")
	}
	expected := float64(s.cfg.Count + 1)
	if event.Timestamp == nil || *event.Timestamp != expected || len(event.Tag) != 0 {
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

func (s *eventState) wait(
	ctx context.Context, timeout time.Duration, description string, predicate func(*eventState) bool,
) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		s.mu.Lock()
		if s.failureErr != nil {
			err := s.failureErr
			s.mu.Unlock()
			return err
		}
		if predicate(s) {
			s.mu.Unlock()
			return nil
		}
		change := s.change
		s.mu.Unlock()
		select {
		case <-change:
		case <-waitCtx.Done():
			return fmt.Errorf("wait for %s: %w", description, waitCtx.Err())
		}
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
	expected := 2 * s.cfg.Count
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
