package time

import (
	"context"
	"errors"
	"math"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var (
	ErrTimeRegulationPending  = errors.New("request for time-regulation pending")
	ErrTimeConstrainedPending = errors.New("request for time-constrained pending")
)

// TimeRegulationEnabled completes an accepted 8.2 request at its safe time.
//
// Preserve the established HLA API name.
//
//revive:disable-next-line:exported
type TimeRegulationEnabledOf[T core.LogicalTimeScalar] struct{ Time T }

func (*TimeRegulationEnabledOf[T]) Seq() uint64 { return 0 }

// TimeConstrainedEnabled completes an accepted 8.5 request at its safe time.
//
// Preserve the established HLA API name.
//
//revive:disable-next-line:exported
type TimeConstrainedEnabledOf[T core.LogicalTimeScalar] struct{ Time T }

func (*TimeConstrainedEnabledOf[T]) Seq() uint64 { return 0 }

// pendingEnableError is called under the evaluator lease, before ext.mu.
func (m *ManagerOf[T]) pendingEnableError(fed core.FederationName, h core.FederateHandle) error {
	m.states.mu.Lock()
	defer m.states.mu.Unlock()
	if fs := m.states.getLocked(fed, h); fs != nil {
		if fs.regulationPending {
			return ErrTimeRegulationPending
		}
		if fs.constrainedPending {
			return ErrTimeConstrainedPending
		}
	}
	return nil
}

func (m *ManagerOf[T]) requestEnable(ctx context.Context, fed core.FederationName, h core.FederateHandle, regulation bool, lookahead T) error {
	ext := extOfOf[T](m)
	lease := ext.evaluatorLock(fed)
	lease.Lock()
	if ext.isHalted(fed) {
		lease.Unlock()
		return core.ErrFederationHalted
	}
	if regulation && !validLookaheadOf[T](lookahead) {
		lease.Unlock()
		return core.ErrTimeInvalidLookahead
	}
	m.states.mu.Lock()
	ext.mu.Lock()
	fs, ns := m.states.getLocked(fed, h), ext.getLocked(fed, h)
	err := validateEnableRequest(fs, ns, regulation)
	if err == nil {
		fs = m.states.getOrCreateLocked(fed, h)
		ext.getOrCreateLocked(fed, h)
		if regulation {
			fs.regulationPending, fs.regulationLookahead = true, lookahead
		} else {
			fs.constrainedPending = true
		}
	}
	ext.mu.Unlock()
	m.states.mu.Unlock()
	lease.Unlock()
	if err != nil {
		return err
	}
	// Acceptance is asynchronous: transport backpressure retains the request.
	if err = m.tryGrantPending(ctx, fed); errors.Is(err, core.ErrFederateOverflow) {
		return nil
	}
	return err
}

// completePendingEnablesLocked runs inside the federation evaluator. Call it
// before evaluating ordinary advance requests so new regulators participate
// in the next grant decision. The returned handles need post-unlock observers.
func (m *ManagerOf[T]) completePendingEnablesLocked(ctx context.Context, fed core.FederationName) ([]core.FederateHandle, error) {
	if extOfOf[T](m).isHalted(fed) {
		return nil, core.ErrFederationHalted
	}
	m.states.mu.Lock()
	var handles []core.FederateHandle
	for key, fs := range m.states.states {
		if key.fed == fed && (fs.regulationPending || fs.constrainedPending) {
			handles = append(handles, key.h)
		}
	}
	m.states.mu.Unlock()
	slices.Sort(handles)
	var changed []core.FederateHandle
	for _, h := range handles {
		for _, regulation := range []bool{true, false} {
			target, ready := m.enableTargetLocked(fed, h, regulation)
			if !ready {
				continue
			}
			if err := m.emitEnableLocked(ctx, fed, h, regulation, target); err != nil {
				return changed, err
			}
			if !slices.Contains(changed, h) {
				changed = append(changed, h)
			}
		}
	}
	return changed, nil
}

// enableTargetLocked implements 8.2/8.3 and 8.5/8.6. For this Float64 time
// representation, Nextafter expresses a peer's strict delivery boundary;
// it is not the logical-time-interval epsilon defined in clause 12.3.
func (m *ManagerOf[T]) enableTargetLocked(fed core.FederationName, h core.FederateHandle, regulation bool) (T, bool) {
	ext := extOfOf[T](m)
	m.states.mu.Lock()
	defer m.states.mu.Unlock()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	target, ready := m.enableTargetStateLocked(ext, fed, h, regulation)
	if !ready {
		return 0, false
	}
	if save := ext.scheduledSaves[fed]; save != nil {
		fs := m.states.getLocked(fed, h)
		if scheduledRecipientPausedLocked(ext, fed, h) || (regulation && fs.constrained && target > save.target) {
			return 0, false
		}
	}
	return target, true
}

// enableTargetStateLocked computes the implicit grant independently of a save
// reservation. The evaluator lease and both time-state locks are held by the
// caller, allowing the save coordinator to inspect a crossing implicit TARA.
func (m *ManagerOf[T]) enableTargetStateLocked(ext *nerStoreOf[T], fed core.FederationName, h core.FederateHandle, regulation bool) (T, bool) {
	fs, ns := m.states.getLocked(fed, h), ext.getLocked(fed, h)
	if fs == nil || ns == nil || regulation && !fs.regulationPending || !regulation && !fs.constrainedPending {
		return 0, false
	}
	target := ns.currentTime
	if regulation {
		if fs.constrained && fs.receivedTSOTime != nil {
			target = max(target, *fs.receivedTSOTime)
		}
		var possible bool
		target, possible = m.regulationEnableTimeLocked(ext, fed, h, target, fs.regulationLookahead)
		if !possible {
			return 0, false
		}
	}
	if fs.constrained || !regulation {
		if galt := m.availableHorizonLocked(ext, fed, h); galt != nil && !horizonBeyond(*galt, target, false) {
			return 0, false
		}
	}
	return target, true
}

// recordReceivedGrant tracks a receive guarantee independently of the outgoing
// zero-lookahead rule, which resets when regulation is first enabled (8.2).
func (fs *federateStateOf[T]) recordReceivedGrant(mode AdvanceMode, at T) {
	if fs == nil || !fs.constrained {
		return
	}
	if strictAdvance(mode) {
		value := at
		fs.preservedReceiveTime = &value
	} else if fs.preservedReceiveTime != nil && at > *fs.preservedReceiveTime {
		fs.preservedReceiveTime = nil
	}
}

// recordOrderedDelivery retains the receive-order frontier if data delivery
// succeeds but its accompanying grant/enabled callback cannot yet be admitted.
// The caller holds the evaluator lease, but neither time-state lock.
func (m *ManagerOf[T]) recordOrderedDelivery(fed core.FederationName, h core.FederateHandle, b bufferedTSOEventOf[T]) {
	if b.receiveOrder || internalTSO(b.event) {
		return
	}
	ext := extOfOf[T](m)
	m.states.mu.Lock()
	defer m.states.mu.Unlock()
	ext.mu.Lock()
	defer ext.mu.Unlock()
	fs, ns := m.states.getLocked(fed, h), ext.getLocked(fed, h)
	if fs == nil || !fs.constrained || ns == nil || ns.pendingNER && ns.mode == ModeFQR {
		return
	}
	if fs.receivedTSOTime == nil || b.timestamp > *fs.receivedTSOTime {
		value := b.timestamp
		fs.receivedTSOTime = &value
	}
}

func minimumEnableTimeOf[T core.LogicalTimeScalar](current, lookahead, peerTime T, exclusive bool) (T, bool) {
	current = max(T(0), current)
	if integerScalar[T]() {
		lower := peerTime - lookahead
		if exclusive {
			var possible bool
			lower, possible = successor(lower)
			if !possible {
				return 0, false
			}
		}
		return max(current, lower), true
	}
	legal := func(at T) bool {
		bound := addTimeBoundOf[T](at, lookahead)
		if exclusive {
			return bound > peerTime
		}
		return bound >= peerTime
	}
	if legal(current) {
		return current, true
	}
	if math.IsInf(float64(peerTime), 1) {
		return 0, false
	}
	floor := peerTime
	if exclusive {
		floor = T(math.Nextafter(float64(floor), math.Inf(1)))
	}
	upper := max(current, floor-lookahead)
	if !legal(upper) {
		upper = T(math.Nextafter(float64(upper), math.Inf(1)))
	}
	if math.IsInf(float64(upper), 1) || !legal(upper) {
		largest := float64(math.MaxFloat64)
		upper = T(largest)
	}
	if !legal(upper) {
		return 0, false
	}
	// Rounded subtraction is only an upper-bound guess, not a lower bound:
	// IEEE distinguishable addition can make an earlier time legal.
	// Positive Float64 bit patterns have numeric order. Search at most 63 bits;
	// ULP walking from zero could otherwise require billions of iterations.
	lo, hi := math.Float64bits(float64(current)), math.Float64bits(float64(upper))
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		if legal(T(math.Float64frombits(mid))) {
			hi = mid
		} else {
			lo = mid
		}
	}
	return T(math.Float64frombits(hi)), true
}

func (m *ManagerOf[T]) takeEnableMessages(fed core.FederationName, h core.FederateHandle, regulation bool, at T) []bufferedTSOEventOf[T] {
	ext := extOfOf[T](m)
	m.states.mu.Lock()
	fs := m.states.getLocked(fed, h)
	constrained := fs != nil && fs.constrained
	m.states.mu.Unlock()
	if regulation && !constrained {
		return nil
	}
	ext.mu.Lock()
	ns := ext.getOrCreateLocked(fed, h)
	ro := ns.roBuffer
	ns.roBuffer = nil
	ext.mu.Unlock()
	limit := at
	if !regulation {
		// Queued messages older than the activation time remain received RO.
		// Equal/future messages become eligible for TSO after the callback.
		limit = predecessor(at)
	}
	return append(ro, m.takeBufferedTSO(fed, h, &limit, regulation)...)
}

func (m *ManagerOf[T]) emitEnableLocked(ctx context.Context, fed core.FederationName, h core.FederateHandle, regulation bool, at T) error {
	release := m.takeEnableMessages(fed, h, regulation, at)
	var callback core.OutboundEvent = &TimeConstrainedEnabledOf[T]{Time: at}
	if regulation {
		callback = &TimeRegulationEnabledOf[T]{Time: at}
	}
	var reservation, lifecycle core.OutboxReservation
	if out, ok := m.opts.Outbox.(core.ReservableOutbox); ok {
		var err error
		reservation, release, err = m.reserveTimeCallback(ctx, fed, h, out, release, callback, "enable")
		if err != nil {
			return err
		}
		defer reservation.Release()
		lifecycle = m.prepareDeletionDelivery(fed, h, release)
		if lifecycle != nil {
			defer lifecycle.Release()
		}
	} else {
		if err := m.deliverBufferedSequence(ctx, fed, h, release); err != nil {
			return err
		}
		release = nil
	}

	// Commit state before visibility, restoring it if callback admission fails.
	// The evaluator and both state locks prevent peers from seeing a false mode.
	ext := extOfOf[T](m)
	m.states.mu.Lock()
	ext.mu.Lock()
	fs, ns := m.states.getLocked(fed, h), ext.getLocked(fed, h)
	previous, previousTime, previousStrict := *fs, ns.currentTime, ns.strictZeroLookahead
	ns.currentTime = at
	if regulation {
		fs.recordReceivedGrant(ModeTARA, at)
		fs.regulating, fs.lookahead = true, fs.regulationLookahead
		fs.regulationPending, fs.regulationLookahead = false, 0
		fs.reducingLookahead, fs.lookaheadTarget = false, 0
		fs.lookaheadOriginTime, fs.lookaheadOriginValue = 0, 0
		ns.strictZeroLookahead = false
	} else {
		fs.constrained, fs.constrainedPending = true, false
		fs.preservedReceiveTime = nil
		fs.receivedTSOTime = nil
	}
	var err error
	if reservation != nil {
		err = reservation.Commit()
	} else {
		err = m.opts.Outbox.Send(ctx, fed, h, callback)
	}
	if err != nil {
		*fs, ns.currentTime, ns.strictZeroLookahead = previous, previousTime, previousStrict
	} else if reservation != nil {
		m.recordBufferedRetractionDeliveries(ctx, fed, h, release)
	}
	ext.mu.Unlock()
	m.states.mu.Unlock()
	if err != nil {
		m.restoreBufferedMessages(fed, h, release)
		return err
	}
	if lifecycle != nil {
		return lifecycle.Commit()
	}
	return nil
}

func validateEnableRequest[T core.LogicalTimeScalar](fs *federateStateOf[T], ns *nerStateOf[T], regulation bool) error {
	switch {
	case ns != nil && ns.pendingNER:
		return core.ErrTimeAdvancingState
	case regulation && fs != nil && fs.regulationPending:
		return ErrTimeRegulationPending
	case !regulation && fs != nil && fs.constrainedPending:
		return ErrTimeConstrainedPending
	case regulation && fs != nil && fs.regulating:
		return core.ErrTimeAlreadyRegulating
	case !regulation && fs != nil && fs.constrained:
		return core.ErrTimeAlreadyConstrained
	}
	return nil
}

func (m *ManagerOf[T]) regulationEnableTimeLocked(ext *nerStoreOf[T], fed core.FederationName, h core.FederateHandle, target, lookahead T) (T, bool) {
	for key, peer := range m.states.states {
		if key.fed != fed || key.h == h || !peer.constrained {
			continue
		}
		other := ext.getLocked(fed, key.h)
		if other == nil {
			continue
		}
		var possible bool
		frontier := other.currentTime
		if peer.receivedTSOTime != nil {
			frontier = max(frontier, *peer.receivedTSOTime)
		}
		exclusive := peer.preservedReceiveTime != nil && *peer.preservedReceiveTime == frontier
		target, possible = minimumEnableTimeOf[T](target, lookahead, frontier, exclusive)
		if !possible {
			return 0, false
		}
	}
	return target, true
}
