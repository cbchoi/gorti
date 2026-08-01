package time

import (
	"cmp"
	"context"
	"slices"
	"sync"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
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
type nerState struct {
	currentTime   core.LogicalTime
	pendingNER    bool
	requestedTime core.LogicalTime
	mode          AdvanceMode
	pendingSince  stdtime.Time

	// asyncDelivery: M22 — whether TSO messages with timestamp t are
	// delivered immediately (true; gorti's pre-M22 behavior) or
	// buffered server-side until federate's currentTime >= t (false;
	// the IEEE 1516.1 §8.17 default). Toggled via Manager.Enable/
	// DisableAsynchronousDelivery.
	asyncDelivery bool

	// tsoBuffer: M22 — TSO events held back when asyncDelivery=false
	// and event.timestamp > currentTime. Drained on advance grant
	// (emitGrant calls releaseBufferedTSO) or on toggle to async on
	// (Manager.EnableAsynchronousDelivery).
	tsoBuffer []bufferedTSOEvent
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
type nerStore struct {
	mu        sync.Mutex
	states    map[federateKey]*nerState
	haltedMu  sync.Mutex
	halted    map[core.FederationName]struct{}
	evalMu    sync.Mutex
	evalLocks map[core.FederationName]*sync.Mutex
}

func newNERStore() *nerStore {
	return &nerStore{
		states:    make(map[federateKey]*nerState),
		halted:    make(map[core.FederationName]struct{}),
		evalLocks: make(map[core.FederationName]*sync.Mutex),
	}
}

// evaluatorLock returns the mutex serialising grant evaluation for fed.
// The map mutex is held only for lookup/allocation; callers hold the returned
// federation mutex across the complete tryGrantPending fixed-point loop.
func (n *nerStore) evaluatorLock(fed core.FederationName) *sync.Mutex {
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
func (n *nerStore) getOrCreateLocked(fed core.FederationName, h core.FederateHandle) *nerState {
	k := federateKey{fed: fed, h: h}
	st, ok := n.states[k]
	if !ok {
		st = &nerState{}
		n.states[k] = st
	}
	return st
}

// getLocked returns the nerState for (fed, h), or nil if none. Caller
// MUST hold n.mu.
func (n *nerStore) getLocked(fed core.FederationName, h core.FederateHandle) *nerState {
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
var managerExt sync.Map // map[*Manager]*nerStore

// extOf returns the *nerStore associated with m, lazily creating one
// on first access. Goroutine-safe: concurrent callers race on
// LoadOrStore and the loser's freshly-allocated store is discarded.
func extOf(m *Manager) *nerStore {
	if v, ok := managerExt.Load(m); ok {
		return v.(*nerStore)
	}
	fresh := newNERStore()
	actual, _ := managerExt.LoadOrStore(m, fresh)
	return actual.(*nerStore)
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
func (m *Manager) regulatingSnapshot(fed core.FederationName) []RegulatingFederate {
	m.states.mu.Lock()
	// Copy only the fields needed after releasing the regulation-state lock.
	var out []RegulatingFederate
	for k, st := range m.states.states {
		if k.fed != fed {
			continue
		}
		if !st.regulating {
			continue
		}
		out = append(out, RegulatingFederate{Handle: k.h, Lookahead: st.lookahead})
	}
	m.states.mu.Unlock()

	// Sort the immutable snapshot for deterministic iteration.
	slices.SortFunc(out, func(a, b RegulatingFederate) int {
		return cmp.Compare(a.Handle, b.Handle)
	})

	// Each regulating federate's NER state (currentTime + pendingNER +
	// requestedTime) lives in nerStore. A federate that has never
	// NER'd has currentTime == 0 (zero value).
	ext := extOf(m)
	ext.mu.Lock()
	for i := range out {
		entry := &out[i]
		var ct core.LogicalTime
		var pending bool
		var requested core.LogicalTime
		if ns := ext.getLocked(fed, entry.Handle); ns != nil {
			ct = ns.currentTime
			pending = ns.pendingNER
			requested = ns.requestedTime
		}
		// Pending federates promote their floor to requestedTime —
		// see HLA NER contribution rule in the docstring above.
		floor := ct
		if pending && float64(requested) > float64(ct) {
			floor = requested
		}
		entry.Time = floor
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
//  2. Eligibility check: federate must be regulating OR constrained.
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
func (m *Manager) nextMessageRequest(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime) error {
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
func (m *Manager) tryGrantPending(ctx context.Context, fed core.FederationName) error {
	ext := extOf(m)
	evalLock := ext.evaluatorLock(fed)
	evalLock.Lock()
	defer evalLock.Unlock()

	for {
		snap := m.regulatingSnapshot(fed)
		// Strategy hook: default LBTSStrategy delegates to the package
		// LBTS function so behavior is unchanged. New(opts) installs
		// defaultLBTS{} when Options.LBTSStrategy is nil.
		lbts := m.opts.LBTSStrategy.LBTS(snap)

		// Materialise candidates under ext.mu, then release before any
		// I/O. D-2: never iterate a map without sorting downstream.
		ext.mu.Lock()
		var candidateStorage [4]candidateGrant
		cands := candidateStorage[:0]
		for k, ns := range ext.states {
			if k.fed != fed {
				continue
			}
			if !ns.pendingNER {
				continue
			}
			c := candidateGrant{
				h:       k.h,
				mode:    ns.mode,
				current: ns.currentTime,
				req:     ns.requestedTime,
			}
			// M38 GA — §8.8/§8.9 next-message input: earliest buffered
			// TSO timestamp for this federate (buffer is FIFO by
			// arrival, not sorted by timestamp — scan for the min).
			for _, b := range ns.tsoBuffer {
				if !c.hasTSO || float64(b.timestamp) < float64(c.nextTSO) {
					c.nextTSO = b.timestamp
					c.hasTSO = true
				}
			}
			cands = append(cands, c)
		}
		ext.mu.Unlock()

		if len(cands) == 0 {
			return nil
		}
		sortCandidates(cands)
		solePending := len(cands) == 1

		// First pass: collect every candidate that decideGrant says to
		// fire under the current LBTS. Emit them in handle order, then
		// restart the outer loop so a just-granted federate's
		// currentTime advance can unblock further peers.
		fired := false
		for _, c := range cands {
			// Strategy hook: default GrantStrategy delegates to the
			// package decideGrant function so behavior is unchanged.
			// New(opts) installs defaultGrant{} when
			// Options.GrantStrategy is nil.
			d := m.opts.GrantStrategy.DecideGrant(GrantContext{
				Mode:        c.mode,
				CurrentTime: c.current,
				Requested:   c.req,
				LBTS:        lbts,
				SolePending: solePending,
				NextTSOTime: c.nextTSO,
				HasTSO:      c.hasTSO,
			})
			if !d.Fire {
				continue
			}
			if err := m.emitGrant(ctx, fed, c.h, d.Time, d.ClearPending); err != nil {
				return err
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

// emitGrant writes the TimeAdvanceGranted record to the event log
// (write-ahead), releases buffered TSO events at-or-before t, sends
// TimeAdvanceGrant through the outbox, then advances the federate's
// currentTime. When clearPending is true the pendingNER flag is also
// cleared (full grant). When false the flag stays — used for forced
// grants where the requested time was not reached and the federate is
// still waiting on peers.
//
// M37 EB-2 — §8.14 delivery order: the grant is the RTI's guarantee
// that the federate ALREADY holds every TSO message with timestamp
// <= t; buffered TSO must therefore drain BEFORE the grant reaches the
// federate's stream. Every advance primitive (NER/NMRA/TAR/TARA/FQR),
// forced grants, and the M36 membership-event grants funnel through
// this single emission point (tryGrantPending → emitGrant), so the
// ordering is corrected here once. Pre-M37 the grant was sent first —
// a federate acting on the grant immediately could miss buffered TSO
// at-or-before the grant time.
//
// Determinism: EventLog.Append happens-before Outbox.Send happens-
// before state mutation. Replay reads from the log; see SRS NFR-DET-1.
func (m *Manager) emitGrant(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime, clearPending bool) error {
	// Production outboxes reserve the complete TSO-before-grant sequence before
	// the write-ahead append. Admission failure restores the extracted TSO
	// events and leaves currentTime/pending state untouched.
	release := m.takeBufferedTSO(fed, h, &t)
	grant := &TimeAdvanceGrant{Time: t}
	var reservation core.OutboxReservation
	if reservable, ok := m.opts.Outbox.(core.ReservableOutbox); ok {
		deliveries := make([]core.OutboxDelivery, 0, len(release)+1)
		for _, buffered := range release {
			deliveries = append(deliveries, core.OutboxDelivery{Recipient: h, Event: buffered.event})
		}
		deliveries = append(deliveries, core.OutboxDelivery{Recipient: h, Event: grant})
		var err error
		reservation, err = reservable.Reserve(ctx, fed, deliveries)
		if err != nil {
			m.restoreBufferedTSO(fed, h, release)
			return err
		}
	}

	// (a) Write-ahead to EventLog (cut-1 relaxation: nil silently drops).
	if m.opts.EventLog != nil {
		rec := &timeAdvanceGrantedRecord{Federate: h, Time: t}
		if err := m.opts.EventLog.Append(ctx, fed, rec); err != nil {
			if reservation != nil {
				reservation.Release()
			}
			m.restoreBufferedTSO(fed, h, release)
			return err
		}
	}

	// (b/c) Transfer every eligible TSO callback followed by the grant. The
	// time state must be committed before the grant becomes visible: otherwise a
	// federate can receive TAG and immediately issue its next TAR while the old
	// pending flag is still set. Hold ext.mu across the visibility boundary so
	// that the next request observes the completed transition.
	ext := extOf(m)
	if reservation != nil {
		ext.mu.Lock()
		ns := ext.getOrCreateLocked(fed, h)
		previousTime := ns.currentTime
		previousPending := ns.pendingNER
		previousRequested := ns.requestedTime
		previousMode := ns.mode
		previousSince := ns.pendingSince
		ns.currentTime = t
		if clearPending {
			ns.pendingNER = false
			ns.requestedTime = 0
			ns.mode = ModeNone
			ns.pendingSince = stdtime.Time{}
		}
		if err := reservation.Commit(); err != nil {
			ns.currentTime = previousTime
			ns.pendingNER = previousPending
			ns.requestedTime = previousRequested
			ns.mode = previousMode
			ns.pendingSince = previousSince
			ext.mu.Unlock()
			return err
		}
		ext.mu.Unlock()
	} else {
		for i, buffered := range release {
			if err := m.opts.Outbox.Send(ctx, fed, h, buffered.event); err != nil {
				m.restoreBufferedTSO(fed, h, release[i:])
				return err
			}
		}
		ext.mu.Lock()
		ns := ext.getOrCreateLocked(fed, h)
		previousTime := ns.currentTime
		previousPending := ns.pendingNER
		previousRequested := ns.requestedTime
		previousMode := ns.mode
		previousSince := ns.pendingSince
		ns.currentTime = t
		if clearPending {
			ns.pendingNER = false
			ns.requestedTime = 0
			ns.mode = ModeNone
			ns.pendingSince = stdtime.Time{}
		}
		if err := m.opts.Outbox.Send(ctx, fed, h, grant); err != nil {
			ns.currentTime = previousTime
			ns.pendingNER = previousPending
			ns.requestedTime = previousRequested
			ns.mode = previousMode
			ns.pendingSince = previousSince
			ext.mu.Unlock()
			return err
		}
		ext.mu.Unlock()
	}
	return nil
}

// isHalted reports whether the federation has entered the halted
// terminal state. W3 (stall detection) will populate the halted set;
// W2 only reads it. Returns false for any federation when the halted
// set is empty (the common live case).
func (n *nerStore) isHalted(fed core.FederationName) bool {
	n.haltedMu.Lock()
	defer n.haltedMu.Unlock()
	_, ok := n.halted[fed]
	return ok
}
