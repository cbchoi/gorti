package ownership

import (
	"context"
	"errors"
	"math"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const MaxCallbackStateCount uint64 = 100000

type ownershipCallbackSession struct {
	scope            core.OwnershipCallbackScope
	enabled          bool
	restoreRequired  bool
	lastID           uint64
	completedThrough uint64
	completed        map[uint64]struct{}
	pending          map[uint64]*terminalCallback
}

type terminalCallback struct {
	event           *CallbackEvent
	enqueueing      bool
	admitted        bool
	lastAttempt     uint64
	inflightAttempt uint64
	sentAttempt     uint64
	reportedAttempt uint64
}

var _ core.OwnershipCallbackCoordinator = (*Manager)(nil)

func validCallbackScope(scope core.OwnershipCallbackScope) bool {
	return scope.Federation != "" && scope.Recipient != 0 && scope.FederationGeneration != 0
}

func (st *federationState) callbackSessionLocked(fed core.FederationName, recipient core.FederateHandle) *ownershipCallbackSession {
	session := st.callbackSessions[recipient]
	if session == nil {
		session = &ownershipCallbackSession{scope: core.OwnershipCallbackScope{Federation: fed, Recipient: recipient}, completed: map[uint64]struct{}{}, pending: map[uint64]*terminalCallback{}}
		st.callbackSessions[recipient] = session
	}
	return session
}

func (m *Manager) EnableOwnershipCallbackReceipts(ctx context.Context, scope core.OwnershipCallbackScope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validCallbackScope(scope) {
		return core.ErrOwnershipCallbackScope
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(scope.Federation)
	session := st.callbackSessions[scope.Recipient]
	if session != nil && session.restoreRequired {
		return core.ErrOwnershipCallbackScope
	}
	if session != nil && session.enabled && session.scope == scope {
		return nil
	}
	if session != nil && len(session.pending) != 0 {
		return core.ErrOwnershipAcquisitionPending
	}
	for key := range st.pendingAcquires {
		if key.acquirer == scope.Recipient {
			return core.ErrOwnershipAcquisitionPending
		}
	}
	if session == nil && uint64(len(st.callbackSessions)) >= MaxCallbackStateCount {
		return core.ErrOwnershipCallbackCapacity
	}
	session = st.callbackSessionLocked(scope.Federation, scope.Recipient)
	session.scope, session.enabled = scope, true
	return nil
}

// Called under m.mu before any associated ownership/pending-request mutation.
// Validate the complete fanout before allocating any identity or obligation.
func (m *Manager) registerTerminalCallbacksLocked(st *federationState, fed core.FederationName, deliveries []core.OutboxDelivery) error {
	for _, delivery := range deliveries {
		if event, ok := delivery.Event.(*CallbackEvent); ok {
			event.Attributes = slices.Clone(event.Attributes)
			slices.Sort(event.Attributes)
			event.Attributes = slices.Compact(event.Attributes)
		}
	}
	if err := m.validateTerminalCallbacksLocked(st, deliveries); err != nil {
		return err
	}
	for _, delivery := range deliveries {
		session := st.callbackSessionLocked(fed, delivery.Recipient)
		event := delivery.Event.(*CallbackEvent)
		session.lastID++
		event.CallbackID, event.CallbackScope, event.RequireInvocationReceipt = session.lastID, session.scope, session.enabled
		if event.identity == nil {
			event.identity = &callbackIdentity{}
		}
		*event.identity = callbackIdentity{scope: session.scope, id: session.lastID, strict: session.enabled}
		session.pending[event.CallbackID] = &terminalCallback{event: cloneCallback(event)}
	}
	return nil
}

func (m *Manager) validateTerminalCallbacksLocked(st *federationState, deliveries []core.OutboxDelivery) error {
	var outstanding uint64
	for _, session := range st.callbackSessions {
		outstanding += session.lastID - session.completedThrough
	}
	if uint64(len(deliveries)) > MaxCallbackStateCount || outstanding > MaxCallbackStateCount-uint64(len(deliveries)) {
		return core.ErrOwnershipCallbackCapacity
	}
	counts := make(map[core.FederateHandle]uint64)
	for _, delivery := range deliveries {
		if err := validateTerminalDelivery(delivery); err != nil {
			return err
		}
		counts[delivery.Recipient]++
	}
	newSessions := uint64(0)
	for recipient := range counts {
		if st.callbackSessions[recipient] == nil {
			newSessions++
		}
	}
	if uint64(len(st.callbackSessions))+newSessions > MaxCallbackStateCount {
		return core.ErrOwnershipCallbackCapacity
	}
	for recipient, count := range counts {
		session := st.callbackSessions[recipient]
		var last, completed uint64
		if session != nil {
			if session.restoreRequired {
				return core.ErrOwnershipCallbackScope
			}
			last, completed = session.lastID, session.completedThrough
		}
		if count > math.MaxUint64-last || count > m.opts.CallbackWindow || last-completed > m.opts.CallbackWindow-count {
			return core.ErrOwnershipCallbackCapacity
		}
	}
	return nil
}

func validateTerminalDelivery(delivery core.OutboxDelivery) error {
	event, ok := delivery.Event.(*CallbackEvent)
	if !ok || delivery.Recipient == 0 || event.Object == 0 || len(event.Attributes) == 0 || uint64(len(event.Attributes)) > MaxCallbackStateCount || (event.Kind != CallbackAcquisitionNotification && event.Kind != CallbackUnavailable && event.Kind != CallbackAcquisitionCancellation) {
		return core.ErrOwnershipCallbackReceipt
	}
	for _, attr := range event.Attributes {
		if attr == 0 {
			return core.ErrOwnershipCallbackReceipt
		}
	}
	return nil
}

func cloneCallback(event *CallbackEvent) *CallbackEvent {
	cloned := *event
	cloned.Attributes, cloned.Tag = slices.Clone(event.Attributes), slices.Clone(event.Tag)
	return &cloned
}

func (session *ownershipCallbackSession) completeLocked(id uint64) {
	delete(session.pending, id)
	session.completed[id] = struct{}{}
	for {
		next := session.completedThrough + 1
		if _, ok := session.completed[next]; !ok {
			break
		}
		delete(session.completed, next)
		session.completedThrough = next
	}
}

// No ownership/object lock crosses projection or queue admission. A failed
// admission retains its event; strict recipients also retain successful ones.
func (m *Manager) sendTerminalCallback(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, event *CallbackEvent) error {
	m.mu.Lock()
	st := m.fed[fed]
	if st == nil {
		m.mu.Unlock()
		return core.ErrOwnershipCallbackScope
	}
	session := st.callbackSessions[recipient]
	if session == nil {
		m.mu.Unlock()
		return core.ErrOwnershipCallbackScope
	}
	entry := session.pending[event.CallbackID]
	if entry == nil {
		m.mu.Unlock()
		return nil
	}
	if entry.event.identity != event.identity {
		m.mu.Unlock()
		return core.ErrOwnershipCallbackScope
	}
	if entry.enqueueing {
		m.mu.Unlock()
		return core.ErrOwnershipCallbackBusy
	}
	entry.enqueueing = true
	cloned := cloneCallback(entry.event)
	m.mu.Unlock()
	err := m.opts.Outbox.Send(ctx, fed, recipient, cloned)
	m.mu.Lock()
	if m.fed[fed] == st && st.callbackSessions[recipient] == session && session.pending[event.CallbackID] == entry && entry.event.identity == event.identity {
		entry.enqueueing = false
		entry.admitted = err == nil
		if err == nil && !session.enabled {
			session.completeLocked(event.CallbackID)
		}
	}
	m.mu.Unlock()
	if err != nil && m.opts.OnTerminalDeliveryError != nil {
		m.opts.OnTerminalDeliveryError(fed, recipient, event.CallbackID, err)
	}
	return err
}

func (m *Manager) finishTerminalAdmission(fed core.FederationName, deliveries []core.OutboxDelivery, admissionError error) {
	m.mu.Lock()
	st := m.fed[fed]
	if st != nil {
		for _, delivery := range deliveries {
			event := delivery.Event.(*CallbackEvent)
			session := st.callbackSessions[delivery.Recipient]
			if session == nil {
				continue
			}
			entry := session.pending[event.CallbackID]
			if entry != nil && entry.event.identity == event.identity {
				entry.admitted = admissionError == nil
				if admissionError == nil && !session.enabled {
					session.completeLocked(event.CallbackID)
				}
			}
		}
	}
	m.mu.Unlock()
	if admissionError != nil && m.opts.OnTerminalDeliveryError != nil {
		for _, delivery := range deliveries {
			m.opts.OnTerminalDeliveryError(fed, delivery.Recipient, delivery.Event.(*CallbackEvent).CallbackID, admissionError)
		}
	}
}

func (m *Manager) callbackSessionForScopeLocked(scope core.OwnershipCallbackScope) (*ownershipCallbackSession, error) {
	st := m.fed[scope.Federation]
	if st == nil {
		return nil, core.ErrOwnershipCallbackScope
	}
	session := st.callbackSessions[scope.Recipient]
	if session == nil || !session.enabled || session.restoreRequired || session.scope != scope {
		return nil, core.ErrOwnershipCallbackScope
	}
	return session, nil
}

func (m *Manager) BeginOwnershipCallbackDelivery(ctx context.Context, scope core.OwnershipCallbackScope, id uint64) (core.OwnershipCallbackReceipt, func(bool), error) {
	if err := ctx.Err(); err != nil {
		return core.OwnershipCallbackReceipt{}, nil, err
	}
	m.mu.Lock()
	session, err := m.callbackSessionForScopeLocked(scope)
	if err != nil {
		m.mu.Unlock()
		return core.OwnershipCallbackReceipt{}, nil, err
	}
	entry := session.pending[id]
	if id != 0 && id <= session.completedThrough {
		m.mu.Unlock()
		return core.OwnershipCallbackReceipt{}, nil, core.ErrOwnershipCallbackComplete
	}
	if _, complete := session.completed[id]; complete {
		m.mu.Unlock()
		return core.OwnershipCallbackReceipt{}, nil, core.ErrOwnershipCallbackComplete
	}
	if entry == nil || entry.lastAttempt == math.MaxUint64 {
		m.mu.Unlock()
		return core.OwnershipCallbackReceipt{}, nil, core.ErrOwnershipCallbackReceipt
	}
	if entry.inflightAttempt != 0 {
		m.mu.Unlock()
		return core.OwnershipCallbackReceipt{}, nil, core.ErrOwnershipCallbackBusy
	}
	entry.lastAttempt++
	entry.admitted = false
	attempt := entry.lastAttempt
	entry.inflightAttempt = attempt
	receipt := core.OwnershipCallbackReceipt{FederationGeneration: scope.FederationGeneration, CallbackEpoch: scope.CallbackEpoch, CallbackID: id, DeliveryAttempt: attempt}
	m.mu.Unlock()
	var once sync.Once
	finish := func(sent bool) {
		once.Do(func() {
			m.finishOwnershipCallbackDelivery(scope, session, entry, id, attempt, sent)
		})
	}
	return receipt, finish, nil
}

func (m *Manager) finishOwnershipCallbackDelivery(scope core.OwnershipCallbackScope, session *ownershipCallbackSession, entry *terminalCallback, id, attempt uint64, sent bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, err := m.callbackSessionForScopeLocked(scope)
	if err != nil || current != session || session.pending[id] != entry || entry.inflightAttempt != attempt {
		return
	}
	entry.inflightAttempt = 0
	if sent {
		entry.sentAttempt = attempt
		if entry.reportedAttempt == attempt {
			session.completeLocked(id)
		}
	} else if entry.reportedAttempt == attempt {
		entry.reportedAttempt = 0
	}
}

func (m *Manager) ReportOwnershipCallbackInvocation(ctx context.Context, scope core.OwnershipCallbackScope, receipt core.OwnershipCallbackReceipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if receipt.CallbackID == 0 || receipt.DeliveryAttempt == 0 || receipt.FederationGeneration != scope.FederationGeneration || receipt.CallbackEpoch != scope.CallbackEpoch {
		return core.ErrOwnershipCallbackReceipt
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	session, err := m.callbackSessionForScopeLocked(scope)
	if err != nil {
		return err
	}
	if receipt.CallbackID <= session.completedThrough {
		return nil
	}
	if _, done := session.completed[receipt.CallbackID]; done {
		return nil
	}
	entry := session.pending[receipt.CallbackID]
	if entry == nil {
		return core.ErrOwnershipCallbackReceipt
	}
	if receipt.DeliveryAttempt == entry.sentAttempt && entry.sentAttempt != 0 {
		session.completeLocked(receipt.CallbackID)
		return nil
	}
	if receipt.DeliveryAttempt == entry.inflightAttempt && entry.inflightAttempt != 0 {
		entry.reportedAttempt = receipt.DeliveryAttempt
		return nil
	}
	return core.ErrOwnershipCallbackReceipt
}

func (m *Manager) RetryOwnershipCallbacks(ctx context.Context, scope core.OwnershipCallbackScope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.RLock()
	st := m.fed[scope.Federation]
	var session *ownershipCallbackSession
	if st != nil {
		session = st.callbackSessions[scope.Recipient]
	}
	if session == nil || session.restoreRequired || (session.enabled && session.scope != scope) {
		m.mu.RUnlock()
		return core.ErrOwnershipCallbackScope
	}
	var events []*CallbackEvent
	for _, entry := range session.pending {
		events = append(events, cloneCallback(entry.event))
	}
	m.mu.RUnlock()
	slices.SortFunc(events, func(a, b *CallbackEvent) int { return compareUint64(a.CallbackID, b.CallbackID) })
	var failures []error
	for _, event := range events {
		if err := m.sendTerminalCallback(ctx, scope.Federation, scope.Recipient, event); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (st *federationState) hasAcquisitionObligationLocked(recipient core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) bool {
	for key := range st.pendingAcquires {
		if key.acquirer == recipient && key.obj == obj && (len(attrs) == 0 || slices.Contains(attrs, key.attr)) {
			return true
		}
	}
	if session := st.callbackSessions[recipient]; session != nil {
		for _, entry := range session.pending {
			if entry.event.Object != obj {
				continue
			}
			if len(attrs) == 0 {
				return true
			}
			for _, attr := range attrs {
				if slices.Contains(entry.event.Attributes, attr) {
					return true
				}
			}
		}
	}
	return false
}

func (m *Manager) HasAcquisitionObligation(fed core.FederationName, recipient core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	return st != nil && st.hasAcquisitionObligationLocked(recipient, obj, attrs)
}

func (m *Manager) RebindOwnershipCallbackScope(scope core.OwnershipCallbackScope) error {
	if !validCallbackScope(scope) {
		return core.ErrOwnershipCallbackScope
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.fed[scope.Federation]
	if st == nil {
		return core.ErrOwnershipCallbackScope
	}
	session := st.callbackSessions[scope.Recipient]
	if session == nil || !session.enabled {
		return core.ErrOwnershipCallbackScope
	}
	if session.scope == scope {
		if session.restoreRequired {
			return core.ErrOwnershipCallbackScope
		}
		return nil
	}
	rebindCallbackSessionLocked(session, scope)
	return nil
}
