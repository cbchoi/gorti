package time

import (
	"context"
	"errors"
	"sort"
	"sync"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrDuplicateNER is returned by NextMessageRequest when the federate
// already has an outstanding NER for which no grant has been emitted.
// The acceptance contract (rti/spec/M3/ner_test.go
// TestSpec_M3_NER_DuplicateRequestRejected) accepts ANY non-nil error
// here; this dedicated sentinel makes the failure mode explicit so
// federate-side code can branch on it.
var ErrDuplicateNER = errors.New("time: federate has an outstanding NER request")

// nerState is the per-federate NER bookkeeping side-table. It lives
// alongside (and not inside) federateState because federateState is
// owned by Wave 1A's regulation.go and the W2 ownership rules forbid
// modifying that file.
//
// Fields:
//   - currentTime: the federate's last granted (or initial) logical
//     time. Initialised to 0 on first interaction.
//   - pendingNER: true while a NER request is outstanding (queued but
//     not yet granted). Cleared when the grant is emitted.
//   - requestedTime: the t parameter of the outstanding NER. Meaningful
//     only when pendingNER is true.
//   - pendingSince: wall-clock time (via Manager.opts.Clock) at which
//     pendingNER transitioned to true. Used by W3 stall detection
//     (CheckStalls) to compare against the federation's StallTimeout.
//     Meaningful only when pendingNER is true; reset on full grant.
type nerState struct {
	currentTime   core.LogicalTime
	pendingNER    bool
	requestedTime core.LogicalTime
	pendingSince  stdtime.Time
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
	mu       sync.Mutex
	states   map[federateKey]*nerState
	haltedMu sync.Mutex
	halted   map[core.FederationName]struct{}
}

func newNERStore() *nerStore {
	return &nerStore{
		states: make(map[federateKey]*nerState),
		halted: make(map[core.FederationName]struct{}),
	}
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
// orchestrator-frozen public types untouched.
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
	// Collect (k, *federateState) for fed's regulating federates only.
	var keys []federateKey
	for k, st := range m.states.states {
		if k.fed != fed {
			continue
		}
		if !st.regulating {
			continue
		}
		keys = append(keys, k)
	}
	// Snapshot regulation fields under the lock.
	regSnap := make(map[federateKey]federateState, len(keys))
	for _, k := range keys {
		regSnap[k] = *m.states.states[k]
	}
	m.states.mu.Unlock()

	// Sort keys for deterministic iteration.
	sort.Slice(keys, func(i, j int) bool { return keys[i].h < keys[j].h })

	// Each regulating federate's NER state (currentTime + pendingNER +
	// requestedTime) lives in nerStore. A federate that has never
	// NER'd has currentTime == 0 (zero value).
	ext := extOf(m)
	out := make([]RegulatingFederate, 0, len(keys))
	ext.mu.Lock()
	for _, k := range keys {
		var ct core.LogicalTime
		var pending bool
		var requested core.LogicalTime
		if ns := ext.getLocked(k.fed, k.h); ns != nil {
			ct = ns.currentTime
			pending = ns.pendingNER
			requested = ns.requestedTime
		}
		reg := regSnap[k]
		// Pending federates promote their floor to requestedTime —
		// see HLA NER contribution rule in the docstring above.
		floor := ct
		if pending && float64(requested) > float64(ct) {
			floor = requested
		}
		out = append(out, RegulatingFederate{
			Handle:    k.h,
			Time:      floor,
			Lookahead: reg.lookahead,
		})
	}
	ext.mu.Unlock()
	return out
}

// nextMessageRequest is the body of Manager.NextMessageRequest.
// Extracted so the exported method's body remains a one-liner and the
// orchestrator-frozen signature in manager.go is preserved verbatim.
//
// Pre-flight ordering (matters for the ner_test.go assertions):
//  1. Halted-federation check (rejects with ErrFederationHalted).
//  2. Eligibility check: federate must be regulating OR constrained.
//  3. Lookahead check (regulating federates only).
//  4. Duplicate-request check.
//
// Grant logic:
//   - Record the request, mark pendingNER.
//   - Compute LBTS over the regulating set.
//   - For every (handle-sorted) federate whose pending request is
//     satisfied by LBTS, emit TimeAdvanceGrant via Outbox in handle
//     order — ascending — to honour the determinism contract
//     (NFR-DET-1; TestSpec_M3_NER_SimultaneousReady_DeterministicGrantOrder).
//   - Write-ahead through EventLog when non-nil.
func (m *Manager) nextMessageRequest(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime) error {
	ext := extOf(m)

	// (1) Halted check. The halted set is owned by W3 (stall detection);
	// W2 reads it through ext.halted, which W3 will populate. Until
	// then this check is a clean no-op for live federations.
	if ext.isHalted(fed) {
		return core.ErrFederationHalted
	}

	// (2) Eligibility: the federate must be in time management. Read
	// regulation/constrained state under stateStore.mu and copy out.
	m.states.mu.Lock()
	regSt := m.states.getLocked(fed, h)
	var regulating, constrained bool
	var lookahead core.LogicalTime
	if regSt != nil {
		regulating = regSt.regulating
		constrained = regSt.constrained
		lookahead = regSt.lookahead
	}
	m.states.mu.Unlock()
	if !regulating && !constrained {
		return core.ErrTimeNotRegulating
	}

	// (3) Lookahead: only regulating federates have a lookahead floor.
	// For a constrained-only federate the request is bounded by LBTS,
	// not by lookahead.
	ext.mu.Lock()
	ns := ext.getOrCreateLocked(fed, h)
	currentTime := ns.currentTime
	if regulating {
		if err := checkLookahead(currentTime, lookahead, t); err != nil {
			ext.mu.Unlock()
			return err
		}
	}

	// (4) Duplicate-request check.
	if ns.pendingNER {
		ext.mu.Unlock()
		return ErrDuplicateNER
	}

	// Record the new request. pendingSince captures the wall time at
	// which the request first became outstanding so W3's CheckStalls
	// can age it against the federation's StallTimeout. The clock is
	// read INSIDE the critical section to keep the wall-time decision
	// happens-before any peer's CheckStalls observation of the flag.
	ns.pendingNER = true
	ns.requestedTime = t
	ns.pendingSince = m.opts.Clock.Now()
	ext.mu.Unlock()

	// Compute LBTS and emit grants for every now-satisfied pending NER
	// in this federation, in handle-sorted order.
	return m.tryGrantPending(ctx, fed)
}

// tryGrantPending evaluates every pending NER in fed against the
// current LBTS and emits grants — in ascending FederateHandle order —
// for each one whose requested time is reachable. This loop runs to
// fixed point: granting one federate's NER advances its currentTime,
// which may raise LBTS and unblock another NER.
//
// Two grant paths:
//
//   (a) FULL grant: emitted when LBTS > F.requestedTime (strict). The
//       federate is advanced to its requested time and pendingNER is
//       cleared. The strict inequality (rather than >=) is what lets
//       SimultaneousReady defer the first NER until all peers also
//       NER and collectively raise LBTS above the requested floor.
//
//   (b) FORCED grant: emitted when EXACTLY ONE federate F is pending
//       in fed AND LBTS < F.requestedTime — i.e. peers are not in a
//       state to ever satisfy F without themselves NER'ing. The
//       forced grant lands at LBTS (not requestedTime); pendingNER
//       remains set so that (i) a duplicate NER is rejected and
//       (ii) the grant completes when peers eventually advance.
//       This is the TwoRegulators_GrantWaits behaviour.
//
// Returns the first error encountered while emitting through Outbox
// or EventLog; partial progress is preserved (a federate granted
// before the failure has its state advanced). In cut-1 the
// fakeOutbox + permissiveEventLog never fail, so the nominal path
// returns nil.
func (m *Manager) tryGrantPending(ctx context.Context, fed core.FederationName) error {
	ext := extOf(m)
	for {
		snap := m.regulatingSnapshot(fed)
		lbts := LBTS(snap)

		// Collect pending federates in fed whose requestedTime is
		// strictly below LBTS — the FULL-grant set. Iteration is over
		// a Go map so we materialise the candidate keys first and sort
		// (D-2: never iterate a map without sorting downstream output).
		ext.mu.Lock()
		type cand struct {
			h core.FederateHandle
			t core.LogicalTime
		}
		var fullGrants []cand
		var pendingCount int
		var solePending cand
		for k, ns := range ext.states {
			if k.fed != fed {
				continue
			}
			if !ns.pendingNER {
				continue
			}
			pendingCount++
			solePending = cand{h: k.h, t: ns.requestedTime}
			if float64(lbts) > float64(ns.requestedTime) {
				fullGrants = append(fullGrants, cand{h: k.h, t: ns.requestedTime})
			}
		}
		ext.mu.Unlock()

		// (a) FULL grants: emit for every fed whose requested < LBTS
		// in handle order; restart the outer loop so a just-granted
		// federate's currentTime advance can unblock further peers.
		if len(fullGrants) > 0 {
			sort.Slice(fullGrants, func(i, j int) bool { return fullGrants[i].h < fullGrants[j].h })
			for _, c := range fullGrants {
				if err := m.emitGrant(ctx, fed, c.h, c.t, true /*clearPending*/); err != nil {
					return err
				}
			}
			continue
		}

		// (b) FORCED grant: only when the federation has exactly one
		// pending NER AND LBTS is below its requested. The forced
		// grant lands at LBTS; the federate stays pending so the
		// duplicate-NER check still fires.
		if pendingCount == 1 && float64(lbts) < float64(solePending.t) {
			if err := m.emitGrant(ctx, fed, solePending.h, lbts, false /*keepPending*/); err != nil {
				return err
			}
		}
		return nil
	}
}

// emitGrant writes the TimeAdvanceGranted record to the event log
// (write-ahead), sends TimeAdvanceGrant through the outbox, then
// advances the federate's currentTime. When clearPending is true the
// pendingNER flag is also cleared (full grant). When false the flag
// stays — used for forced grants where the requested time was not
// reached and the federate is still waiting on peers.
//
// Determinism: EventLog.Append happens-before Outbox.Send happens-
// before state mutation. Replay reads from the log; see SRS NFR-DET-1.
func (m *Manager) emitGrant(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime, clearPending bool) error {
	// (a) Write-ahead to EventLog (cut-1 relaxation: nil silently drops).
	if m.opts.EventLog != nil {
		rec := &timeAdvanceGrantedRecord{Federate: h, Time: t}
		if err := m.opts.EventLog.Append(ctx, fed, rec); err != nil {
			return err
		}
	}

	// (b) Send the grant on the federate's outbound stream.
	grant := &TimeAdvanceGrant{Time: t}
	if err := m.opts.Outbox.Send(ctx, fed, h, grant); err != nil {
		return err
	}

	// (c) State mutation: advance currentTime; optionally clear pending.
	ext := extOf(m)
	ext.mu.Lock()
	ns := ext.getOrCreateLocked(fed, h)
	ns.currentTime = t
	if clearPending {
		ns.pendingNER = false
		ns.requestedTime = 0
		ns.pendingSince = stdtime.Time{}
	}
	ext.mu.Unlock()
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
