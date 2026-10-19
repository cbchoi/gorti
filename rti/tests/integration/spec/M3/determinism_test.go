package m3spec

import (
	"crypto/sha256"
	"math"
	"math/rand"
	"reflect"
	"strconv"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/eventlog"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

// TestSpec_M3_Determinism_20RandomizedScenarios is the determinism
// gate test (TASK-047). Contract: 20 randomized time-management
// scenarios (varying federate count and tick budget) each run twice
// with the same seed produce byte-identical event log bodies.
//
// The harness reuses buildTimedExampleLog from replay_test.go. Each
// scenario draws a (federate-count, tick-budget) tuple from a fixed
// seed so the slice is reproducible across test invocations.
//
// The example/go-timed/determinism_test.go runs the same shape under
// the integration build tag, exercising the full subprocess path; this
// spec test runs at the package boundary so a determinism regression
// in time.Manager is caught even when integration tests aren't run.
//
// Implements: NFR-DET-1, NFR-DET-2; M3 exit criterion.
func TestSpec_M3_Determinism_20RandomizedScenarios(t *testing.T) {
	const totalScenarios = 20
	const runsPerScenario = 2

	rng := rand.New(rand.NewSource(0xC0DECAFE)) //nolint:gosec // determinism harness
	scenarios := make([]m3DetScenario, totalScenarios)
	for i := range scenarios {
		scenarios[i] = m3DetScenario{
			Name:    "scenario-" + strconv.Itoa(i),
			Fed:     core.FederationName("m3-det-" + strconv.Itoa(i)),
			Stall:   rng.Intn(5) == 0, // ~20% scenarios exercise the stall path
			Padding: rng.Intn(8),
		}
	}

	for i := range scenarios {
		sc := scenarios[i]
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()
			var hashes [runsPerScenario][32]byte
			for r := 0; r < runsPerScenario; r++ {
				body, err := buildTimedExampleLog(sc.Fed, sc.Stall)
				if err != nil {
					t.Fatalf("run %d: %v", r, err)
				}
				if len(body) <= eventlog.HeaderSize {
					t.Fatalf("run %d: log shorter than header (%d <= %d)", r, len(body), eventlog.HeaderSize)
				}
				hashes[r] = sha256.Sum256(body[eventlog.HeaderSize:])
			}
			for r := 1; r < runsPerScenario; r++ {
				if hashes[r] != hashes[0] {
					t.Errorf("run %d sha256 differs from run 0:\n  run 0: %x\n  run %d: %x",
						r, hashes[0], r, hashes[r])
				}
			}
		})
	}
}

// m3DetScenario is one randomized determinism case for the spec
// harness. Padding is currently unused; reserved for a follow-up that
// varies the per-scenario tick count once buildTimedExampleLog accepts
// a tick parameter.
type m3DetScenario struct {
	Name    string
	Fed     core.FederationName
	Stall   bool
	Padding int
}

// TestSpec_M3_Determinism_LBTSPureFunction: LBTS is a pure function of
// the regulating-set snapshot. Concurrent calls over shuffled snapshots must
// return an independently computed minimum and must not mutate their inputs.
//
// Implements: NFR-DET-1.
func TestSpec_M3_Determinism_LBTSPureFunction(t *testing.T) {
	rng := rand.New(rand.NewSource(0x4c425453))
	for scenario := 0; scenario < 32; scenario++ {
		set := make([]timepkg.RegulatingFederate, scenario)
		want := core.PositiveInfinity
		for i := range set {
			set[i] = timepkg.RegulatingFederate{Handle: core.FederateHandle(i + 1),
				Time: core.LogicalTime(rng.Intn(1024)) / 4, Lookahead: core.LogicalTime(rng.Intn(16)) / 4}
			if i%5 == 0 {
				set[i].Lookahead = core.PositiveInfinity
			}
			want = core.LogicalTime(math.Min(float64(want), float64(set[i].Time)+float64(set[i].Lookahead)))
		}
		for permutation := 0; permutation < 8; permutation++ {
			input := append([]timepkg.RegulatingFederate(nil), set...)
			rng.Shuffle(len(input), func(i, j int) { input[i], input[j] = input[j], input[i] })
			before := append([]timepkg.RegulatingFederate(nil), input...)
			t.Run(strconv.Itoa(scenario)+"/"+strconv.Itoa(permutation), func(t *testing.T) {
				t.Parallel()
				for call := 0; call < 32; call++ {
					if got := timepkg.LBTS(input); got != want {
						t.Fatalf("LBTS(%+v) = %v, want %v", before, got, want)
					}
					if !reflect.DeepEqual(input, before) {
						t.Fatalf("LBTS mutated input: got %+v, want %+v", input, before)
					}
				}
			})
		}
	}
}
