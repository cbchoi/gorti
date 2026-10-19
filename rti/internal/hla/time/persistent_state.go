package time

import (
	"fmt"
	"slices"
	"sync"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ManagerState contains the persistent time state of one federation. Pending
// ages exclude time spent between saving and restoring. Strategy implementations
// and process-wide dependencies remain application configuration, not saved state.
type ManagerStateOf[T core.LogicalTimeScalar] struct {
	Halted       bool
	StallTimeout stdtime.Duration
	Federates    []FederateStateOf[T]
}

// FederateState preserves both stores, including entries created by asynchronous
// delivery alone. Queue order is arrival order, not timestamp order.
type FederateStateOf[T core.LogicalTimeScalar] struct {
	Elapsed              *ElapsedTimeState
	Handle               core.FederateHandle
	HasRegulationState   bool
	Regulating           bool
	Constrained          bool
	RegulationPending    bool
	RegulationLookahead  T
	ConstrainedPending   bool
	PreservedReceiveTime *T
	ReceivedTSOTime      *T
	Lookahead            T
	RequestedLookahead   *T
	LookaheadOriginTime  T
	LookaheadOriginValue T
	HasAdvanceState      bool
	CurrentTime          T
	StrictZeroLookahead  bool
	PendingRequest       bool
	RequestedTime        T
	Mode                 AdvanceMode
	// FlushMinimum retains the minimum delivered during a partial FQR or
	// next-message request, including a scheduled-save queue drain.
	FlushMinimum         *T
	HasPendingSince      bool
	PendingAge           stdtime.Duration
	AsynchronousDelivery bool
	Queue                []core.MessageDeliveryOf[T]
	ROQueue              []core.MessageDeliveryOf[T]
}

// EventCloner copies a supported callback without retaining mutable payload
// aliases. The adapter supplies this port to keep event codecs out of time
// algorithms. It must not call the manager; ExportState invokes it under locks.
type EventCloner func(core.OutboundEvent) (core.OutboundEvent, error)

// ExportState returns a deep, handle-sorted snapshot without emitting callbacks.
// The evaluator lease prevents an in-flight queue drain/grant from being captured
// halfway through. The application must separately quiesce federation services
// and background work across the complete multi-service save transaction.
func (m *ManagerOf[T]) ExportState(fed core.FederationName, clone EventCloner) (*ManagerStateOf[T], error) {
	ext := extOfOf[T](m)
	lock := ext.evaluatorLock(fed)
	lock.Lock()
	defer lock.Unlock()
	m.states.mu.Lock()
	defer m.states.mu.Unlock()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	ext.haltedMu.Lock()
	defer ext.haltedMu.Unlock()

	_, halted := ext.halted[fed]
	timeout, hasTimeout := ext.stallTimeouts[fed]
	if !hasTimeout {
		timeout = m.opts.StallTimeout
	}
	state := &ManagerStateOf[T]{Halted: halted, StallTimeout: timeout}
	byHandle := make(map[core.FederateHandle]*FederateStateOf[T])
	for key, fs := range m.states.states {
		if key.fed != fed {
			continue
		}
		byHandle[key.h] = exportRegulationState(key.h, fs)
	}
	now := m.opts.Clock.Now()
	for key, ns := range ext.states {
		if key.fed != fed {
			continue
		}
		entry := byHandle[key.h]
		if entry == nil {
			entry = &FederateStateOf[T]{Handle: key.h}
			byHandle[key.h] = entry
		}
		if err := exportAdvanceState(entry, ns, now, scheduledPauseAtLocked(ext, fed, key.h), clone); err != nil {
			return nil, err
		}
	}
	for _, entry := range byHandle {
		state.Federates = append(state.Federates, *entry)
	}
	slices.SortFunc(state.Federates, func(a, b FederateStateOf[T]) int {
		if a.Handle < b.Handle {
			return -1
		}
		if a.Handle > b.Handle {
			return 1
		}
		return 0
	})
	if len(state.Federates) == 0 && !halted && !hasTimeout {
		return nil, nil
	}
	if err := validateManagerStateOf[T](state); err != nil {
		return nil, err
	}
	return state, nil
}

// PrepareImportState validates and detaches the complete image before returning
// an infallible, idempotent commit. The commit replaces only fed, preserves the
// existing evaluator mutex, and emits no callbacks or journal records. A nil or
// zero state clears that federation. The application owns global quiescence.
func (m *ManagerOf[T]) PrepareImportState(fed core.FederationName, state *ManagerStateOf[T], clone EventCloner) (func(), error) {
	if err := validateManagerStateOf[T](state); err != nil {
		return nil, err
	}
	var saved *ManagerStateOf[T]
	if state != nil {
		copyState := *state
		copyState.Federates = append([]FederateStateOf[T](nil), state.Federates...)
		for i := range copyState.Federates {
			entry := &copyState.Federates[i]
			if entry.PreservedReceiveTime != nil {
				received := *entry.PreservedReceiveTime
				entry.PreservedReceiveTime = &received
			}
			if entry.ReceivedTSOTime != nil {
				received := *entry.ReceivedTSOTime
				entry.ReceivedTSOTime = &received
			}
			if entry.FlushMinimum != nil {
				minimum := *entry.FlushMinimum
				entry.FlushMinimum = &minimum
			}
			if entry.RequestedLookahead != nil {
				target := *entry.RequestedLookahead
				entry.RequestedLookahead = &target
			}
			if entry.Elapsed != nil {
				history := *entry.Elapsed
				entry.Elapsed = &history
			}
			entry.Queue = append([]core.MessageDeliveryOf[T](nil), entry.Queue...)
			for j := range entry.Queue {
				event, err := clonePersistentEvent(clone, entry.Queue[j].Event)
				if err != nil {
					return nil, fmt.Errorf("time snapshot federate %d: %w", entry.Handle, err)
				}
				entry.Queue[j].Event = event
			}
			entry.ROQueue = append([]core.MessageDeliveryOf[T](nil), entry.ROQueue...)
			for j := range entry.ROQueue {
				event, err := clonePersistentEvent(clone, entry.ROQueue[j].Event)
				if err != nil {
					return nil, fmt.Errorf("time snapshot federate %d: %w", entry.Handle, err)
				}
				entry.ROQueue[j].Event = event
			}
		}
		saved = &copyState
	}
	var once sync.Once
	return func() { once.Do(func() { m.commitState(fed, saved) }) }, nil
}

// ImportState is the checked immediate restore path. Multi-service restoration
// should prepare every manager first and then call the returned commits.
func (m *ManagerOf[T]) ImportState(fed core.FederationName, state *ManagerStateOf[T], clone EventCloner) error {
	commit, err := m.PrepareImportState(fed, state, clone)
	if err != nil {
		return err
	}
	commit()
	return nil
}

func (m *ManagerOf[T]) commitState(fed core.FederationName, state *ManagerStateOf[T]) {
	ext := extOfOf[T](m)
	lock := ext.evaluatorLock(fed)
	lock.Lock()
	defer lock.Unlock()
	m.states.mu.Lock()
	defer m.states.mu.Unlock()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	delete(ext.deliveryRetries, fed)
	ext.haltedMu.Lock()
	defer ext.haltedMu.Unlock()
	for key := range m.states.states {
		if key.fed == fed {
			delete(m.states.states, key)
		}
	}
	for key := range ext.states {
		if key.fed == fed {
			delete(ext.states, key)
		}
	}
	delete(ext.halted, fed)
	delete(ext.stallTimeouts, fed)
	if state == nil {
		return
	}
	if state.Halted {
		ext.halted[fed] = struct{}{}
	}
	if state.StallTimeout != 0 {
		ext.stallTimeouts[fed] = state.StallTimeout
	}
	now := m.opts.Clock.Now()
	for _, entry := range state.Federates {
		key := federateKey{fed: fed, h: entry.Handle}
		if entry.HasRegulationState {
			m.states.states[key] = entry.restoreRegulationState()
		}
		if !entry.HasAdvanceState {
			continue
		}
		ns := entry.restoreAdvanceState(now)
		ext.states[key] = ns
	}
}

func clonePersistentEvent(clone EventCloner, event core.OutboundEvent) (core.OutboundEvent, error) {
	if clone == nil || event == nil {
		return nil, fmt.Errorf("time snapshot requires a supported event cloner and non-nil callback")
	}
	copyEvent, err := clone(event)
	if err != nil {
		return nil, err
	}
	if copyEvent == nil {
		return nil, fmt.Errorf("time snapshot event cloner returned nil")
	}
	return copyEvent, nil
}

func validPersistentTimeOf[T core.LogicalTimeScalar](t T) bool {
	return !scalarNaN(t) && t >= 0
}

func validateManagerStateOf[T core.LogicalTimeScalar](state *ManagerStateOf[T]) error {
	if state == nil {
		return nil
	}
	if state.StallTimeout < 0 {
		return fmt.Errorf("time snapshot has negative stall timeout")
	}
	if state.StallTimeout == 0 && (state.Halted || len(state.Federates) > 0) {
		return fmt.Errorf("time snapshot has no stall timeout")
	}
	seen := make(map[core.FederateHandle]bool, len(state.Federates))
	for _, entry := range state.Federates {
		bad := func(reason string) error { return fmt.Errorf("time snapshot federate %d: %s", entry.Handle, reason) }
		if entry.Handle == 0 || seen[entry.Handle] {
			return bad("zero or duplicate handle")
		}
		seen[entry.Handle] = true
		if err := entry.validatePersistentState(); err != nil {
			return err
		}
	}
	return nil
}

func (entry FederateStateOf[T]) invalidSnapshot(reason string) error {
	return fmt.Errorf("time snapshot federate %d: %s", entry.Handle, reason)
}

func (entry FederateStateOf[T]) validatePersistentState() error {
	for _, validate := range []func() error{
		entry.validateStores,
		entry.validateLookaheadReduction,
		entry.validateEnableState,
		entry.validateStrictReceiveHistory,
		entry.validateTSOReceiveHistory,
		entry.validateRegulationLookahead,
		entry.validateAdvanceRequest,
		entry.validateDeliveredMinimum,
		entry.validateAdvanceStore,
		entry.validateQueues,
	} {
		if err := validate(); err != nil {
			return err
		}
	}
	return nil
}

func (entry FederateStateOf[T]) validateStores() error {
	bad := entry.invalidSnapshot
	if err := validateElapsedState(entry.Elapsed); err != nil {
		return bad(err.Error())
	}
	if entry.Elapsed != nil && !entry.HasAdvanceState {
		return bad("elapsed history without advance state")
	}
	if entry.StrictZeroLookahead && !entry.HasAdvanceState {
		return bad("strict zero-lookahead history without advance state")
	}
	if !entry.HasRegulationState && !entry.HasAdvanceState {
		return bad("missing state stores")
	}
	return nil
}

func (entry FederateStateOf[T]) validateLookaheadReduction() error {
	bad := entry.invalidSnapshot
	if !validLookaheadOf[T](entry.Lookahead) {
		return bad("invalid lookahead")
	}
	if entry.RequestedLookahead != nil {
		if !entry.HasRegulationState || !entry.Regulating || !validLookaheadOf[T](*entry.RequestedLookahead) || *entry.RequestedLookahead >= entry.Lookahead {
			return bad("invalid requested lookahead reduction")
		}
		if !validLookaheadOf[T](entry.LookaheadOriginValue) || entry.LookaheadOriginValue < entry.Lookahead || !validPersistentTimeOf[T](entry.LookaheadOriginTime) || entry.LookaheadOriginTime > entry.CurrentTime {
			return bad("invalid lookahead reduction origin")
		}
		fs := federateStateOf[T]{lookahead: entry.Lookahead, lookaheadTarget: *entry.RequestedLookahead, reducingLookahead: true, lookaheadOriginTime: entry.LookaheadOriginTime, lookaheadOriginValue: entry.LookaheadOriginValue}
		if fs.projectedLookahead(entry.CurrentTime) != entry.Lookahead {
			return bad("lookahead does not match reduction origin")
		}
	} else if entry.LookaheadOriginTime != 0 || entry.LookaheadOriginValue != 0 {
		return bad("lookahead reduction origin without target")
	}
	return nil
}

func (entry FederateStateOf[T]) validateEnableState() error {
	bad := entry.invalidSnapshot
	if !entry.HasRegulationState && (entry.Regulating || entry.Constrained || entry.Lookahead != 0) {
		return bad("regulation data without state")
	}
	if entry.RegulationPending || entry.ConstrainedPending {
		if !entry.HasRegulationState || !entry.HasAdvanceState || entry.PendingRequest {
			return bad("pending enable without state or during advance")
		}
		if entry.RegulationPending && entry.Regulating || entry.ConstrainedPending && entry.Constrained {
			return bad("enabled mode also pending")
		}
	}
	return nil
}

func (entry FederateStateOf[T]) validateStrictReceiveHistory() error {
	bad := entry.invalidSnapshot
	if entry.PreservedReceiveTime != nil && (!entry.HasRegulationState || !entry.HasAdvanceState || !entry.Constrained || !validPersistentTimeOf[T](*entry.PreservedReceiveTime) || *entry.PreservedReceiveTime > entry.CurrentTime) {
		return bad("invalid constrained receiver strict history")
	}
	return nil
}

func (entry FederateStateOf[T]) validateTSOReceiveHistory() error {
	bad := entry.invalidSnapshot
	if entry.ReceivedTSOTime != nil {
		received := *entry.ReceivedTSOTime
		if !entry.HasRegulationState || !entry.HasAdvanceState || !entry.Constrained || !validPersistentTimeOf[T](received) {
			return bad("invalid constrained TSO delivery history")
		}
		if received > entry.CurrentTime && !entry.RegulationPending && (!entry.PendingRequest || entry.Mode == ModeFQR || received > entry.RequestedTime) {
			return bad("TSO delivery history exceeds advance horizon")
		}
	}
	return nil
}

func (entry FederateStateOf[T]) validateRegulationLookahead() error {
	bad := entry.invalidSnapshot
	if entry.RegulationPending {
		if !validLookaheadOf[T](entry.RegulationLookahead) {
			return bad("invalid pending regulation lookahead")
		}
	} else if entry.RegulationLookahead != 0 {
		return bad("regulation lookahead without pending enable")
	}
	if !entry.Regulating && entry.Lookahead != 0 {
		return bad("lookahead without regulation")
	}
	return nil
}

func (entry FederateStateOf[T]) validateAdvanceRequest() error {
	bad := entry.invalidSnapshot
	if !validPersistentTimeOf[T](entry.CurrentTime) || !validPersistentTimeOf[T](entry.RequestedTime) {
		return bad("invalid logical time")
	}
	if entry.PendingAge < 0 || (!entry.HasPendingSince && entry.PendingAge != 0) {
		return bad("invalid pending age")
	}
	if entry.PendingRequest {
		if entry.Mode < ModeNER || entry.Mode > ModeFQR || entry.RequestedTime < entry.CurrentTime {
			return bad("invalid advance request")
		}
	} else if entry.Mode != ModeNone || entry.RequestedTime != 0 || entry.HasPendingSince || entry.PendingAge != 0 {
		return bad("request data without pending request")
	}
	return nil
}

func (entry FederateStateOf[T]) validateDeliveredMinimum() error {
	bad := entry.invalidSnapshot
	if entry.FlushMinimum != nil {
		if !entry.HasAdvanceState || !entry.PendingRequest || (entry.Mode != ModeFQR && !entry.Mode.usesNextMessageTarget()) || !validPersistentTimeOf[T](*entry.FlushMinimum) || *entry.FlushMinimum < entry.CurrentTime || entry.Mode.usesNextMessageTarget() && *entry.FlushMinimum > entry.RequestedTime {
			return bad("invalid FQR/next-message delivered minimum")
		}
	}
	return nil
}

func (entry FederateStateOf[T]) validateAdvanceStore() error {
	bad := entry.invalidSnapshot
	if !entry.HasAdvanceState && (entry.CurrentTime != 0 || entry.PendingRequest || entry.AsynchronousDelivery || len(entry.Queue) > 0 || len(entry.ROQueue) > 0) {
		return bad("advance data without state")
	}
	return nil
}

func (entry FederateStateOf[T]) validateQueues() error {
	bad := entry.invalidSnapshot
	for _, queued := range entry.Queue {
		if queued.Recipient != entry.Handle || !validPersistentTimeOf[T](queued.Timestamp) || queued.ReceiveOrder {
			return bad("invalid queued destination or timestamp")
		}
		if queued.RetractionHandle != 0 && queued.Sender == 0 {
			return bad("retraction handle without sender")
		}
	}
	for _, queued := range entry.ROQueue {
		if queued.Recipient != entry.Handle || !queued.ReceiveOrder || queued.Timestamp != 0 || queued.RetractionHandle != 0 {
			return bad("invalid RO queue entry")
		}
	}
	return nil
}

func exportRegulationState[T core.LogicalTimeScalar](h core.FederateHandle, fs *federateStateOf[T]) *FederateStateOf[T] {
	entry := &FederateStateOf[T]{Handle: h, HasRegulationState: true, Regulating: fs.regulating, Constrained: fs.constrained, Lookahead: fs.lookahead}
	entry.RegulationPending = fs.regulationPending
	entry.RegulationLookahead = fs.regulationLookahead
	entry.ConstrainedPending = fs.constrainedPending
	if fs.preservedReceiveTime != nil {
		received := *fs.preservedReceiveTime
		entry.PreservedReceiveTime = &received
	}
	if fs.receivedTSOTime != nil {
		received := *fs.receivedTSOTime
		entry.ReceivedTSOTime = &received
	}
	if fs.reducingLookahead {
		target := fs.lookaheadTarget
		entry.RequestedLookahead = &target
		entry.LookaheadOriginTime = fs.lookaheadOriginTime
		entry.LookaheadOriginValue = fs.lookaheadOriginValue
	}
	return entry
}

func exportAdvanceState[T core.LogicalTimeScalar](entry *FederateStateOf[T], ns *nerStateOf[T], now, paused stdtime.Time, clone EventCloner) error {
	entry.HasAdvanceState = true
	if ns.elapsed != nil {
		ns.elapsed.accrue(now, ns.pendingNER)
		history := ns.elapsed.ElapsedTimeState
		entry.Elapsed = &history
	}
	entry.CurrentTime = ns.currentTime
	entry.StrictZeroLookahead = ns.strictZeroLookahead
	entry.PendingRequest = ns.pendingNER
	entry.RequestedTime = ns.requestedTime
	entry.Mode = ns.mode
	if ns.flushMinimum != nil {
		minimum := *ns.flushMinimum
		entry.FlushMinimum = &minimum
	}
	entry.AsynchronousDelivery = ns.asyncDelivery
	entry.HasPendingSince = !ns.pendingSince.IsZero()
	if entry.HasPendingSince {
		entry.PendingAge = now.Sub(ns.pendingSince)
		if !paused.IsZero() {
			entry.PendingAge -= now.Sub(paused)
		}
		if entry.PendingAge < 0 {
			entry.PendingAge = 0
		}
	}
	for _, queued := range ns.tsoBuffer {
		event, err := clonePersistentEvent(clone, queued.event)
		if err != nil {
			return fmt.Errorf("time snapshot federate %d: %w", entry.Handle, err)
		}
		entry.Queue = append(entry.Queue, core.MessageDeliveryOf[T]{Recipient: entry.Handle, Timestamp: queued.timestamp, Event: event, Sender: queued.sender, RetractionHandle: queued.retractionHandle})
	}
	for _, queued := range ns.roBuffer {
		event, err := clonePersistentEvent(clone, queued.event)
		if err != nil {
			return fmt.Errorf("time snapshot federate %d: %w", entry.Handle, err)
		}
		entry.ROQueue = append(entry.ROQueue, core.MessageDeliveryOf[T]{Recipient: entry.Handle, Event: event, Sender: queued.sender, ReceiveOrder: true})
	}
	return nil
}

func (entry FederateStateOf[T]) restoreRegulationState() *federateStateOf[T] {
	fs := &federateStateOf[T]{regulating: entry.Regulating, constrained: entry.Constrained, lookahead: entry.Lookahead}
	fs.regulationPending = entry.RegulationPending
	fs.regulationLookahead = entry.RegulationLookahead
	fs.constrainedPending = entry.ConstrainedPending
	if entry.PreservedReceiveTime != nil {
		received := *entry.PreservedReceiveTime
		fs.preservedReceiveTime = &received
	}
	if entry.ReceivedTSOTime != nil {
		received := *entry.ReceivedTSOTime
		fs.receivedTSOTime = &received
	}
	if entry.RequestedLookahead != nil {
		fs.lookaheadTarget = *entry.RequestedLookahead
		fs.reducingLookahead = true
		fs.lookaheadOriginTime = entry.LookaheadOriginTime
		fs.lookaheadOriginValue = entry.LookaheadOriginValue
	}
	return fs
}

func (entry FederateStateOf[T]) restoreAdvanceState(now stdtime.Time) *nerStateOf[T] {
	ns := &nerStateOf[T]{currentTime: entry.CurrentTime, pendingNER: entry.PendingRequest, requestedTime: entry.RequestedTime, mode: entry.Mode, asyncDelivery: entry.AsynchronousDelivery}
	ns.strictZeroLookahead = entry.StrictZeroLookahead
	if entry.FlushMinimum != nil {
		minimum := *entry.FlushMinimum
		ns.flushMinimum = &minimum
	}
	// Saved durations resume at the import clock. Downtime is not attributed
	// to either state, and the fresh token fences pre-restore publications.
	if entry.Elapsed != nil {
		ns.elapsed = &elapsedAccounting{ElapsedTimeState: *entry.Elapsed, last: now}
	}
	if entry.HasPendingSince {
		ns.pendingSince = now.Add(-entry.PendingAge)
	}
	for _, queued := range entry.Queue {
		ns.tsoBuffer = append(ns.tsoBuffer, bufferedTSOEventOf[T]{timestamp: queued.Timestamp, event: queued.Event, sender: queued.Sender, retractionHandle: queued.RetractionHandle})
	}
	for _, queued := range entry.ROQueue {
		ns.roBuffer = append(ns.roBuffer, bufferedTSOEventOf[T]{event: queued.Event, sender: queued.Sender, receiveOrder: true})
	}
	return ns
}
