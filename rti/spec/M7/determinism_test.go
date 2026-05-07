package m7spec

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// TestSpec_M7_Determinism_20RandomizedScenarios is the M7 milestone
// gate test. Contract: 20 randomized scenarios mixing NER + NMRA +
// TAR + TARA + FQR + lookahead variations + regulating-set permutations
// each run twice with the same seed produce byte-identical event traces.
//
// The harness builds a fresh time.Manager per scenario (so the
// per-Manager extOf side-table is isolated), runs a deterministic
// sequence of advance calls drawn from a per-scenario rand.Source, then
// SHA-256s a canonical encoding of the outbox + event-log emissions.
// Two runs of the same scenario MUST produce identical hashes.
//
// Coverage notes (cut-2 W1):
//   - Mixed-mode scenarios exercise the cross-mode interaction in
//     tryGrantPending (NER pending + TAR call from peer can grant both).
//   - Lookahead-zero scenarios cover the TARA/NMRA "grant at LBTS == t"
//     path that NER cannot reach (the M7 distinguishing semantic).
//   - The 20 scenarios are kept lean: each runs ~10-30 advance calls.
//     Cut-3 may extend to longer scenarios + concurrent goroutines once
//     the TSO queue ships and FQR has real drain semantics.
//
// Implements: NFR-DET-1, NFR-DET-2; M7 exit criterion.
func TestSpec_M7_Determinism_20RandomizedScenarios(t *testing.T) {
	const totalScenarios = 20
	const runsPerScenario = 2

	rng := rand.New(rand.NewSource(0x4D375CE3D)) //nolint:gosec // determinism harness
	scenarios := make([]m7DetScenario, totalScenarios)
	for i := range scenarios {
		scenarios[i] = m7DetScenario{
			Name:    "scenario-" + strconv.Itoa(i),
			Seed:    rng.Int63(),
			NumFeds: 2 + rng.Intn(4),   // 2-5 regulating federates
			NumOps:  10 + rng.Intn(21), // 10-30 advance ops
		}
	}

	for i := range scenarios {
		sc := scenarios[i]
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()
			var hashes [runsPerScenario][32]byte
			var bodies [runsPerScenario][]byte
			for r := 0; r < runsPerScenario; r++ {
				body, err := runM7DetScenario(sc)
				if err != nil {
					t.Fatalf("run %d: %v", r, err)
				}
				if len(body) == 0 {
					t.Fatalf("run %d: empty trace body — scenario produced no observable events",
						r)
				}
				bodies[r] = body
				hashes[r] = sha256.Sum256(body)
			}
			if hashes[0] != hashes[1] {
				t.Errorf("non-deterministic trace for %s:\n  run 0 sha=%x (%d bytes)\n  run 1 sha=%x (%d bytes)\n  body0=%q\n  body1=%q",
					sc.Name, hashes[0], len(bodies[0]), hashes[1], len(bodies[1]),
					string(bodies[0]), string(bodies[1]))
			}
		})
	}
}

// m7DetScenario is one randomized determinism case. Seed pins the
// per-scenario rand source so the operation sequence is reproducible
// across runs.
type m7DetScenario struct {
	Name    string
	Seed    int64
	NumFeds int // count of regulating federates (2-5)
	NumOps  int // count of advance operations (10-30)
}

// runM7DetScenario executes one scenario and returns a canonical byte
// trace of every observable event (outbox sends + event-log appends in
// the order they occurred). Two invocations with the same scenario MUST
// return byte-identical traces.
//
// Per-scenario state:
//   - Fresh time.Manager + fakeOutbox + permissiveEventLog (so extOf
//     side-table is empty).
//   - All federates have lookahead drawn from the scenario rng;
//     ~25% draw lookahead 0 to exercise the LBTS-equals-t path.
//   - Each op draws (federate, mode, requested-time-bump) from the
//     scenario rng; per-federate target tracker keeps requests above
//     the lookahead floor (avoids ErrTimeRequestInPast as scenario
//     noise).
//
// Operations whose pre-flight fails (e.g. duplicate request) are
// swallowed — their error contributes to the trace via the absence of
// any emission, which is itself observable in the trace (a missing
// entry).
func runM7DetScenario(sc m7DetScenario) ([]byte, error) {
	rng := rand.New(rand.NewSource(sc.Seed)) //nolint:gosec // determinism harness

	out := newFakeOutbox()
	log := newPermissiveEventLog()
	mgr, err := timepkg.New(timepkg.Options{
		Clock:    core.NewFakeClock(stdtime.Unix(0, 0)),
		Outbox:   out,
		EventLog: log,
	})
	if err != nil {
		return nil, fmt.Errorf("time.New: %w", err)
	}

	ctx := context.Background()
	const fed core.FederationName = "m7-det"

	// Enable regulation for sc.NumFeds federates with handles 1..N.
	// Lookahead in [0, 3]; ~25% chance of lookahead 0 to exercise the
	// LBTS-equals-t path that distinguishes the Available variants.
	lookaheads := make(map[core.FederateHandle]core.LogicalTime, sc.NumFeds)
	for i := 0; i < sc.NumFeds; i++ {
		h := core.FederateHandle(i + 1)
		var la core.LogicalTime
		if rng.Intn(4) == 0 {
			la = 0
		} else {
			la = core.LogicalTime(1 + rng.Intn(3)) // 1..3
		}
		lookaheads[h] = la
		if e := mgr.EnableRegulation(ctx, fed, h, la); e != nil {
			return nil, fmt.Errorf("enable %d: %w", h, e)
		}
	}

	// Operation generator: each op picks a random federate + mode +
	// requested-time. The requested-time tracks the federate's last
	// target so duplicate-pending requests don't dominate the trace;
	// we use lookahead+rng increment.
	type modeFn = func(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime) error
	modes := []modeFn{
		mgr.NextMessageRequest,
		mgr.NextMessageRequestAvailable,
		mgr.TimeAdvanceRequest,
		mgr.TimeAdvanceRequestAvailable,
		mgr.FlushQueueRequest,
	}
	// Per-federate "last-target" tracker so successive ops pick
	// monotonically-increasing requested times.
	target := make(map[core.FederateHandle]float64, sc.NumFeds)

	for op := 0; op < sc.NumOps; op++ {
		h := core.FederateHandle(1 + rng.Intn(sc.NumFeds))
		fn := modes[rng.Intn(len(modes))]
		la := float64(lookaheads[h])
		bump := la + float64(1+rng.Intn(3)) // 1..3 above lookahead floor
		target[h] += bump
		t := core.LogicalTime(target[h])
		// Best-effort: ignore ErrDuplicateNER (federate still pending
		// from a prior op) and ErrTimeRequestInPast (defensive — the
		// monotonic target tracker should already prevent this). These
		// errors are part of scenario noise; the trace records only
		// successful emissions.
		_ = fn(ctx, fed, h, t)
	}

	// Materialise the outbox + event-log into a canonical, ordered
	// byte trace. Each fixture preserves insertion order under its own
	// mutex; we concatenate the two streams.
	return canonicalTrace(out, log), nil
}

// canonicalTrace renders the outbox + event-log appends as a single
// deterministic byte stream. Format (one record per line):
//
//	"OUT  fed=<name> h=<int> evt=<TAG|HALT> time=<float>\n"
//	"LOG  fed=<name> seq=<int> evt=<descriptor>\n"
//
// The two streams are concatenated in order (outbox first, then log)
// because the cut-2 spec doesn't constrain the relative order across
// the two sinks; what matters is each sink's internal order is stable.
// (M3's stall test pins per-sink order, so this is consistent.)
func canonicalTrace(out *fakeOutbox, log *permissiveEventLog) []byte {
	var buf []byte
	for _, s := range out.Sent() {
		t, evtName := describeOutEvent(s.Event)
		buf = append(buf, []byte(fmt.Sprintf("OUT  fed=%s h=%d evt=%s time=%s\n",
			s.Federation, s.Federate, evtName, formatFloat(t)))...)
	}
	logRecs := snapshotLog(log)
	for _, r := range logRecs {
		buf = append(buf, []byte(fmt.Sprintf("LOG  fed=%s seq=%d evt=%s\n",
			r.Federation, r.Seq, describeLogEvent(r.Event)))...)
	}
	return buf
}

func describeOutEvent(e core.OutboundEvent) (float64, string) {
	switch ev := e.(type) {
	case *timepkg.TimeAdvanceGrant:
		return float64(ev.Time), "TAG"
	case *timepkg.FederationHalted:
		return 0, "HALT"
	default:
		return 0, "?"
	}
}

// describeLogEvent renders an EventRecord deterministically. The cut-2
// permissiveEventLog stores opaque records; we use fmt.Sprintf("%+v")
// which prints exported field names + values — stable across runs as
// long as Go's reflect.Value.String stays deterministic (which it is,
// per the language spec on fmt verb formatting).
func describeLogEvent(e core.EventRecord) string {
	return fmt.Sprintf("%+v", e)
}

// snapshotLog reads the permissiveEventLog's appended records under the
// mutex and returns a copy sorted by Seq. Insertion order already
// matches Seq, but the explicit sort defends against fixture refactors.
func snapshotLog(l *permissiveEventLog) []permissiveAppend {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]permissiveAppend, len(l.appended))
	copy(out, l.appended)
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

func formatFloat(f float64) string {
	// strconv.FormatFloat is locale-independent and round-trippable;
	// ideal for a determinism trace.
	return strconv.FormatFloat(f, 'g', -1, 64)
}
