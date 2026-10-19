package time

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ErrDuplicateNER is the package-local alias for core.ErrTimeAdvancingState
// (M21 TASK-202b — see docs/M21_DISPATCH_PLAN.md §2.3.1). It is returned
// by every advance primitive (NER/NMRA/TAR/TARA/FQR via dispatchAdvance)
// when the federate already has an outstanding advance for which no
// grant has been emitted.
//
// The acceptance contract (rti/spec/M3/ner_test.go) accepts ANY non-nil
// error here; aliasing to the core sentinel lets the gRPC wire layer
// (errs.go) match via errors.Is without importing the time package.
// Existing call sites that compare to time.ErrDuplicateNER continue to
// work because errors.Is(time.ErrDuplicateNER, core.ErrTimeAdvancingState)
// is true (identity).
var ErrDuplicateNER = core.ErrTimeAdvancingState

// nerState is the per-federate time-advance bookkeeping side-table. It
// lives alongside (and not inside) federateState because federateState
// is owned by Wave 1A's regulation.go and the W2 ownership rules forbid
// modifying that file.
//
// The struct name retains the historical "ner" prefix from M3 W2 — when
// only NextMessageRequest existed — to avoid renaming the side-table
// across every consumer (regulatingSnapshot, stall.go, federationMembers).
// Conceptually it now holds state for ANY of the five time-advance
// primitives (NER, NMRA, TAR, TARA, FQR); the active mode is recorded
// in the `mode` field and consulted by tryGrantPending to dispatch the
// per-mode grant condition.
//
// Fields:
//   - currentTime: the federate's last granted (or initial) logical
//     time. Initialised to 0 on first interaction.
//   - pendingNER: true while ANY mode's request is outstanding (queued
//     but not yet granted). Name retained for binary-compat with M3
//     consumers; treat as "pendingRequest". Cleared on every default-
//     policy grant (M38 GA — §8.8 one request, one grant); alternative
//     GrantStrategy implementations may keep it via ClearPending=false.
//   - requestedTime: the t parameter of the outstanding request.
//     Meaningful only when pendingNER is true.
//   - mode: which AdvanceMode produced the outstanding request. Drives
//     the per-mode grant predicate in decideGrant. Zero (ModeNone) when
//     not pending.
//   - pendingSince: wall-clock time (via Manager.opts.Clock) at which
//     pendingNER transitioned to true. Used by W3 stall detection
//     (CheckStalls) to compare against the federation's StallTimeout.
//     Meaningful only when pendingNER is true; reset on full grant.
type nerStateOf[T core.LogicalTimeScalar] struct {
	elapsed       *elapsedAccounting
	currentTime   T
	pendingNER    bool
	requestedTime T
	mode          AdvanceMode
	pendingSince  stdtime.Time
	// TAR/NMR history at currentTime survives same-time Available grants.
	strictZeroLookahead bool
	// Earliest TSO already delivered during a partially completed FQR/NMR.
	flushMinimum *T

	// Asynchronous delivery applies to RO, not to the TSO timestamp gate.
	asyncDelivery bool

	// Timestamped events awaiting advancement or held by a scheduled save.
	tsoBuffer []bufferedTSOEventOf[T]
	roBuffer  []bufferedTSOEventOf[T]
}

// nerStore is the goroutine-safe table from federateKey to nerState.
// A separate mutex from stateStore keeps lock granularity manageable
// without forcing Wave 1A's file to grow.
//
// Lock order, when both are held: stateStore.mu BEFORE nerStore.mu.
// In the current implementation only nerStore.mu is held during the
// NER critical section; the regulation snapshot is read under
// stateStore.mu, copied out, then nerStore.mu is taken. This avoids
// deadlock entirely.
type nerStoreOf[T core.LogicalTimeScalar] struct {
	mu             sync.Mutex
	states         map[federateKey]*nerStateOf[T]
	haltedMu       sync.Mutex
	halted         map[core.FederationName]struct{}
	evalMu         sync.Mutex
	evalLocks      map[core.FederationName]*sync.Mutex
	stallTimeouts  map[core.FederationName]stdtime.Duration         // restored policy; guarded by mu
	scheduledSaves map[core.FederationName]*scheduledTimeBarrierOf[ // guarded by mu; not checkpoint state
	T]
	deliveryRetries map[core.FederationName]bool // guarded by mu; runtime retry hints only
}

func newNERStoreOf[T core.LogicalTimeScalar]() *nerStoreOf[T] {
	return &nerStoreOf[T]{
		states:        make(map[federateKey]*nerStateOf[T]),
		halted:        make(map[core.FederationName]struct{}),
		evalLocks:     make(map[core.FederationName]*sync.Mutex),
		stallTimeouts: make(map[core.FederationName]stdtime.Duration),
	}
}

// evaluatorLock returns the mutex serialising grant evaluation for fed.
// The map mutex is held only for lookup/allocation; callers hold the returned
// federation mutex across the complete tryGrantPending fixed-point loop.
func (n *nerStoreOf[T]) evaluatorLock(fed core.FederationName) *sync.Mutex {
	n.evalMu.Lock()
	defer n.evalMu.Unlock()
	if lock := n.evalLocks[fed]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	n.evalLocks[fed] = lock
	return lock
}

// getOrCreateLocked returns the nerState for (fed, h), creating a
// zero-valued entry if absent. Caller MUST hold n.mu.
func (n *nerStoreOf[T]) getOrCreateLocked(fed core.FederationName, h core.FederateHandle) *nerStateOf[T] {
	k := federateKey{fed: fed, h: h}
	st, ok := n.states[k]
	if !ok {
		st = &nerStateOf[T]{}
		n.states[k] = st
	}
	return st
}

// getLocked returns the nerState for (fed, h), or nil if none. Caller
// MUST hold n.mu.
func (n *nerStoreOf[T]) getLocked(fed core.FederationName, h core.FederateHandle) *nerStateOf[T] {
	return n.states[federateKey{fed: fed, h: h}]
}

// managerExtensions holds W2 (and forthcoming W3) per-Manager mutable
// state without modifying manager.go's Manager struct or constructor.
// The W2 file-ownership rule forbids both, so the side-table pattern
// below threads the extra storage through a package-private sync.Map
// keyed by *Manager. This adds one map lookup per call but keeps the
// stable public types untouched.
//
// W3 may add a "halt mu / halt set" field here without breaking W2's
// contract — both stores live behind the same lazy-init helper.
var managerExt sync.Map

// extOf returns the *nerStore associated with m, lazily creating one
// on first access. Goroutine-safe: concurrent callers race on
// LoadOrStore and the loser's freshly-allocated store is discarded.
func extOfOf[T core.LogicalTimeScalar](m *ManagerOf[T]) *nerStoreOf[T] {
	if value, ok := managerExt.Load(m); ok {
		return value.(*nerStoreOf[T])
	}
	fresh := newNERStoreOf[T]()
	actual, _ := managerExt.LoadOrStore(m, fresh)
	return actual.(*nerStoreOf[T])
}

// regulatingSnapshot returns a deterministic snapshot of every
// regulating federate in fed, suitable for handoff to the pure
// LBTS([]RegulatingFederate) function. The returned slice is sorted
// ascending by FederateHandle so any downstream iteration that depends
// on order is deterministic (LBTS itself does not, but other call
// sites might).
//
// HLA NER contribution rule: a federate with an outstanding NER
// promises not to send any TSO message before requestedTime+lookahead;
// its contribution to LBTS therefore uses requestedTime, not
// currentTime. Non-pending federates contribute currentTime+lookahead
// (the HLA invariant for unmanaged regulators). See
// TestSpec_M3_NER_TwoRegulators_GrantWaits's prose: "LBTS = min(5+1,
// 0+2) = 2" — the 5+1 is fed1's requested+lookahead.
//
// Caller MUST NOT hold s.mu — this function takes it.
func (m *ManagerOf[T]) regulatingSnapshot(fed core.FederationName) []RegulatingFederateOf[T] {
	m.states.mu.Lock()
	defer m.states.mu.Unlock()
	// Copy only the fields needed after releasing the regulation-state lock.
	var out []RegulatingFederateOf[T]
	for k, st := range m.states.states {
		if k.fed != fed {
			continue
		}
		if !st.regulating {
			continue
		}
		out = append(out, RegulatingFederateOf[T]{Handle: k.h, Lookahead: st.lookahead})
	}

	// Sort the immutable snapshot for deterministic iteration.
	slices.SortFunc(out, func(a, b RegulatingFederateOf[T]) int {
		return cmp.Compare(a.Handle, b.Handle)
	})

	// Each regulating federate's NER state (currentTime + pendingNER +
	// requestedTime) lives in nerStore. A federate that has never
	// NER'd has currentTime == 0 (zero value).
	ext := extOfOf[T](m)
	ext.mu.Lock()
	for i := range out {
		entry := &out[i]
		*entry = regulatingBoundSnapshotOf[T](entry.Handle, m.states.getLocked(fed, entry.Handle), ext.getLocked(fed, entry.Handle))
	}
	ext.mu.Unlock()
	return out
}

// nextMessageRequest is the body of Manager.NextMessageRequest.
// Extracted so the exported method's body remains a one-liner and the
// stable signature in manager.go is preserved verbatim.
//
// Cut-2 (M7) note: the four sibling primitives (NMRA, TAR, TARA, FQR)
// share this exact pre-flight + recording shape via dispatchAdvance in
// advance.go; nextMessageRequest is the M3-original entry point and
// stays here both for git-history hygiene and because the M3 spec test
// suite specifically exercises it. Both call sites converge on
// tryGrantPending.
//
// Pre-flight ordering (matters for the ner_test.go assertions):
//  1. Halted-federation check (rejects with ErrFederationHalted).
//  2. Membership is checked by the service boundary, independently of time modes.
//  3. Lookahead check (regulating federates only).
//  4. Duplicate-request check.
//
// Grant logic:
//   - Record the request, mark pendingNER + mode=ModeNER.
//   - Compute LBTS over the regulating set.
//   - For every (handle-sorted) federate whose pending request is
//     satisfied by LBTS per its per-mode predicate, emit TimeAdvanceGrant
//     via Outbox in handle order — ascending — to honour the
//     determinism contract (NFR-DET-1;
//     TestSpec_M3_NER_SimultaneousReady_DeterministicGrantOrder).
//   - Write-ahead through EventLog when non-nil.
func (m *ManagerOf[T]) nextMessageRequest(ctx context.Context, fed core.FederationName, h core.FederateHandle, t T) error {
	return m.dispatchAdvance(ctx, fed, h, t, ModeNER)
}

// tryGrantPending evaluates every pending time-advance request in fed
// against the current LBTS and emits grants — in ascending
// FederateHandle order — for each one whose per-mode predicate is
// satisfied. This loop runs to fixed point: granting one federate's
// request advances its currentTime, which may raise LBTS and unblock
// another peer.
//
// The per-mode dispatch is delegated to decideGrant in advance.go; this
// function owns the iteration discipline (handle-sorted candidates,
// fixed-point restart) and the I/O (emitGrant). Mode-specific semantics
// — strict vs inclusive LBTS, forced-grant vs incremental-grant — live
// in decideGrant.
//
// Cross-mode interaction: when several federates are pending under
// different modes (e.g. fed1 NER, fed2 TAR), each is evaluated against
// the same LBTS but with its own predicate. The TAR family's
// "incremental grant at LBTS" path can fire even when peers are pending,
// so a TAR call may unblock other federates' NERs in a single fixed-
// point round. The "sole-pending" gate for NER/NMRA forced grants
// counts ALL pending requests in fed regardless of mode — the gate's
// purpose is "no peer will ever raise LBTS for me", which is true iff
// nobody else has an outstanding request.
//
// Returns the first error encountered while emitting through Outbox or
// EventLog; partial progress is preserved (a federate granted before
// the failure has its state advanced). In cut-1 the fakeOutbox +
// permissiveEventLog never fail, so the nominal path returns nil.
func (m *ManagerOf[T]) tryGrantPending(ctx context.Context, fed core.FederationName) (err error) {
	defer func() {
		if err != nil && !errors.Is(err, core.ErrFederationHalted) {
			m.markDeliveryRetry(fed)
		}
	}()
	ext := extOfOf[T](m)
	evalLock := ext.evaluatorLock(fed)
	evalLock.Lock()
	var changedStorage [4]core.FederateHandle
	changed := changedStorage[:0]
	defer func() {
		evalLock.Unlock()
		m.finishDeletionDelivery(fed)
		for _, h := range changed {
			m.fireTimeStateChanged(ctx, fed, h)
		}
	}()

	for {
		enabled, err := m.completePendingEnablesLocked(ctx, fed)
		changed = mergeChangedFederates(changed, enabled)
		if err != nil {
			return err
		}
		if err := m.flushPendingMessages(ctx, fed); err != nil {
			return err
		}
		snap := m.regulatingSnapshot(fed)
		// Strategy hook: default LBTSStrategy delegates to the package
		// LBTS function so behavior is unchanged. New(opts) installs
		// defaultLBTS{} when Options.LBTSStrategy is nil.
		lbts := m.lbtsBound(snap)

		// Materialise candidates under ext.mu, then release before any
		// I/O. D-2: never iterate a map without sorting downstream.
		ext.mu.Lock()
		var candidateStorage [4]candidateGrantOf[T]
		cands := candidateStorage[:0]
		cands = pendingGrantCandidatesLocked(ext, fed, cands)
		ext.mu.Unlock()

		if len(cands) == 0 {
			return nil
		}
		sortCandidatesOf[T](cands)
		solePending := len(cands) == 1

		// First pass: collect every candidate that decideGrant says to
		// fire under the current LBTS. Emit them in handle order, then
		// restart the outer loop so a just-granted federate's
		// currentTime advance can unblock further peers.
		fired := false
		for _, c := range cands {
			d := m.decideCandidateGrant(ext, fed, c, lbts, solePending)
			progress, granted, err := m.tryCandidateGrant(ctx, fed, c, d)
			if err != nil {
				return err
			}
			if !progress {
				continue
			}
			if granted && m.opts.OnTimeStateChanged != nil && !slices.Contains(changed, c.h) {
				changed = append(changed, c.h)
			}
			fired = true
			// One emission can change LBTS (via currentTime advance) and
			// invalidate the rest of this pass; restart the outer loop.
			break
		}
		if !fired {
			return nil
		}
	}
}

// emitGrant releases eligible RO and TSO callbacks before the grant. If the
// sequence exceeds transport capacity, successful prefixes remain delivered
// and the unsent suffix stays queued. The grant is reserved before its journal
// append, and time state is committed before the grant becomes visible.
func (m *ManagerOf[T]) emitGrant(ctx context.Context, fed core.FederationName, h core.FederateHandle, t T, clearPending bool) error {
	// Prefer one reservation; bounded-prefix delivery preserves progress when
	// the domain backlog is larger than the transport queue.
	ext := extOfOf[T](m)
	ext.mu.Lock()
	flushing := ext.getLocked(fed, h) != nil && ext.getLocked(fed, h).mode == ModeFQR
	ext.mu.Unlock()
	var tso []bufferedTSOEventOf[T]
	if flushing {
		tso = m.takeBufferedTSOThrough(fed, h, nil, true, &t)
	} else {
		tso = m.takeBufferedTSO(fed, h, &t, true)
	}
	release := append(m.takeBufferedRO(fed, h), tso...)
	var lifecycle core.OutboxReservation
	grant := &TimeAdvanceGrantOf[T]{Time: t}
	var reservation core.OutboxReservation
	if reservable, ok := m.opts.Outbox.(core.ReservableOutbox); ok {
		var err error
		reservation, release, err = m.reserveTimeCallback(ctx, fed, h, reservable, release, grant, "grant")
		if err != nil {
			return err
		}
		defer reservation.Release()
		lifecycle = m.prepareDeletionDelivery(fed, h, release)
		if lifecycle != nil {
			defer lifecycle.Release()
		}
	}

	// (a) Write-ahead to EventLog (cut-1 relaxation: nil silently drops).
	if m.opts.EventLog != nil {
		rec := &TimeAdvanceGrantedRecordOf[T]{Federate: h, Time: t}
		if err := m.opts.EventLog.Append(ctx, fed, rec); err != nil {
			if reservation != nil {
				reservation.Release()
			}
			m.restoreBufferedMessages(fed, h, release)
			return err
		}
	}

	// (b/c) Transfer every eligible message callback followed by the grant. The
	// time state must be committed before the grant becomes visible: otherwise a
	// federate can receive TAG and immediately issue its next TAR while the old
	// pending flag is still set. Hold ext.mu across the visibility boundary so
	// that the next request observes the completed transition.
	if reservation != nil {
		if err := m.commitReservedGrant(ctx, fed, h, t, clearPending, reservation, release); err != nil {
			return err
		}
	} else {
		if err := m.sendUnreservedGrant(ctx, fed, h, t, clearPending, grant, release); err != nil {
			return err
		}
	}
	if lifecycle != nil {
		return lifecycle.Commit()
	}
	return nil
}

// isHalted reports whether the federation has entered the halted
// terminal state. W3 (stall detection) will populate the halted set;
// W2 only reads it. Returns false for any federation when the halted
// set is empty (the common live case).
func (n *nerStoreOf[T]) isHalted(fed core.FederationName) bool {
	n.haltedMu.Lock()
	defer n.haltedMu.Unlock()
	_, ok := n.halted[fed]
	return ok
}

func (m *ManagerOf[T]) commitReservedGrant(ctx context.Context, fed core.FederationName, h core.FederateHandle, t T, clearPending bool, reservation core.OutboxReservation, release []bufferedTSOEventOf[T]) error {
	ext := extOfOf[T](m)
	m.states.mu.Lock()
	ext.mu.Lock()
	ns := ext.getOrCreateLocked(fed, h)
	fs := m.states.getLocked(fed, h)
	var previousRegulation federateStateOf[T]
	if fs != nil {
		previousRegulation = *fs
	}
	previousTime := ns.currentTime
	previousPending := ns.pendingNER
	previousRequested := ns.requestedTime
	previousMode := ns.mode
	previousSince := ns.pendingSince
	previousStrict := ns.strictZeroLookahead
	fs.recordReceivedGrant(ns.mode, t)
	ns.recordGrantBound(t)
	ns.currentTime = t
	fs.advanceLookahead(t)
	if clearPending {
		ns.pendingNER = false
		ns.requestedTime = 0
		ns.mode = ModeNone
		ns.pendingSince = stdtime.Time{}
	}
	if err := reservation.Commit(); err != nil {
		if fs != nil {
			*fs = previousRegulation
		}
		ns.currentTime = previousTime
		ns.pendingNER = previousPending
		ns.requestedTime = previousRequested
		ns.mode = previousMode
		ns.pendingSince = previousSince
		ns.strictZeroLookahead = previousStrict
		ext.mu.Unlock()
		m.states.mu.Unlock()
		m.restoreBufferedMessages(fed, h, release)
		return err
	}
	m.recordBufferedRetractionDeliveries(ctx, fed, h, release)
	ns.elapsed.accrue(m.opts.Clock.Now(), previousPending)
	if clearPending {
		ns.flushMinimum = nil
	}
	ext.mu.Unlock()
	m.states.mu.Unlock()
	return nil
}

func (m *ManagerOf[T]) sendUnreservedGrant(ctx context.Context, fed core.FederationName, h core.FederateHandle, t T, clearPending bool, grant core.OutboundEvent, release []bufferedTSOEventOf[T]) error {
	ext := extOfOf[T](m)
	for i, buffered := range release {
		if err := m.deliverBufferedEvent(ctx, fed, h, buffered); err != nil {
			m.restoreBufferedMessages(fed, h, release[i:])
			return err
		}
	}
	m.states.mu.Lock()
	ext.mu.Lock()
	ns := ext.getOrCreateLocked(fed, h)
	fs := m.states.getLocked(fed, h)
	var previousRegulation federateStateOf[T]
	if fs != nil {
		previousRegulation = *fs
	}
	previousTime := ns.currentTime
	previousPending := ns.pendingNER
	previousRequested := ns.requestedTime
	previousMode := ns.mode
	previousSince := ns.pendingSince
	previousStrict := ns.strictZeroLookahead
	fs.recordReceivedGrant(ns.mode, t)
	ns.recordGrantBound(t)
	ns.currentTime = t
	fs.advanceLookahead(t)
	if clearPending {
		ns.pendingNER = false
		ns.requestedTime = 0
		ns.mode = ModeNone
		ns.pendingSince = stdtime.Time{}
	}
	if err := m.opts.Outbox.Send(ctx, fed, h, grant); err != nil {
		if fs != nil {
			*fs = previousRegulation
		}
		ns.currentTime = previousTime
		ns.pendingNER = previousPending
		ns.requestedTime = previousRequested
		ns.mode = previousMode
		ns.pendingSince = previousSince
		ns.strictZeroLookahead = previousStrict
		ext.mu.Unlock()
		m.states.mu.Unlock()
		return err
	}
	ns.elapsed.accrue(m.opts.Clock.Now(), previousPending)
	if clearPending {
		ns.flushMinimum = nil
	}
	ext.mu.Unlock()
	m.states.mu.Unlock()
	return nil
}

func pendingGrantCandidatesLocked[T core.LogicalTimeScalar](ext *nerStoreOf[T], fed core.FederationName, cands []candidateGrantOf[T]) []candidateGrantOf[T] {
	for k, ns := range ext.states {
		if k.fed != fed {
			continue
		}
		if !ns.pendingNER {
			continue
		}
		c := candidateGrantOf[T]{
			h:       k.h,
			mode:    ns.mode,
			current: ns.currentTime,
			req:     ns.requestedTime,
		}
		c.nextTSO, c.hasTSO = nextGrantMessage(ns)
		cands = append(cands, c)
	}
	return cands
}

func (m *ManagerOf[T]) decideCandidateGrant(ext *nerStoreOf[T], fed core.FederationName, c candidateGrantOf[T], lbts core.TimeBoundOf[T], solePending bool) GrantDecisionOf[T] {
	// A pending NMR peer can grant early and lower its send floor.
	// Do not let handle order grant this receiver past that floor.
	candidateBound := lbts
	m.states.mu.Lock()
	ext.mu.Lock()
	if state := m.states.getLocked(fed, c.h); state != nil && state.constrained {
		if bound := m.availableHorizonLocked(ext, fed, c.h); bound != nil {
			candidateBound = minimumHorizon(candidateBound, *bound)
		}
	} else {
		// GALT does not bound an unconstrained federate's advance.
		// Any retained incoming messages are received in RO, so they
		// also cannot select an earlier NMR target.
		candidateBound = unboundedHorizon[T]()
		c.hasTSO = false
	}
	ext.mu.Unlock()
	m.states.mu.Unlock()
	// Strategy hook: default GrantStrategy delegates to the
	// package decideGrant function so behavior is unchanged.
	// New(opts) installs defaultGrant{} when
	// Options.GrantStrategy is nil.
	return m.opts.GrantStrategy.DecideGrant(GrantContextOf[T]{
		Mode:        c.mode,
		CurrentTime: c.current,
		Requested:   c.req, LBTS: horizonScalar(candidateBound), LBTSUnbounded: candidateBound.Unbounded, SolePending: solePending,
		NextTSOTime: c.nextTSO,
		HasTSO:      c.hasTSO,
	})
}

// Progress includes a delivered next-message group that requires reevaluation;
// granted distinguishes it from a published grant for time-state observers.
func (m *ManagerOf[T]) tryCandidateGrant(ctx context.Context, fed core.FederationName, c candidateGrantOf[T], d GrantDecisionOf[T]) (progress, granted bool, err error) {
	ext := extOfOf[T](m)
	if !d.Fire {
		if err := m.drainBufferedRO(ctx, fed, c.h); err != nil {
			return false, false, err
		}
		return false, false, nil
	}
	ext.mu.Lock()
	pastSave := m.holdGrantForScheduledSaveLocked(ext, fed, c.h, d.Time)
	ext.mu.Unlock()
	if pastSave {
		if err := m.drainBufferedRO(ctx, fed, c.h); err != nil {
			return false, false, err
		}
		return false, false, nil
	}
	if c.mode.usesNextMessageTarget() {
		reevaluate, err := m.deliverNextMessageGroup(ctx, fed, c.h, d.Time)
		if err != nil {
			return false, false, err
		}
		if reevaluate {
			return true, false, nil
		}
	}
	if err := m.emitGrant(ctx, fed, c.h, d.Time, d.ClearPending); err != nil {
		return false, false, err
	}
	return true, true, nil
}

func nextGrantMessage[T core.LogicalTimeScalar](ns *nerStateOf[T]) (next T, hasTSO bool) {
	if (ns.mode == ModeFQR || ns.mode.usesNextMessageTarget()) && ns.flushMinimum != nil {
		next, hasTSO = *ns.flushMinimum, true
	}
	for _, item := range ns.tsoBuffer {
		if !internalTSO(item.event) && (!hasTSO || item.timestamp < next) {
			next, hasTSO = item.timestamp, true
		}
	}
	return next, hasTSO
}

func mergeChangedFederates(changed, enabled []core.FederateHandle) []core.FederateHandle {
	for _, h := range enabled {
		if !slices.Contains(changed, h) {
			changed = append(changed, h)
		}
	}
	return changed
}
