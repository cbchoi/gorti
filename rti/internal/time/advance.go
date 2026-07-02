package time

import (
	"context"
	"sort"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// AdvanceMode tags an outstanding time-advance request with the IEEE
// 1516.1-2010 §8 primitive that produced it. tryGrantPending dispatches
// per-mode grant conditions:
//
//   - ModeNER  (§8.8 NextMessageRequest)          — grant lands at
//     min(t, next queued TSO time) and requires LBTS strictly above the
//     grant time; otherwise the request stays pending (M38 GA — no
//     interim grant; the pre-M38 sole-pending forced grant at LBTS is
//     retired).
//   - ModeNMRA (§8.9 NextMessageRequestAvailable) — same grant target,
//     inclusive boundary (LBTS >= grant time).
//   - ModeTAR  (§8.10 TimeAdvanceRequest)         — grant fires at min(t, LBTS) whenever
//     it produces forward progress (LBTS != t case).
//     No "sole-pending" gate: incremental grants are
//     the standard TAR behaviour.
//   - ModeTARA (§8.11 TimeAdvanceRequestAvailable) — like TAR but allows LBTS == t (grant at t).
//   - ModeFQR  (§8.13 FlushQueueRequest)          — cut-1 simplification: behaves like TAR
//     (drain-queue semantics deferred to cut-3 when
//     the TSO queue ships). Always grants when LBTS
//     produces progress.
//
// The semantic divergence between the strict / inclusive and the NER /
// TAR families is captured in two predicates evaluated against LBTS,
// requestedTime, and the federate's currentTime; see decideGrant below.
type AdvanceMode int

const (
	// ModeNone is the zero value; an entry with ModeNone has no
	// outstanding request. Used as the "cleared" state after a grant
	// fires (alongside pendingNER=false).
	ModeNone AdvanceMode = iota
	// ModeNER tags requests originating in NextMessageRequest. M3 W2 default.
	ModeNER
	// ModeNMRA tags requests from NextMessageRequestAvailable.
	ModeNMRA
	// ModeTAR tags requests from TimeAdvanceRequest.
	ModeTAR
	// ModeTARA tags requests from TimeAdvanceRequestAvailable.
	ModeTARA
	// ModeFQR tags requests from FlushQueueRequest.
	ModeFQR
)

// String renders the mode for debug logging and the stall-trigger
// payload. Stable across versions: replay parsers (M4) must accept
// these literals.
func (m AdvanceMode) String() string {
	switch m {
	case ModeNER:
		return "NER"
	case ModeNMRA:
		return "NMRA"
	case ModeTAR:
		return "TAR"
	case ModeTARA:
		return "TARA"
	case ModeFQR:
		return "FQR"
	default:
		return "none"
	}
}

// usesNextMessageTarget reports whether the mode's grant target is
// min(requestedTime, earliest queued TSO timestamp for the requester) —
// the IEEE 1516.1-2010 §8.8/§8.9 next-message semantics. Only NER and
// NMRA: their contract is "advance me to the next relevant message, or
// to t if none arrives first". TAR / TARA / FQR grants target the
// requested time itself; queued TSO at-or-before the grant time drains
// on emission (§8.14, emitGrant → releaseBufferedTSO).
//
// M38 GA: this replaces the pre-M38 allowsForcedGrant "sole-pending →
// grant at LBTS, KEEP pending" escape hatch. §8.8 defines no interim
// grant: a next-message request either completes (at the message time
// or the requested time) or stays pending until a TSO arrival
// (asyncdelivery.go BufferTSO poke) or an LBTS raise (peer advance,
// lookahead change, membership change) re-evaluates it.
func (m AdvanceMode) usesNextMessageTarget() bool {
	return m == ModeNER || m == ModeNMRA
}

// allowsIncrementalGrant reports whether the mode emits an immediate
// grant at LBTS even when LBTS has not reached the requested time.
//
// M37 EB-5: FQR only. IEEE 1516.1-2010 §8.12 defines flushQueueRequest
// as the early-grant primitive (grant may land below the requested
// time after flushing the queue). TAR (§8.10) and TARA (§8.11) grants
// are to EXACTLY the requested time — the RTI holds the request until
// LBTS covers it, delivering intervening TSO messages meanwhile. The
// pre-M37 "TAR family incremental grant at LBTS" burned a TAR(t) with
// a full grant at LBTS < t (clearPending=true), so a constrained
// federate waiting on a far-future TAR was silently parked below its
// requested time and buffered TSO at higher timestamps never released
// (om_delete_object_tso 15/16).
//
// NER / NMRA never do: they wait for LBTS to satisfy the
// strict-or-inclusive comparison against the §8.8/§8.9 grant target
// (see usesNextMessageTarget).
func (m AdvanceMode) allowsIncrementalGrant() bool {
	return m == ModeFQR
}

// inclusiveLBTS reports whether the mode's full-grant predicate is
// `LBTS >= requestedTime` (true) versus `LBTS > requestedTime` (false).
// The "Available" variants and FQR (cut-1 simplification) use inclusive;
// NER uses exclusive.
//
// M37 EB-5 — TAR moved from exclusive to inclusive when its
// incremental-grant-at-LBTS path was removed (see
// allowsIncrementalGrant): §8.10 grants must land at EXACTLY the
// requested time, and the inclusive boundary preserves the
// zero-lookahead peer lockstep (two la=0 federates both TAR(t) →
// LBTS == t → both grant) that the incremental path used to service.
// Cut-3 simplification: this collapses the TAR/TARA grant-boundary
// distinction; distinguishing them properly needs open/closed LBTS
// bounds (a message at exactly LBTS from a nonzero-lookahead pending
// peer remains possible), tracked as a follow-up alongside the
// zero-lookahead strictly-greater send rule.
func (m AdvanceMode) inclusiveLBTS() bool {
	return m == ModeNMRA || m == ModeTAR || m == ModeTARA || m == ModeFQR
}

// grantDecision is the outcome of evaluating one pending request against
// the current LBTS. It bundles the grant time and the post-grant
// disposition (clear pending vs keep pending) so tryGrantPending can
// emit grants in a single pass without re-deriving semantics inline.
type grantDecision struct {
	// fire is true when a grant should be emitted; false means hold.
	fire bool
	// time is the grant time (federate.currentTime advances to this).
	time core.LogicalTime
	// clearPending is true when pendingNER should be cleared after the
	// grant. M38 GA: every fired grant now clears — the field is kept
	// so alternative GrantStrategy implementations (alt_*.go) can still
	// model keep-pending policies.
	clearPending bool
}

// decideGrant evaluates a single pending request against the current
// LBTS. The caller (tryGrantPending) owns the outer fixed-point loop;
// decideGrant is pure (no I/O, no mutex) and so can be unit-tested in
// isolation.
//
// Inputs:
//   - mode         : the request's AdvanceMode (decides predicate family).
//   - currentTime  : federate's last granted (or initial) logical time.
//   - requested    : the t parameter of the outstanding request.
//   - lbts         : current LBTS over the regulating set.
//   - solePending  : true when this is the only pending request in the
//     federation. M38 GA: unused by the default policy (the forced-grant
//     hatch it gated is retired); retained for GrantContext parity so
//     alternative strategies can still consult it.
//   - nextTSO      : earliest buffered TSO timestamp queued for the
//     requester (asyncdelivery.go tsoBuffer). Meaningful iff hasTSO.
//   - hasTSO       : whether any TSO message is queued for the requester.
//
// Returns a grantDecision; when fire is false the request stays pending
// untouched.
func decideGrant(mode AdvanceMode, currentTime, requested, lbts core.LogicalTime, solePending bool, nextTSO core.LogicalTime, hasTSO bool) grantDecision {
	_ = solePending // M38 GA: see docstring — kept for strategy-API parity.
	rt := float64(requested)
	ct := float64(currentTime)
	lb := float64(lbts)

	// §8.8/§8.9 — NER/NMRA grant target: min(requested, next queued TSO
	// time). A queued message at-or-before currentTime is already
	// releasable (it drains with any grant, releaseBufferedTSO) and
	// cannot pull the target below the federate's present time.
	target := rt
	if mode.usesNextMessageTarget() && hasTSO {
		if nt := float64(nextTSO); nt > ct && nt < rt {
			target = nt
		}
	}

	// Full grant predicate against the target: strict for NER/TAR,
	// inclusive for NMRA/TARA/FQR. Strictness guarantees no further TSO
	// message at exactly the grant time can arrive afterwards (§8.8:
	// every message at the grant time is delivered before the grant);
	// the Available variants accept same-time stragglers post-grant
	// (§8.9/§8.11) and so allow the boundary.
	fullGrant := lb > target
	if mode.inclusiveLBTS() {
		fullGrant = lb >= target
	}
	if fullGrant {
		return grantDecision{fire: true, time: core.LogicalTime(target), clearPending: true}
	}

	// FQR: emit incremental grant at LBTS whenever LBTS produces
	// forward progress (lbts > currentTime). Clear pending — "one
	// request → one grant".
	if mode.allowsIncrementalGrant() && lb > ct {
		return grantDecision{fire: true, time: lbts, clearPending: true}
	}

	// M38 GA — everything else HOLDS. §8.8 defines no interim grant for
	// a pending next-message request; re-evaluation happens on TSO
	// arrival (BufferTSO poke), LBTS raises, and resign/membership
	// events.
	return grantDecision{}
}

// dispatchAdvance is the unified entry point for NMRA / TAR / TARA /
// FQR. It mirrors nextMessageRequest's pre-flight discipline and ends
// by calling tryGrantPending exactly the way NER does — the per-mode
// semantic divergence is delegated to decideGrant (consulted inside
// tryGrantPending via ns.Mode).
//
// Pre-flight ordering matches NER (manager.go-frozen behaviour):
//  1. Halted-federation check.
//  2. Eligibility (regulating OR constrained).
//  3. Advance-target enforcement (t >= currentTime, all federates).
//     M36 DB-1: lookahead no longer floors the target — per IEEE
//     1516.1 §8.8 it constrains outgoing TSO timestamps only.
//  4. Duplicate-request check (any outstanding request blocks; the
//     federate cannot mix modes either).
//
// On success, records (mode, requestedTime, pendingSince) and runs the
// shared grant-loop.
func (m *Manager) dispatchAdvance(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime, mode AdvanceMode) error {
	ext := extOf(m)

	// (1) Halted check.
	if ext.isHalted(fed) {
		return core.ErrFederationHalted
	}

	// (2) Eligibility.
	m.states.mu.Lock()
	regSt := m.states.getLocked(fed, h)
	var regulating, constrained bool
	if regSt != nil {
		regulating = regSt.regulating
		constrained = regSt.constrained
	}
	m.states.mu.Unlock()
	if !regulating && !constrained {
		return core.ErrTimeNotRegulating
	}

	// (3) Advance-target check: t >= currentTime for every mode and
	// every requester (M36 DB-1 — IEEE 1516.1 §8.8). Lookahead no
	// longer floors the target; it constrains outgoing TSO timestamps
	// only. See checkAdvanceTarget in lookahead.go.
	ext.mu.Lock()
	ns := ext.getOrCreateLocked(fed, h)
	currentTime := ns.currentTime
	if err := checkAdvanceTarget(currentTime, t); err != nil {
		ext.mu.Unlock()
		return err
	}

	// (4) Duplicate-request check (cross-mode: any outstanding request blocks).
	if ns.pendingNER {
		ext.mu.Unlock()
		return ErrDuplicateNER
	}

	// Record the new request.
	ns.pendingNER = true
	ns.requestedTime = t
	ns.mode = mode
	ns.pendingSince = m.opts.Clock.Now()
	ext.mu.Unlock()

	// Compute LBTS and emit grants for every now-satisfied pending
	// request in this federation, in handle-sorted order.
	return m.tryGrantPending(ctx, fed)
}

// candidateGrants is a side-table extraction used by tryGrantPending to
// keep determinism. We materialise (handle, mode, currentTime,
// requestedTime) under ext.mu, release the lock, then sort by handle
// before evaluating decideGrant for each entry. This guarantees
// per-iteration handle order is deterministic regardless of map
// iteration randomisation (NFR-DET-1).
type candidateGrant struct {
	h       core.FederateHandle
	mode    AdvanceMode
	current core.LogicalTime
	req     core.LogicalTime
	// nextTSO / hasTSO — M38 GA: earliest timestamp among the
	// federate's buffered TSO events at snapshot time, feeding the
	// §8.8/§8.9 next-message grant target in decideGrant.
	nextTSO core.LogicalTime
	hasTSO  bool
}

func sortCandidates(cs []candidateGrant) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].h < cs[j].h })
}
