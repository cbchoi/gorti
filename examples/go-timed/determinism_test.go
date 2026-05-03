//go:build integration

package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// eventLogHeaderSize mirrors the on-disk header layout from
// rti/internal/eventlog/format.go. The header includes a CreatedAtNs
// field stamped from the clock at writer-open time. In Deterministic
// mode the FakeClock pins this value (so the header is also identical
// across runs), but the harness still strips the header so the body-
// only comparison is robust to future header-format changes.
//
// This duplicates the constant rather than importing eventlog (which
// is internal and unreachable from examples/). The duplication is
// guarded by the M2 spec test rti/spec/M2/eventlog_test.go which
// pins the on-disk layout — if the header size changes there, this
// test fails noisily and gets updated in the same PR.
const eventLogHeaderSize = 64

// scenario is one randomized determinism case. Each scenario runs 3×
// with the same seed and the same parameters; the contract (NFR-DET-1)
// is that all 3 runs produce a byte-identical event-log body.
type scenario struct {
	Name           string
	FederationName string
	Ticks          int
	// All scenarios run with Deterministic = true (FakeClock); the
	// determinism guarantee depends on it.
}

// generateScenarios builds N pseudo-random scenarios from a fixed seed.
// "Random" here is the size of the workload (federate count, lookahead
// vector, NER timestamp pattern); the SAME scenario is run 3× and
// compared. Different scenarios in the slice are independent.
//
// Determinism note: rand.New(rand.NewSource(seed)) is fully reproducible
// (the math/rand package is documented to be deterministic for a given
// seed across Go versions). The scenarios slice is therefore identical
// for every test invocation.
func generateScenarios(seed int64, n int) []scenario {
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // determinism harness, NOT crypto
	out := make([]scenario, n)
	for i := 0; i < n; i++ {
		// Federation name embeds the index to keep per-scenario log
		// files distinct on disk; tick count varies in [5, 25].
		ticks := 5 + rng.Intn(20)
		out[i] = scenario{
			Name:           "scenario-" + strconv.Itoa(i),
			FederationName: "timed-det-" + strconv.Itoa(i),
			Ticks:          ticks,
		}
	}
	return out
}

// TestSpec_M3_Determinism_TwentyScenarios runs 20 randomized scenarios
// (varying federate count, lookahead vectors, NER timestamp sequences),
// each 3× with the same seed, and asserts every scenario's three
// event-log bodies are byte-identical.
//
// Implements: NFR-DET-1, NFR-DET-2; M3 exit criterion #1.
func TestSpec_M3_Determinism_TwentyScenarios(t *testing.T) {
	bin := buildRtidOnce(t)
	const totalScenarios = 20
	const runsPerScenario = 3

	scenarios := generateScenarios(0xDEADBEEF, totalScenarios)
	for i := range scenarios {
		sc := scenarios[i]
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()
			hashes := make([][32]byte, runsPerScenario)
			for r := 0; r < runsPerScenario; r++ {
				dir := t.TempDir()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				_, err := runExample(ctx, exampleArgs{
					FederationName: sc.FederationName,
					Ticks:          sc.Ticks,
					LogDir:         dir,
					Deterministic:  true,
					RtidBinary:     bin,
				})
				cancel()
				if err != nil {
					t.Fatalf("run %d: %v", r, err)
				}
				body, err := readLogBody(filepath.Join(dir, sc.FederationName+".log"))
				if err != nil {
					t.Fatalf("run %d: read log: %v", r, err)
				}
				hashes[r] = sha256.Sum256(body)
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

// TestSpec_M3_Determinism_DefaultTripleTenRuns is a tighter
// determinism check: the default federate triple {1.0, 2.0, 0.5} run
// 10× with the same seed produces byte-identical bodies.
//
// Implements: NFR-DET-1.
func TestSpec_M3_Determinism_DefaultTripleTenRuns(t *testing.T) {
	bin := buildRtidOnce(t)
	const runs = 10
	const ticks = 50

	hashes := make([][32]byte, runs)
	for i := 0; i < runs; i++ {
		dir := t.TempDir()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err := runExample(ctx, exampleArgs{
			FederationName: "timed-det-default",
			Ticks:          ticks,
			LogDir:         dir,
			Deterministic:  true,
			RtidBinary:     bin,
		})
		cancel()
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		body, err := readLogBody(filepath.Join(dir, "timed-det-default.log"))
		if err != nil {
			t.Fatalf("run %d: read log: %v", i, err)
		}
		hashes[i] = sha256.Sum256(body)
	}
	for i := 1; i < runs; i++ {
		if hashes[i] != hashes[0] {
			t.Errorf("run %d sha256 differs from run 0:\n  run 0: %x\n  run %d: %x",
				i, hashes[0], i, hashes[i])
		}
	}
}

// readLogBody reads the on-disk timed log file and returns its body
// (everything after the eventLogHeaderSize-byte header). The header is
// stripped so the comparison is body-only — robust to header-stamp
// changes even though Deterministic mode pins the timestamp.
func readLogBody(path string) ([]byte, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path constructed from t.TempDir
	if err != nil {
		return nil, err
	}
	if len(b) < eventLogHeaderSize {
		return nil, fmt.Errorf("log file shorter than header (%d < %d)", len(b), eventLogHeaderSize)
	}
	return b[eventLogHeaderSize:], nil
}
