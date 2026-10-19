package sync

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	gosync "sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var ErrNotImplemented = errors.New("sync: not implemented")

// SyncPointState retains the established public state type name.
type SyncPointState int //nolint:revive // Renaming the exported type would break existing callers.

const (
	StateUnknown SyncPointState = iota
	StateAnnounced
	// Achievements are complete; actual synchronized invocations may still be pending.
	StateAchieved
)

type MembersResolver func(core.FederationName) []core.FederateHandle

type Options struct {
	Outbox   core.Outbox
	EventLog core.EventLog
	Members  MembersResolver
	// Required for the assessed production profile. The zero value preserves
	// legacy embedded compositions, which cannot prove callback invocation.
	RequireInvocations bool
}

type Manager struct {
	opts Options
	// Serializes admission and state publication without holding mu across outbox I/O.
	opMu gosync.Mutex
	mu   gosync.RWMutex
	fed  map[core.FederationName]*federationState
}

var _ core.SyncCoordinator = (*Manager)(nil)
var _ core.SyncLifecycle = (*Manager)(nil)
var _ core.SyncCallbackObserver = (*Manager)(nil)
var _ core.SyncPendingQuery = (*Manager)(nil)

type federationState struct {
	points       map[string]*syncPoint
	nextSequence uint64
	nextEpoch    uint64
	pending      []CallbackState
	obligations  map[uint64]CallbackState
}

type syncPoint struct {
	tag      []byte
	state    SyncPointState
	required map[core.FederateHandle]struct{}
	achieved map[core.FederateHandle]struct{}
	failed   map[core.FederateHandle]struct{}
	// dynamic is exclusively the legacy no-members-resolver profile.
	dynamic    bool
	allMembers bool
	epoch      uint64
	callbacks  map[core.FederateHandle]InvocationState
}

func New(opts Options) (*Manager, error) {
	if opts.Outbox == nil {
		return nil, errors.New("sync: outbox required")
	}
	if opts.RequireInvocations {
		if opts.Members == nil {
			return nil, errors.New("sync: strict profile requires membership resolver")
		}
		if _, ok := opts.Outbox.(core.ReservableOutbox); !ok {
			return nil, errors.New("sync: strict profile requires reservable outbox")
		}
	}
	return &Manager{opts: opts, fed: map[core.FederationName]*federationState{}}, nil
}

func (m *Manager) RequiresInvocationReceipts() bool { return m.opts.RequireInvocations }

func (m *Manager) PendingFederations() []core.FederationName {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var names []core.FederationName
	for name, state := range m.fed {
		if len(state.pending) != 0 {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func emptyFederationState() *federationState {
	return &federationState{points: map[string]*syncPoint{}, obligations: map[uint64]CallbackState{}}
}

func cloneFederationState(src *federationState) *federationState {
	if src == nil {
		return emptyFederationState()
	}
	dst := emptyFederationState()
	dst.nextSequence, dst.nextEpoch = src.nextSequence, src.nextEpoch
	for label, p := range src.points {
		dst.points[label] = &syncPoint{tag: slices.Clone(p.tag), state: p.state, required: maps.Clone(p.required),
			achieved: maps.Clone(p.achieved), failed: maps.Clone(p.failed), dynamic: p.dynamic,
			allMembers: p.allMembers, epoch: p.epoch, callbacks: maps.Clone(p.callbacks)}
	}
	for _, pending := range src.pending {
		dst.pending = append(dst.pending, cloneCallbackState(pending))
	}
	for sequence, callback := range src.obligations {
		dst.obligations[sequence] = cloneCallbackState(callback)
	}
	return dst
}

func (m *Manager) OnFederationDestroyed(fed core.FederationName) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	delete(m.fed, fed)
	m.mu.Unlock()
}

// transition publishes a complete new state only after all-recipient admission.
// Membership has already committed when lifecycle is true: retain the new state
// and its ordered pending callbacks if admission fails, rather than lose the hook.
func (m *Manager) transition(ctx context.Context, fed core.FederationName, lifecycle bool, change func(*federationState) error) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.RLock()
	next := cloneFederationState(m.fed[fed])
	m.mu.RUnlock()
	if err := change(next); err != nil {
		return err
	}
	deliveries := make([]core.OutboxDelivery, len(next.pending))
	for i, callback := range next.pending {
		deliveries[i] = callback.delivery()
	}
	var reservation core.OutboxReservation
	if len(deliveries) != 0 {
		if out, ok := m.opts.Outbox.(core.ReservableOutbox); ok {
			var err error
			reservation, err = out.Reserve(ctx, fed, deliveries)
			if err != nil {
				if lifecycle {
					m.publish(fed, next)
				}
				return err
			}
			if reservation == nil {
				if lifecycle {
					m.publish(fed, next)
				}
				return errors.New("sync: nil outbox reservation")
			}
			defer reservation.Release()
		} else {
			// This compatibility path is intentionally not the strict profile.
			for _, delivery := range deliveries {
				if err := m.opts.Outbox.Send(ctx, fed, delivery.Recipient, delivery.Event); err != nil {
					return err
				}
			}
		}
	}
	pending := next.pending
	next.pending = nil
	if !m.opts.RequireInvocations {
		for _, callback := range pending {
			delete(next.obligations, callback.Sequence)
		}
	}
	m.publish(fed, next)
	if reservation != nil {
		if err := reservation.Commit(); err != nil {
			// A conforming reservation cannot fail after admission. Retain the
			// obligation instead of claiming completion on a broken adapter.
			retry := cloneFederationState(next)
			retry.pending = pending
			m.publish(fed, retry)
			return err
		}
	}
	return nil
}

func (m *Manager) publish(fed core.FederationName, state *federationState) {
	m.mu.Lock()
	m.fed[fed] = state
	m.mu.Unlock()
}

func (st *federationState) enqueue(callback CallbackState) error {
	if st.nextSequence == ^uint64(0) {
		return errors.New("sync: callback sequence exhausted")
	}
	st.nextSequence++
	callback.Sequence = st.nextSequence
	if point := st.points[callback.Label]; point != nil {
		callback.Epoch = point.epoch
		inv := point.callbacks[callback.Recipient]
		switch callback.Kind {
		case callbackAnnounce:
			inv.Announce = callback.Sequence
		case callbackSynchronized:
			inv.Synchronized = callback.Sequence
		}
		if callback.Kind == callbackAnnounce || callback.Kind == callbackSynchronized {
			point.callbacks[callback.Recipient] = inv
		}
	}
	st.pending = append(st.pending, cloneCallbackState(callback))
	st.obligations[callback.Sequence] = cloneCallbackState(callback)
	return nil
}

func (m *Manager) Register(ctx context.Context, fed core.FederationName, label string, tag []byte, required []core.FederateHandle) error {
	return m.register(ctx, fed, label, tag, required, nil)
}

func (m *Manager) RegisterBy(ctx context.Context, fed core.FederationName, registrant core.FederateHandle, label string, tag []byte, required []core.FederateHandle) error {
	return m.register(ctx, fed, label, tag, required, &registrant)
}

func (m *Manager) register(ctx context.Context, fed core.FederationName, label string, tag []byte, requested []core.FederateHandle, registrant *core.FederateHandle) error {
	registered := false
	err := m.transition(ctx, fed, false, func(st *federationState) error {
		fail := func(reason RegistrationFailureReason) error {
			if registrant == nil {
				return core.ErrSyncPointAlreadyRegistered
			}
			return st.enqueue(CallbackState{Kind: callbackRegistrationFailed, Label: label, Recipient: *registrant, Reason: reason})
		}
		if _, exists := st.points[label]; exists {
			return fail(RegistrationFailureLabelNotUnique)
		}
		allMembers := len(requested) == 0
		members, valid := m.registrationMembers(fed, requested, registrant)
		if !valid {
			return fail(RegistrationFailureSetMemberNotJoined)
		}
		if st.nextEpoch == ^uint64(0) {
			return errors.New("sync: label epoch exhausted")
		}
		st.nextEpoch++
		point := &syncPoint{tag: slices.Clone(tag), state: StateAnnounced, required: handleSet(members),
			achieved: map[core.FederateHandle]struct{}{}, failed: map[core.FederateHandle]struct{}{},
			dynamic: allMembers && m.opts.Members == nil, allMembers: allMembers,
			epoch: st.nextEpoch, callbacks: map[core.FederateHandle]InvocationState{}}
		st.points[label] = point
		if registrant != nil {
			if err := st.enqueue(CallbackState{Kind: callbackRegistrationSucceeded, Label: label, Recipient: *registrant}); err != nil {
				return err
			}
		}
		for _, h := range sortedHandles(point.required) {
			if err := st.enqueue(CallbackState{Kind: callbackAnnounce, Label: label, Tag: slices.Clone(tag), Recipient: h, Required: sortedHandles(point.required)}); err != nil {
				return err
			}
		}
		registered = true
		return nil
	})
	if err == nil && registered && m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtRegistered, label: label})
	}
	return err
}

func (m *Manager) registrationMembers(fed core.FederationName, requested []core.FederateHandle, registrant *core.FederateHandle) ([]core.FederateHandle, bool) {
	if m.opts.Members == nil {
		return requested, true
	}
	joined := m.opts.Members(fed)
	if len(requested) == 0 {
		return joined, true
	}
	if registrant != nil {
		for _, h := range requested {
			if !slices.Contains(joined, h) {
				return nil, false
			}
		}
	}
	return requested, true
}

func (m *Manager) Achieve(ctx context.Context, fed core.FederationName, h core.FederateHandle, label string) error {
	return m.achieve(ctx, fed, h, label, true)
}

func (m *Manager) AchieveWith(ctx context.Context, fed core.FederationName, h core.FederateHandle, label string, successfully bool) error {
	return m.achieve(ctx, fed, h, label, successfully)
}

func (m *Manager) achieve(ctx context.Context, fed core.FederationName, h core.FederateHandle, label string, successfully bool) error {
	synchronized := false
	err := m.transition(ctx, fed, false, func(st *federationState) error {
		point := st.points[label]
		if point == nil {
			return core.ErrSyncPointNotRegistered
		}
		if point.dynamic {
			point.required[h] = struct{}{}
		}
		if _, required := point.required[h]; !required {
			return core.ErrSyncPointNotRegistered
		}
		// Entry, not return, establishes announcement knowledge. An SDK must
		// report entry before running a handler that can reenter this service.
		if m.opts.RequireInvocations && !point.callbacks[h].AnnounceEntered {
			return core.ErrSyncPointNotRegistered
		}
		if m.announcementPending(st, point, label, h) {
			return core.ErrSyncPointNotRegistered
		}
		if _, achieved := point.achieved[h]; achieved {
			return core.ErrSyncPointAlreadyAchieved
		}
		point.achieved[h] = struct{}{}
		if !successfully {
			point.failed[h] = struct{}{}
		}
		if err := m.complete(st, label, point); err != nil {
			return err
		}
		synchronized = point.state == StateAchieved
		return nil
	})
	if err == nil && m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtAchieved, label: label, federate: h})
		if synchronized {
			_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtSynchronized, label: label})
		}
	}
	return err
}

func (m *Manager) announcementPending(st *federationState, point *syncPoint, label string, h core.FederateHandle) bool {
	for _, pending := range st.pending {
		if pending.Label == label && pending.Recipient == h && pending.Kind == callbackAnnounce {
			if !m.opts.RequireInvocations || !point.callbacks[h].AnnounceEntered {
				return true
			}
		}
	}
	return false
}

func (m *Manager) complete(st *federationState, label string, point *syncPoint) error {
	if point.state == StateAchieved {
		return nil
	}
	for h := range point.required {
		if _, achieved := point.achieved[h]; !achieved {
			return nil
		}
	}
	point.state = StateAchieved
	for _, h := range sortedHandles(point.required) {
		if err := st.enqueue(CallbackState{Kind: callbackSynchronized, Label: label, Recipient: h, Failed: sortedHandles(point.failed)}); err != nil {
			return err
		}
	}
	if m.opts.RequireInvocations && len(point.required) == 0 {
		delete(st.points, label)
	}
	return nil
}

func (m *Manager) FederateJoined(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	return m.transition(ctx, fed, true, func(st *federationState) error {
		for _, label := range sortedLabels(st) {
			point := st.points[label]
			if !point.allMembers || point.state != StateAnnounced {
				continue
			}
			if _, exists := point.required[h]; exists {
				continue
			}
			point.required[h] = struct{}{}
			if err := st.enqueue(CallbackState{Kind: callbackAnnounce, Label: label, Recipient: h, Tag: slices.Clone(point.tag), Required: sortedHandles(point.required)}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (m *Manager) FederateResigned(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	return m.transition(ctx, fed, true, func(st *federationState) error {
		st.pending = slices.DeleteFunc(st.pending, func(callback CallbackState) bool { return callback.Recipient == h })
		for sequence, callback := range st.obligations {
			if callback.Recipient == h {
				delete(st.obligations, sequence)
			}
		}
		for _, label := range sortedLabels(st) {
			point := st.points[label]
			delete(point.required, h)
			delete(point.achieved, h)
			delete(point.failed, h)
			delete(point.callbacks, h)
			if err := m.complete(st, label, point); err != nil {
				return err
			}
			m.retire(st, label, point)
		}
		return nil
	})
}

// RetryPending drains durable lifecycle obligations in order. Single-delivery
// reservations allow a late join across more points than one queue can hold.
func (m *Manager) RetryPending(ctx context.Context, fed core.FederationName) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	out, ok := m.opts.Outbox.(core.ReservableOutbox)
	if !ok {
		return errors.New("sync: retry requires reservable outbox")
	}
	for {
		m.mu.RLock()
		next := cloneFederationState(m.fed[fed])
		m.mu.RUnlock()
		if len(next.pending) == 0 {
			return nil
		}
		reservation, err := out.Reserve(ctx, fed, []core.OutboxDelivery{next.pending[0].delivery()})
		if err != nil {
			return err
		}
		if reservation == nil {
			return errors.New("sync: nil retry reservation")
		}
		if err := reservation.Commit(); err != nil {
			reservation.Release()
			return err
		}
		reservation.Release()
		if !m.opts.RequireInvocations {
			delete(next.obligations, next.pending[0].Sequence)
		}
		next.pending = next.pending[1:]
		m.publish(fed, next)
	}
}

func (m *Manager) ObserveSyncCallbackEntry(ctx context.Context, fed core.FederationName, h core.FederateHandle, kind core.SyncCallbackKind, label string, sequence uint64) error {
	return m.observeCallback(ctx, fed, h, kind, label, sequence, true)
}

func (m *Manager) ObserveSyncCallback(ctx context.Context, fed core.FederationName, h core.FederateHandle, kind core.SyncCallbackKind, label string, sequence uint64) error {
	return m.observeCallback(ctx, fed, h, kind, label, sequence, false)
}

func (m *Manager) observeCallback(_ context.Context, fed core.FederationName, h core.FederateHandle, kind core.SyncCallbackKind, label string, sequence uint64, entry bool) error {
	if sequence == 0 {
		return errors.New("sync: invocation requires callback sequence")
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.RLock()
	next := cloneFederationState(m.fed[fed])
	m.mu.RUnlock()
	callback, outstanding := next.obligations[sequence]
	if !outstanding || callback.Recipient != h || callback.Label != label || callback.Kind.observedKind() != kind {
		return nil // Completed/stale receipts cannot revive a retired epoch.
	}
	if callbackPending(next.pending, sequence) {
		return errors.New("sync: callback has not been delivered")
	}
	if !entry && m.opts.RequireInvocations && !callback.Entered {
		return errors.New("sync: callback completion requires authenticated entry")
	}
	if entry {
		callback.Entered = true
		next.obligations[sequence] = callback
	} else {
		delete(next.obligations, sequence)
	}
	point := next.points[label]
	if point == nil || point.epoch != callback.Epoch {
		m.publish(fed, next)
		return nil
	}
	publish, err := m.observePointCallback(next, point, h, kind, label, sequence, entry)
	if err != nil {
		return err
	}
	if publish {
		m.publish(fed, next)
	}
	return nil
}

func (m *Manager) observePointCallback(next *federationState, point *syncPoint, h core.FederateHandle, kind core.SyncCallbackKind, label string, sequence uint64, entry bool) (bool, error) {
	inv := point.callbacks[h]
	switch kind {
	case core.SyncCallbackAnnounced:
		if inv.Announce != sequence {
			return false, nil
		}
		if entry {
			inv.AnnounceEntered = true
		} else {
			inv.AnnounceInvoked = true
		}
	case core.SyncCallbackSynchronized:
		if inv.Synchronized != sequence || point.state != StateAchieved {
			return false, nil
		}
		if !entry {
			inv.SynchronizedInvoked = true
		}
	case core.SyncCallbackRegistrationSucceeded, core.SyncCallbackRegistrationFailed:
		return true, nil
	default:
		return false, fmt.Errorf("sync: unknown callback kind %d", kind)
	}
	point.callbacks[h] = inv
	m.retire(next, label, point)
	return true, nil
}

func callbackPending(pending []CallbackState, sequence uint64) bool {
	return slices.ContainsFunc(pending, func(callback CallbackState) bool { return callback.Sequence == sequence })
}

func (m *Manager) retire(st *federationState, label string, point *syncPoint) {
	if !m.opts.RequireInvocations || point.state != StateAchieved {
		return
	}
	for h := range point.required {
		if !point.callbacks[h].SynchronizedInvoked {
			return
		}
	}
	delete(st.points, label)
}

func (m *Manager) Snapshot(fed core.FederationName) []core.SyncPointInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st == nil {
		return nil
	}
	var out []core.SyncPointInfo
	for _, label := range sortedLabels(st) {
		point := st.points[label]
		out = append(out, core.SyncPointInfo{Label: label, State: syncStateToCore(point.state),
			RequiredHandles: sortedHandles(point.required), AchievedHandles: sortedHandles(point.achieved)})
	}
	return out
}

func syncStateToCore(state SyncPointState) core.SyncPointSnapshotState {
	switch state {
	case StateAnnounced:
		return core.SyncPointStateAnnounced
	case StateAchieved:
		return core.SyncPointStateAchieved
	default:
		return core.SyncPointStateUnknown
	}
}

func (m *Manager) QueryState(fed core.FederationName, label string) SyncPointState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if st := m.fed[fed]; st != nil {
		if point := st.points[label]; point != nil {
			return point.state
		}
	}
	return StateUnknown
}

func sortedLabels(st *federationState) []string {
	labels := make([]string, 0, len(st.points))
	for label := range st.points {
		labels = append(labels, label)
	}
	slices.Sort(labels)
	return labels
}

func handleSet(handles []core.FederateHandle) map[core.FederateHandle]struct{} {
	set := make(map[core.FederateHandle]struct{}, len(handles))
	for _, h := range handles {
		set[h] = struct{}{}
	}
	return set
}

func sortedHandles(set map[core.FederateHandle]struct{}) []core.FederateHandle {
	handles := make([]core.FederateHandle, 0, len(set))
	for h := range set {
		handles = append(handles, h)
	}
	slices.Sort(handles)
	return handles
}
