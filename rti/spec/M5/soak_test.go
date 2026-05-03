//go:build soak

// Package m5spec / soak_test.go — long-running soak under the `soak` build
// tag. Excluded from the default `go test ./...` run; CI invokes it
// separately with -tags=soak.
//
// Per TASK-078: 10-minute default run; assert no panics, no goroutine
// leaks (sampled via runtime.NumGoroutine before/after), all RPC errors
// carry codes from proto/rti/v1/errors.proto.

package m5spec

import (
	"testing"
)

// TestSpec_M5_Soak_NoPanicNoLeak: spins up a real *rtid* server, drives
// a sustained mixed workload (object updates + interactions + NER) for
// the configured duration, asserts terminal goroutine count is within
// a small delta of the pre-test count.
//
// SCAFFOLD: Agent A wires this in TASK-078. The build tag keeps it out
// of the default test run; the test body should be a real test (not a
// skip) because the tag itself gates it.
//
// Implements: NFR-PERF-1..4; soak hardening contract.
func TestSpec_M5_Soak_NoPanicNoLeak(t *testing.T) {
	t.Fatalf("scaffolded; Agent A wires this in TASK-078 (build tag `soak` keeps it out of default test runs)")
}
