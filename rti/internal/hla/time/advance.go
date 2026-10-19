package time

import (
	"cmp"
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// AdvanceMode identifies the pending IEEE 1516.1-2010 service:
// TAR (8.8), TARA (8.9), NMR (8.10), NMRA (8.11), or FQR (8.12).
// TAR/TARA grants target exactly the requested time. NMR/NMRA may instead
// target an earlier incoming message; FQR may also grant below the request.
// NER is the retained internal name for NMR. See decideGrant for the
// strict/inclusive bound predicates and emitGrant for queue admission.
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

// usesNextMessageTarget selects the next-message target of 8.10/8.11.
// A request completes at the message time or requested time; it does not
// issue an interim grant while retaining the same pending request.
func (m AdvanceMode) usesNextMessageTarget() bool {
	return m == ModeNER || m == ModeNMRA
}

// inclusiveLBTS reports whether the mode's full-grant predicate is
// `LBTS >= requestedTime` (true) versus `LBTS > requestedTime` (false).
// The Available variants and FQR use the inclusive boundary. NER and
// TAR are strict because a regulating peer whose lower bound equals the
// request may still legally send a timestamp-equal message; granting at
// equality can overtake that message.
func (m AdvanceMode) inclusiveLBTS() bool {
	return m == ModeNMRA || m == ModeTARA || m == ModeFQR
}

// grantDecision is the outcome of evaluating one pending request against
// the current LBTS. It bundles the grant time and the post-grant
// disposition (clear pending vs keep pending) so tryGrantPending can
// emit grants in a single pass without re-deriving semantics inline.
type grantDecisionOf[T core.LogicalTimeScalar] struct {
	// fire is true when a grant should be emitted; false means hold.
	fire bool
	// time is the grant time (federate.currentTime advances to this).
	time T
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
func decideGrantOf[T core.LogicalTimeScalar](mode AdvanceMode, currentTime, requested, lbts T, solePending bool, nextTSO T, hasTSO bool) grantDecisionOf[T] {
	_ = solePending // M38 GA: see docstring — kept for strategy-API parity.
	// Preserve historical internal infinities at this Float64 boundary. Typed
	// callers distinguish an unbounded horizon from a finite final time.
	d := decideScalarGrant(mode, currentTime, requested, lbts, false, nextTSO, hasTSO)
	return grantDecisionOf[T]{fire: d.fire, time: d.time, clearPending: d.clearPending}
}

// dispatchAdvance is the unified entry point for NMRA / TAR / TARA /
// FQR. It mirrors nextMessageRequest's pre-flight discipline and ends
// by calling tryGrantPending exactly the way NER does — the per-mode
// semantic divergence is delegated to decideGrant (consulted inside
// tryGrantPending via ns.Mode).
//
// Pre-flight ordering matches NER (manager.go-frozen behaviour):
//  1. Halted-federation check.
//  2. Membership is checked at the application/transport boundary; neither
//     regulation nor constraint is required for a joined federate to advance.
//  3. Advance-target enforcement (t >= currentTime, all federates).
//     M36 DB-1: lookahead no longer floors the target — per IEEE
//     1516.1 §8.8 it constrains outgoing TSO timestamps only.
//  4. Duplicate-request check (any outstanding request blocks; the
//     federate cannot mix modes either).
//
// On success, records (mode, requestedTime, pendingSince) and runs the
// shared grant-loop.
func (m *ManagerOf[T]) dispatchAdvance(ctx context.Context, fed core.FederationName, h core.FederateHandle, t T, mode AdvanceMode) error {
	if err := m.recordAdvance(fed, h, t, mode); err != nil {
		return err
	}
	return m.tryGrantPending(ctx, fed)
}

func (m *ManagerOf[T]) recordAdvance(fed core.FederationName, h core.FederateHandle, t T, mode AdvanceMode) error {
	ext := extOfOf[T](m)
	lease := ext.evaluatorLock(fed)
	lease.Lock()
	defer lease.Unlock()

	// (1) Halted check.
	if ext.isHalted(fed) {
		return core.ErrFederationHalted
	}
	if err := m.pendingEnableError(fed, h); err != nil {
		return err
	}

	// (3) Advance-target check: t >= currentTime for every mode and
	// every requester (M36 DB-1 — IEEE 1516.1 §8.8). Lookahead no
	// longer floors the target; it constrains outgoing TSO timestamps
	// only. See checkAdvanceTarget in lookahead.go.
	ext.mu.Lock()
	ns := ext.getLocked(fed, h)
	currentTime := T(0)
	if ns != nil {
		currentTime = ns.currentTime
	}
	if err := checkAdvanceTargetOf[T](currentTime, t); err != nil {
		ext.mu.Unlock()
		return err
	}
	// Rejected first requests must not create per-federate state.
	ns = ext.getOrCreateLocked(fed, h)

	// (4) Duplicate-request check (cross-mode: any outstanding request blocks).
	if ns.pendingNER {
		ext.mu.Unlock()
		return ErrDuplicateNER
	}

	// Record the new request.
	now := m.opts.Clock.Now()
	ns.elapsed.accrue(now, ns.pendingNER)
	ns.pendingNER = true
	ns.requestedTime = t
	ns.mode = mode
	ns.flushMinimum = nil
	ns.pendingSince = now
	ext.mu.Unlock()

	return nil
}

// candidateGrants is a side-table extraction used by tryGrantPending to
// keep determinism. We materialise (handle, mode, currentTime,
// requestedTime) under ext.mu, release the lock, then sort by handle
// before evaluating decideGrant for each entry. This guarantees
// per-iteration handle order is deterministic regardless of map
// iteration randomisation (NFR-DET-1).
type candidateGrantOf[T core.LogicalTimeScalar] struct {
	h       core.FederateHandle
	mode    AdvanceMode
	current T
	req     T
	// nextTSO / hasTSO — M38 GA: earliest timestamp among the
	// federate's buffered TSO events at snapshot time, feeding the
	// §8.8/§8.9 next-message grant target in decideGrant.
	nextTSO T
	hasTSO  bool
}

func sortCandidatesOf[T core.LogicalTimeScalar](cs []candidateGrantOf[T]) {
	slices.SortFunc(cs, func(a, b candidateGrantOf[T]) int { return cmp.Compare(a.h, b.h) })
}
