package m5spec

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/perf"
)

// TestSpec_M5_PerfHarnessRuns: the perf.Manager.RunBaseline harness
// produces a Result with the documented schema through
// perf.New and perf.Manager.RunBaseline.
//
// The test runs at the smallest size (Size2) for a brief duration to
// keep CI under budget. TASK-080 runs at all four sizes (2/5/25/100)
// for the full 10s each and records the benchmark results.
//
// Implements: NFR-PERF-1, NFR-PERF-2, NFR-SCALE-2; M5 exit criterion.
func TestSpec_M5_PerfHarnessRuns(t *testing.T) {
	mgr, err := perf.New(perf.Options{
		Size:     perf.Size2,
		Duration: 1 * stdtime.Second,
	})
	if err != nil {
		if errors.Is(err, perf.ErrNotImplemented) {
			t.Skip("perf.New not yet implemented (TASK-079 RED state)")
		}
		t.Fatalf("perf.New: %v", err)
	}
	if mgr == nil {
		t.Fatal("perf.New returned nil manager without error")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*stdtime.Second)
	defer cancel()

	res, err := mgr.RunBaseline(ctx)
	if errors.Is(err, perf.ErrNotImplemented) {
		t.Skip("perf.Manager.RunBaseline not yet implemented (TASK-079 RED state)")
	}
	if err != nil {
		t.Fatalf("RunBaseline: %v", err)
	}

	// Schema invariants — checked structurally, not by value.
	if res.SchemaVersion != perf.SchemaVersion {
		t.Errorf("Result.SchemaVersion = %d, want %d", res.SchemaVersion, perf.SchemaVersion)
	}
	if res.FederationSize != int(perf.Size2) {
		t.Errorf("Result.FederationSize = %d, want %d", res.FederationSize, perf.Size2)
	}
	if res.DurationSeconds <= 0 {
		t.Errorf("Result.DurationSeconds = %v, want > 0", res.DurationSeconds)
	}
	if res.InteractionsSent <= 0 {
		t.Errorf("Result.InteractionsSent = %d, want > 0 (workload should send something)", res.InteractionsSent)
	}
	if res.ThroughputPerSecond <= 0 {
		t.Errorf("Result.ThroughputPerSecond = %v, want > 0", res.ThroughputPerSecond)
	}
	if res.LatencyP50Ms < 0 {
		t.Errorf("Result.LatencyP50Ms = %v, want >= 0", res.LatencyP50Ms)
	}
	if res.LatencyP99Ms < res.LatencyP50Ms {
		t.Errorf("Result.LatencyP99Ms (%v) < LatencyP50Ms (%v)", res.LatencyP99Ms, res.LatencyP50Ms)
	}
}

// TestSpec_M5_PerfResultSerializesToDocumentedSchema: round-trips the
// Result via encoding/json; the resulting object has all documented
// snake_case fields. This pins the schema for downstream tooling
// (TASK-084 consumes the Result produced by TASK-080).
//
// Implements: NFR-PERF-1; schema-stability contract.
func TestSpec_M5_PerfResultSerializesToDocumentedSchema(t *testing.T) {
	res := perf.Result{
		SchemaVersion:       perf.SchemaVersion,
		FederationSize:      2,
		DurationSeconds:     1.5,
		InteractionsSent:    100,
		ThroughputPerSecond: 66.7,
		LatencyP50Ms:        4.2,
		LatencyP99Ms:        12.0,
		Notes:               "test",
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	required := []string{
		"schema_version", "federation_size", "duration_seconds",
		"interactions_sent", "throughput_per_second",
		"latency_p50_ms", "latency_p99_ms",
	}
	for _, key := range required {
		if _, ok := got[key]; !ok {
			t.Errorf("Result JSON missing required field %q (got: %v)", key, got)
		}
	}
}
