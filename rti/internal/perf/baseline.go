package perf

import (
	"context"
	"errors"
	"time"
)

// ErrNotImplemented is returned by stub methods until Agent A implements
// them in TASK-079. Spec tests in rti/spec/M5/perf_test.go fail RED with
// this error initially.
var ErrNotImplemented = errors.New("perf: not implemented (Agent A M5 deliverable)")

// SchemaVersion is the JSON schema version for Result. Bump when adding
// fields; downstream agents (e.g. TASK-084) version-check before reading.
const SchemaVersion = 1

// FederationSize is one of the supported perf-baseline configurations.
// M5 exit demands all four at TASK-080 run time.
type FederationSize int

const (
	Size2   FederationSize = 2
	Size5   FederationSize = 5
	Size25  FederationSize = 25
	Size100 FederationSize = 100
)

// Result is the JSON-serializable output of one Manager.RunBaseline call.
// Field names + types are FROZEN; serialized via encoding/json with
// snake_case via JSON tags below.
type Result struct {
	SchemaVersion       int     `json:"schema_version"`
	FederationSize      int     `json:"federation_size"`
	DurationSeconds     float64 `json:"duration_seconds"`
	InteractionsSent    int64   `json:"interactions_sent"`
	ThroughputPerSecond float64 `json:"throughput_per_second"`
	LatencyP50Ms        float64 `json:"latency_p50_ms"`
	LatencyP99Ms        float64 `json:"latency_p99_ms"`
	Notes               string  `json:"notes,omitempty"`
}

// Options configure one Manager.RunBaseline call.
type Options struct {
	// Size is the number of federates to spawn. Required.
	Size FederationSize

	// Duration is how long to drive the workload. Defaults to 10s when zero.
	Duration time.Duration

	// RtidAddress is the gRPC endpoint the Manager dials. Defaults to
	// ":8442" when empty (matches cmd/rtid's default).
	RtidAddress string

	// FederationName scopes the run. Defaults to "perf-baseline-<size>".
	FederationName string
}

// Manager runs a single perf-baseline configuration end-to-end.
//
// Spec test contract (rti/spec/M5/perf_test.go::TestSpec_M5_PerfHarnessRuns):
// Manager.RunBaseline must produce a Result with all numeric fields
// populated and SchemaVersion == 1. The test runs at Size2 with a short
// duration; TASK-080 runs all four sizes for the full 10s each.
type Manager struct {
	opts Options
}

// New constructs a Manager. Validates Options.Size; other fields default.
func New(opts Options) (*Manager, error) {
	_ = opts
	return &Manager{opts: opts}, ErrNotImplemented
}

// RunBaseline executes the configured measurement and returns a Result.
//
// FROZEN-shape: Agent A implements the body. Spec test asserts the
// returned Result has correct shape (schema version, size, non-zero
// throughput, populated latency percentiles).
func (m *Manager) RunBaseline(ctx context.Context) (Result, error) {
	_ = ctx
	return Result{}, ErrNotImplemented
}
