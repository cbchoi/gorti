package ownership

import (
	"math/rand"
	gosync "sync"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// randomAcquirer is a Phase-4 reference alternative NegotiationStrategy
// that picks a uniformly-random candidate acquirer instead of the
// lowest-handle one. It is the canonical NON-DETERMINISM-PRESERVING
// reference impl for the research platform: its DeterminismPreserving()
// returns false, which lights up the strict-mode rejection path in
// research.Apply (apply.go) and the per-impl-opt-in replay-skip path in
// the M3/M4 replay test fixtures.
//
// # When a researcher would want this
//
//   - As a pedagogical illustration of how a non-preserving strategy is
//     declared, registered, and rejected by strict mode. Without this
//     alt, the strict-mode gate is dead code; with it, a researcher can
//     write `determinism = "strict"` and `negotiation =
//     "random-acquirer"` in a TOML config and immediately see the rtid
//     refuse to start with a NonPreservingError.
//
//   - As a reference template for researchers writing their own
//     non-preserving alts (market-based, stochastic priority, simulated
//     bidding-with-random-tiebreaks). The shape is: hold a private
//     *rand.Rand, lock-protect it for goroutine safety since
//     SelectAcquirer can be called concurrently from multiple federate
//     RPC handlers, and honestly report DeterminismPreserving() ==
//     false because the same input produces different outputs across
//     runs.
//
//   - For perf or fairness studies where lowest-handle selection
//     introduces a starvation skew (federates with low handles always
//     win); a random pick averages out across many transfers.
//
// # Determinism contract
//
// math/rand is seeded from time.Now().UnixNano() at construction so a
// fresh rtid yields a different acquirer sequence each run. This is the
// classical non-determinism source the design doc §3.2 calls out
// (alongside time.Now and unsorted map iteration). DeterminismPreserving()
// returns false; the rtid composition root rejects this strategy at
// boot under determinism = "strict", and the replay-test fixtures skip
// with reason under determinism = "per-impl-opt-in".
//
// # Concurrency
//
// SelectAcquirer is invoked from gRPC handler goroutines that call
// ownership.Manager methods, which can be concurrent. *rand.Rand is
// not goroutine-safe, so we guard it with a mutex. The mutex is held
// only across a single Intn call, so contention is negligible.
type randomAcquirer struct {
	mu  gosync.Mutex
	rng *rand.Rand
}

// NewRandomAcquirer returns a fresh randomAcquirer seeded from the
// current wall time. Exposed for tests that want to verify the alt
// produces variable output; production wires through
// RandomAcquirerNegotiationStrategy().
//
// Each call returns an independent *rand.Rand instance — the package
// does not share global state — which keeps tests hermetic.
func NewRandomAcquirer() NegotiationStrategy {
	return &randomAcquirer{
		rng: rand.New(rand.NewSource(stdtime.Now().UnixNano())),
	}
}

// SelectAcquirer picks a uniformly-random candidate from c.Candidates,
// or core.InvalidFederateHandle when the set is empty (mirroring the
// default's "no transfer" behavior).
func (r *randomAcquirer) SelectAcquirer(c SelectAcquirerContext) core.FederateHandle {
	if len(c.Candidates) == 0 {
		return core.InvalidFederateHandle
	}
	r.mu.Lock()
	idx := r.rng.Intn(len(c.Candidates))
	r.mu.Unlock()
	return c.Candidates[idx]
}

// Name returns "random-acquirer", the registry key under which Phase 3's
// research.Default() pre-registers this alt.
func (*randomAcquirer) Name() string { return "random-acquirer" }

// DeterminismPreserving returns false: math/rand seeded from time.Now
// produces different output across runs given the same SelectAcquirer
// inputs. This is the honest-flag illustration that exercises the
// strict-mode rejection path in research.Apply.
func (*randomAcquirer) DeterminismPreserving() bool { return false }

// Compile-time assertion: *randomAcquirer satisfies NegotiationStrategy.
var _ NegotiationStrategy = (*randomAcquirer)(nil)

// RandomAcquirerNegotiationStrategy returns a fresh randomAcquirer
// instance. The research registry's Default() constructor calls this
// to register the alt under "random-acquirer".
//
// The returned value is a pointer — a shared instance across the rtid
// — because randomAcquirer holds rng state that has to persist between
// calls for the random sequence to be meaningful. Multiple federations
// share one rng; that's intentional and matches how a researcher
// running fairness experiments expects the seed to spread across the
// federation pool.
func RandomAcquirerNegotiationStrategy() NegotiationStrategy {
	return NewRandomAcquirer()
}
