package time

import "github.com/cbchoi/gorti/rti/internal/core"

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
type LBTSStrategy interface {
	// LBTS computes the lower bound on time stamps over the regulating
	// set. Signature MUST match the package-level LBTS function exactly
	// so the strategy slot is a drop-in replacement.
	LBTS(regulators []RegulatingFederate) core.LogicalTime
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
type defaultLBTS struct{}

// LBTS delegates to the package-level LBTS function.
func (defaultLBTS) LBTS(regulators []RegulatingFederate) core.LogicalTime {
	return LBTS(regulators)
}

// Name returns "default" — the registry key for the package default.
func (defaultLBTS) Name() string { return "default" }

// DeterminismPreserving returns true: the default LBTS algorithm is
// order-independent (see lbts.go docstring) and so replay-safe.
func (defaultLBTS) DeterminismPreserving() bool { return true }

// Compile-time assertion: defaultLBTS satisfies LBTSStrategy.
var _ LBTSStrategy = (*defaultLBTS)(nil)
