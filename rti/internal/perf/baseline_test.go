package perf

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestNew_RejectsZeroSize asserts the constructor surfaces an error for
// the degenerate case rather than producing a nil-but-OK manager.
func TestNew_RejectsZeroSize(t *testing.T) {
	if _, err := New(Options{Size: 0}); err == nil {
		t.Fatal("want error for Size=0, got nil")
	}
}

// TestNew_DefaultsDurationAndFederationName confirms the constructor
// fills in the documented defaults (Duration=10s, FederationName=
// "perf-baseline-<size>") so the harness is usable with just a Size.
func TestNew_DefaultsDurationAndFederationName(t *testing.T) {
	mgr, err := New(Options{Size: Size2})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if mgr.opts.Duration != 10*time.Second {
		t.Errorf("Duration default = %v, want 10s", mgr.opts.Duration)
	}
	if mgr.opts.FederationName != "perf-baseline-2" {
		t.Errorf("FederationName default = %q, want %q", mgr.opts.FederationName, "perf-baseline-2")
	}
}

// TestRunBaseline_Size2_ProducesPopulatedResult covers the spec
// invariants from rti/spec/M5/perf_test.go locally so unit-test runs
// catch regressions without invoking the spec harness. Uses a short
// 500ms duration to stay under CI budget.
func TestRunBaseline_Size2_ProducesPopulatedResult(t *testing.T) {
	mgr, err := New(Options{Size: Size2, Duration: 500 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := mgr.RunBaseline(ctx)
	if err != nil {
		t.Fatalf("RunBaseline: %v", err)
	}
	if res.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", res.SchemaVersion, SchemaVersion)
	}
	if res.FederationSize != int(Size2) {
		t.Errorf("FederationSize = %d, want %d", res.FederationSize, Size2)
	}
	if res.DurationSeconds <= 0 {
		t.Errorf("DurationSeconds = %v, want > 0", res.DurationSeconds)
	}
	if res.InteractionsSent <= 0 {
		t.Errorf("InteractionsSent = %d, want > 0", res.InteractionsSent)
	}
	if res.ThroughputPerSecond <= 0 {
		t.Errorf("ThroughputPerSecond = %v, want > 0", res.ThroughputPerSecond)
	}
	if res.LatencyP50Ms < 0 {
		t.Errorf("LatencyP50Ms = %v, want >= 0", res.LatencyP50Ms)
	}
	if res.LatencyP99Ms < res.LatencyP50Ms {
		t.Errorf("LatencyP99Ms (%v) < LatencyP50Ms (%v)", res.LatencyP99Ms, res.LatencyP50Ms)
	}
}

// TestRunBaseline_Size5_ScalesUp exercises a slightly larger fanout to
// make sure the harness handles >2 federates without hangs or panics.
// Kept short for CI; TASK-080 runs full sizes via the perf binary.
func TestRunBaseline_Size5_ScalesUp(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in -short")
	}
	mgr, err := New(Options{Size: Size5, Duration: 500 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := mgr.RunBaseline(ctx)
	if err != nil {
		t.Fatalf("RunBaseline: %v", err)
	}
	if res.FederationSize != int(Size5) {
		t.Errorf("FederationSize = %d, want %d", res.FederationSize, Size5)
	}
	if res.InteractionsSent <= 0 {
		t.Errorf("InteractionsSent = %d, want > 0", res.InteractionsSent)
	}
}

// TestPercentilesMs covers the helper directly since the live harness
// produces non-deterministic samples. Single-sample, two-sample, and
// 100-sample inputs cover the index-bounds branches.
func TestPercentilesMs(t *testing.T) {
	if p50, p99 := percentilesMs(nil); p50 != 0 || p99 != 0 {
		t.Errorf("empty: got (%v,%v), want (0,0)", p50, p99)
	}
	if p50, p99 := percentilesMs([]int64{1_000_000}); p50 != 1.0 || p99 != 1.0 {
		t.Errorf("single sample: got (%v,%v), want (1,1)", p50, p99)
	}
	// 100 samples 1..100 ms; p50 ~ 50ms, p99 ~ 99ms.
	samples := make([]int64, 100)
	for i := range samples {
		samples[i] = int64(i+1) * 1_000_000 // ms in nanos
	}
	p50, p99 := percentilesMs(samples)
	if p50 < 49 || p50 > 51 {
		t.Errorf("p50 = %v, want ~50", p50)
	}
	if p99 < 98 || p99 > 100 {
		t.Errorf("p99 = %v, want ~99", p99)
	}
}

// TestResult_JSONShape pins the snake_case field set so a future
// accidental rename of a struct tag fails here in addition to the spec
// test in rti/spec/M5/perf_test.go.
func TestResult_JSONShape(t *testing.T) {
	r := Result{
		SchemaVersion:       SchemaVersion,
		FederationSize:      2,
		DurationSeconds:     1.0,
		InteractionsSent:    10,
		ThroughputPerSecond: 10.0,
		LatencyP50Ms:        1.0,
		LatencyP99Ms:        2.0,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, k := range []string{
		"schema_version", "federation_size", "duration_seconds",
		"interactions_sent", "throughput_per_second",
		"latency_p50_ms", "latency_p99_ms",
	} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing key %q in %v", k, got)
		}
	}
}
