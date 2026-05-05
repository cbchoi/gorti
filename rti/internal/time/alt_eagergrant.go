package time

// eagerGrant is a Phase-4 reference alternative GrantStrategy. It fires
// every grant immediately at the requested time, ignoring LBTS and the
// per-mode predicate divergence enforced by the default decideGrant.
//
// # When a researcher would want this
//
// As a deliberately-incorrect baseline for studying optimistic /
// what-if time-advance protocols. The eager strategy short-circuits
// the conservative LBTS gate and pretends every request is satisfiable
// the moment it arrives — which is the limiting case of optimistic
// scheduling (no rollback machinery, just trust the request). Useful
// for:
//
//   - Comparing throughput against the conservative default in perf
//     experiments where causality violations are tolerated.
//   - Sanity-checking that the GrantStrategy slot is actually consulted
//     end-to-end (a federate that requested t and saw a grant at t,
//     even with stale LBTS, proves the slot is wired).
//   - Pedagogical demos: what would the timing diagram look like if
//     we ignored Chandy-Misra-Bryant entirely?
//
// THIS BREAKS HLA STRICT SEMANTICS. A federate using this strategy may
// observe a TSO message in its past. Replay determinism may also be
// affected if the federation is mixed (the eager federate's grants are
// a pure function of (Mode, Requested) and stay deterministic, but the
// downstream message ordering does not). Per the determinism contract
// rule on this file: the strategy itself is deterministic (no rand, no
// time.Now, no map iteration), so DeterminismPreserving() returns true
// honestly. The upstream causality violation is a CORRECTNESS issue,
// not a determinism one.
//
// # Behavior
//
// On every DecideGrant call:
//
//   - Fire is true.
//   - Time is the requested time (federates land exactly where they
//     asked).
//   - ClearPending is true (no NER/NMRA forced-grant escape hatch;
//     every grant is a full grant).
//
// The default GrantStrategy by contrast routes through decideGrant
// which gates on LBTS and may return Fire=false (hold).
type eagerGrant struct{}

// DecideGrant returns an immediate full grant at the requested time,
// regardless of mode, current time, LBTS, or sole-pending status.
func (eagerGrant) DecideGrant(c GrantContext) GrantDecision {
	return GrantDecision{
		Fire:         true,
		Time:         c.Requested,
		ClearPending: true,
	}
}

// Name returns "eager", the registry key under which Phase 3's
// research.Default() pre-registers this alt.
func (eagerGrant) Name() string { return "eager" }

// DeterminismPreserving returns true: DecideGrant is a pure function of
// the input GrantContext (no rand, no time.Now, no map iteration). The
// causality violation introduced by ignoring LBTS is a CORRECTNESS
// issue, not a determinism one — the same input still produces the
// same output. Researchers running replay against an eager federation
// will see deterministic (but conservatively-incorrect) traces.
func (eagerGrant) DeterminismPreserving() bool { return true }

// Compile-time assertion: eagerGrant satisfies GrantStrategy.
var _ GrantStrategy = (*eagerGrant)(nil)

// EagerGrantStrategy returns the Phase-4 reference alt GrantStrategy.
// The research registry's Default() constructor calls this to register
// the alt under "eager".
func EagerGrantStrategy() GrantStrategy { return eagerGrant{} }
