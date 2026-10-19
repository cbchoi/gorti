package time

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// LBTSStrategy is the per-service algorithm hook for LBTS computation.
// Researchers swap in alternative LBTS algorithms (conservative-min
// variants, lookahead policies, hierarchical LBTS, …) by providing a
// type that implements this interface and wiring it through
// Options.LBTSStrategy.
//
// The default implementation (defaultLBTS) wraps the package-level
// LBTS([]RegulatingFederate) function and is selected when
// Options.LBTSStrategy is nil. The signature of LBTS(...) here is
// IDENTICAL to the package-level function so the swap is mechanical —
// the production code path calls strategy.LBTS(snap) instead of
// LBTS(snap) without touching arguments or return type.
//
// DeterminismPreserving() reports whether the implementation maintains
// the NFR-DET-1 contract (same inputs → same outputs, regardless of
// slice ordering). The default returns true because min over IEEE-754
// doubles is associative and commutative for non-NaN values; alternate
// strategies that introduce randomness or floating-point reduction
// trees that depend on input order MUST return false.
//
// Name() is the registry key used in Phase 3's TOML config (the
// `[time].lbts = "..."` setting). The default impl returns "default".
//
// See docs/research-platform.md §6.1 for the design context.
type LBTSStrategyOf[T core.LogicalTimeScalar] interface {
	// LBTS computes the lower bound on time stamps over the regulating
	// set. Signature MUST match the package-level LBTS function exactly
	// so the strategy slot is a drop-in replacement.
	LBTS(regulators []RegulatingFederateOf[T]) T
	// Name is the registry key for Phase 3 TOML selection.
	Name() string
	// DeterminismPreserving reports whether this implementation honors
	// the NFR-DET-1 contract. Replay tests gate on this when
	// determinism mode is "per-impl-opt-in".
	DeterminismPreserving() bool
}

// defaultLBTS is the package-default LBTSStrategy. Its LBTS method
// delegates to the existing exported LBTS function so behavior is
// preserved bit-for-bit; this struct exists only as a swap-point.
type defaultLBTSOf[T core.LogicalTimeScalar] struct{}

// LBTS delegates to the package-level LBTS function.
func (defaultLBTSOf[T]) LBTS(regulators []RegulatingFederateOf[T]) T {
	return LBTSOf[T](regulators)
}

func (defaultLBTSOf[T]) BoundLBTS(regulators []RegulatingFederateOf[T]) core.TimeBoundOf[T] {
	return LBTSBoundOf(regulators)
}

// Name returns "default" — the registry key for the package default.
func (defaultLBTSOf[T]) Name() string { return "default" }

// DeterminismPreserving returns true: the default LBTS algorithm is
// order-independent (see lbts.go docstring) and so replay-safe.
func (defaultLBTSOf[T]) DeterminismPreserving() bool { return true }

// Compile-time assertion: defaultLBTS satisfies LBTSStrategy.
var _ LBTSStrategyOf[core.LogicalTime] = (*defaultLBTSOf[core.LogicalTime])(nil)

// DefaultLBTSStrategy returns the package-default LBTSStrategy. Phase 3
// research registry uses this to pre-register the "default" impl so the
// composition root and tests share the exact same instance the Manager
// uses when Options.LBTSStrategy is nil.
func DefaultLBTSStrategyOf[T core.LogicalTimeScalar]() LBTSStrategyOf[T] { return defaultLBTSOf[T]{} }

// GrantContext is the input bundle for a single grant decision. It
// captures the inputs that decideGrant takes today and is passed by
// value so strategies cannot mutate the caller's state.
//
// Field semantics mirror decideGrant in advance.go:
//   - Mode         : the AdvanceMode of the outstanding request.
//   - CurrentTime  : the federate's last granted (or initial) logical time.
//   - Requested    : the t parameter of the outstanding request.
//   - LBTS         : the strategy's regulating-set bound, capped by the
//     requester's available peer bound when it is time-constrained.
//   - SolePending  : true when this is the only pending request in the
//     federation. M38 GA: the default policy no longer consults it (the
//     NER/NMRA forced-grant hatch is retired); kept for alternative
//     strategies.
//   - NextTSOTime  : earliest buffered TSO timestamp queued for the
//     requester (M38 GA — the §8.8/§8.9 next-message grant target
//     input). Meaningful only when HasTSO is true.
//   - HasTSO       : whether any TSO message is queued for the requester.
type GrantContextOf[T core.LogicalTimeScalar] struct {
	Mode          AdvanceMode
	CurrentTime   T
	Requested     T
	LBTS          T
	LBTSUnbounded bool
	SolePending   bool
	NextTSOTime   T
	HasTSO        bool
}

// GrantDecision is the output of a strategy's DecideGrant call. It
// mirrors the (unexported) grantDecision struct in advance.go so the
// default strategy is a pure delegation. The fields are exported so
// alternative strategies in sibling packages (alt_*.go) can construct
// values directly.
//
// Fire == false means hold (the request stays pending untouched).
// ClearPending == true means the post-grant disposition is "full grant"
// (clear pendingNER); false advances currentTime but keeps the request
// pending. M38 GA: the default policy always clears (§8.8: one request,
// one grant); keep-pending remains available to alternative strategies.
type GrantDecisionOf[T core.LogicalTimeScalar] struct {
	Fire         bool
	Time         T
	ClearPending bool
}

// GrantStrategy is the per-service algorithm hook for the time-advance
// grant decision. Researchers swap in alternative grant policies
// (optimistic variants, lazy advance, predictive grants, …) by
// providing a type that implements this interface and wiring it through
// Options.GrantStrategy.
//
// The default implementation (defaultGrant) lifts the existing
// decideGrant body and is selected when Options.GrantStrategy is nil.
//
// See docs/research-platform.md §6.1 for the design context.
type GrantStrategyOf[T core.LogicalTimeScalar] interface {
	// DecideGrant evaluates a single pending request against the
	// current LBTS. Pure (no I/O, no mutex); the caller owns the outer
	// fixed-point loop and the side effects.
	DecideGrant(ctx GrantContextOf[T]) GrantDecisionOf[T]
	// Name is the registry key for Phase 3 TOML selection.
	Name() string
	// DeterminismPreserving reports whether this implementation honors
	// the NFR-DET-1 contract.
	DeterminismPreserving() bool
}

// defaultGrant is the package-default GrantStrategy. Its DecideGrant
// method delegates to the existing decideGrant function so behavior is
// preserved bit-for-bit.
type defaultGrantOf[T core.LogicalTimeScalar] struct{}

// DecideGrant delegates to the package-level decideGrant function.
func (defaultGrantOf[T]) DecideGrant(c GrantContextOf[T]) GrantDecisionOf[T] {
	d := decideScalarGrant(c.Mode, c.CurrentTime, c.Requested, c.LBTS, c.LBTSUnbounded && integerScalar[T](), c.NextTSOTime, c.HasTSO)
	return GrantDecisionOf[T]{
		Fire:         d.fire,
		Time:         d.time,
		ClearPending: d.clearPending,
	}
}

// Name returns "default" — the registry key for the package default.
func (defaultGrantOf[T]) Name() string { return "default" }

// DeterminismPreserving returns true: the default grant policy is a
// pure function of (Mode, CurrentTime, Requested, LBTS, SolePending)
// and so honors NFR-DET-1.
func (defaultGrantOf[T]) DeterminismPreserving() bool { return true }

// Compile-time assertion: defaultGrant satisfies GrantStrategy.
var _ GrantStrategyOf[core.LogicalTime] = (*defaultGrantOf[core.LogicalTime])(nil)

// DefaultGrantStrategy returns the package-default GrantStrategy. Phase 3
// research registry uses this to pre-register the "default" impl so the
// composition root and tests share the exact same instance the Manager
// uses when Options.GrantStrategy is nil.
func DefaultGrantStrategyOf[T core.LogicalTimeScalar]() GrantStrategyOf[T] {
	return defaultGrantOf[T]{}
}
