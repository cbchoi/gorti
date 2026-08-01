package main

// Round-2 GC product-default tests. These mutate process-global GC
// state (debug.SetGCPercent / debug.SetMemoryLimit), so every test
// pins a sentinel first and restores via t.Cleanup, and none of them
// calls t.Parallel (t.Setenv would reject it anyway).

import (
	"io"
	"log/slog"
	"math"
	"os"
	"runtime/debug"
	"testing"
)

// unsetGOGC removes GOGC from the environment for the duration of the
// test. t.Setenv registers restoration of the original value; the
// os.Unsetenv afterwards turns "set to empty" into a true unset.
func unsetGOGC(t *testing.T) {
	t.Helper()
	t.Setenv("GOGC", "")
	if err := os.Unsetenv("GOGC"); err != nil {
		t.Fatalf("os.Unsetenv(GOGC): %v", err)
	}
}

// pinGCPercent installs a sentinel GC percent and restores the prior
// value when the test finishes.
func pinGCPercent(t *testing.T, sentinel int) {
	t.Helper()
	prev := debug.SetGCPercent(sentinel)
	t.Cleanup(func() { debug.SetGCPercent(prev) })
}

func TestApplyGoRuntimeTuning_GOGCSet_LeavesGCUntouched(t *testing.T) {
	t.Setenv("GOGC", "200")
	pinGCPercent(t, 123)
	if err := applyGoRuntimeTuning(0, 0, nil); err != nil {
		t.Fatalf("applyGoRuntimeTuning: %v", err)
	}
	if got := currentGCPercent(); got != 123 {
		t.Fatalf("GC percent = %d after apply with GOGC set; want untouched sentinel 123", got)
	}
}

func TestApplyGoRuntimeTuning_GOGCUnset_InstallsProductDefault(t *testing.T) {
	unsetGOGC(t)
	pinGCPercent(t, 123)
	if err := applyGoRuntimeTuning(0, 0, nil); err != nil {
		t.Fatalf("applyGoRuntimeTuning: %v", err)
	}
	if got := currentGCPercent(); got != defaultGCPercent {
		t.Fatalf("GC percent = %d after apply with GOGC unset; want product default %d", got, defaultGCPercent)
	}
}

func TestApplyGoRuntimeTuning_EmptyGOGCCountsAsUnset(t *testing.T) {
	// The Go runtime ignores an empty GOGC, so the product default
	// must still engage — gogcEnvSet treats "" as unset.
	t.Setenv("GOGC", "")
	pinGCPercent(t, 123)
	if err := applyGoRuntimeTuning(0, 0, nil); err != nil {
		t.Fatalf("applyGoRuntimeTuning: %v", err)
	}
	if got := currentGCPercent(); got != defaultGCPercent {
		t.Fatalf("GC percent = %d after apply with empty GOGC; want product default %d", got, defaultGCPercent)
	}
}

func TestApplyGoRuntimeTuning_MinusOne_LeavesGCUntouched(t *testing.T) {
	// -1 opts out of the product default even when GOGC is unset.
	unsetGOGC(t)
	pinGCPercent(t, 123)
	if err := applyGoRuntimeTuning(gcPercentUnmanaged, 0, nil); err != nil {
		t.Fatalf("applyGoRuntimeTuning: %v", err)
	}
	if got := currentGCPercent(); got != 123 {
		t.Fatalf("GC percent = %d after apply with -1; want untouched sentinel 123", got)
	}
}

func TestApplyGoRuntimeTuning_ExplicitValueBeatsGOGC(t *testing.T) {
	// Explicit operator flag (>0 after main()'s translation) wins over
	// a set GOGC env: flag > env > product default.
	t.Setenv("GOGC", "200")
	pinGCPercent(t, 123)
	if err := applyGoRuntimeTuning(777, 0, nil); err != nil {
		t.Fatalf("applyGoRuntimeTuning: %v", err)
	}
	if got := currentGCPercent(); got != 777 {
		t.Fatalf("GC percent = %d after explicit 777 with GOGC set; want 777", got)
	}
}

func TestApplyGoRuntimeTuning_MemLimit_ExactValue(t *testing.T) {
	prev := debug.SetMemoryLimit(-1) // negative input reads without changing
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	pinGCPercent(t, 123)

	const limit = int64(3 << 30) // 3 GiB
	if err := applyGoRuntimeTuning(gcPercentUnmanaged, limit, nil); err != nil {
		t.Fatalf("applyGoRuntimeTuning: %v", err)
	}
	if got := debug.SetMemoryLimit(-1); got != limit {
		t.Fatalf("memory limit = %d after apply; want exact %d", got, limit)
	}
	if got := currentGCPercent(); got != 123 {
		t.Fatalf("GC percent = %d; -1 must leave GC untouched while setting the mem limit", got)
	}
}

func TestApplyGoRuntimeTuning_MemLimitZero_NoCall(t *testing.T) {
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	// Pin a recognizable non-default limit; a zero config value must
	// not disturb it (0 = "do not call SetMemoryLimit").
	debug.SetMemoryLimit(7 << 30)
	if err := applyGoRuntimeTuning(gcPercentUnmanaged, 0, nil); err != nil {
		t.Fatalf("applyGoRuntimeTuning: %v", err)
	}
	if got := debug.SetMemoryLimit(-1); got != 7<<30 {
		t.Fatalf("memory limit = %d after apply with 0; want untouched 7GiB sentinel", got)
	}
}

func TestApplyGoRuntimeTuning_RejectsNonsense(t *testing.T) {
	unsetGOGC(t)
	pinGCPercent(t, 123)
	prevLimit := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prevLimit) })

	cases := []struct {
		name      string
		gcPercent int
		memLimit  int64
	}{
		{"gc percent -2", -2, 0},
		{"gc percent below -1", -400, 0},
		{"gc percent above cap", maxGCPercent + 1, 0},
		{"negative mem limit", gcPercentUnmanaged, -5},
		{"negative mem limit with valid gc", 400, math.MinInt64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := applyGoRuntimeTuning(tc.gcPercent, tc.memLimit, nil); err == nil {
				t.Fatalf("applyGoRuntimeTuning(%d, %d) = nil error; want rejection",
					tc.gcPercent, tc.memLimit)
			}
			if err := validateGoRuntimeCLIConfig(tc.gcPercent, tc.memLimit); err == nil {
				t.Fatalf("validateGoRuntimeCLIConfig(%d, %d) = nil error; want rejection",
					tc.gcPercent, tc.memLimit)
			}
		})
	}
	// A rejected gc-percent must not have installed anything.
	if got := currentGCPercent(); got != 123 {
		t.Fatalf("GC percent = %d after rejected configs; want untouched sentinel 123", got)
	}
	if got := debug.SetMemoryLimit(-1); got != prevLimit {
		t.Fatalf("memory limit = %d after rejected configs; want untouched %d", got, prevLimit)
	}
}

// TestNewRTID_GCDefaultEngagesOnBenchCompositionPath is the executable
// proof for the round-2 placement question: the lrcbench harness
// composes newRTID with a zero-GCPercent rtidConfig, and the product
// default must fire on exactly that path — measured here via the
// debug.SetGCPercent read-back, not trusted.
func TestNewRTID_GCDefaultEngagesOnBenchCompositionPath(t *testing.T) {
	unsetGOGC(t)
	pinGCPercent(t, 123)

	// Identical composition to runLRCBench (lrc_bench_test.go).
	srv, err := newRTID(rtidConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("newRTID: %v", err)
	}
	t.Cleanup(func() { _ = srv.plugins.Close() })
	t.Cleanup(srv.grpcS.Stop)

	if got := currentGCPercent(); got != defaultGCPercent {
		t.Fatalf("GC percent = %d after bench-path newRTID with GOGC unset; want product default %d",
			got, defaultGCPercent)
	}
}

// TestNewRTID_GCDefaultRespectsGOGCOnBenchCompositionPath: operator env
// always wins — the same zero-GCPercent composition must NOT touch GC
// when GOGC is present.
func TestNewRTID_GCDefaultRespectsGOGCOnBenchCompositionPath(t *testing.T) {
	t.Setenv("GOGC", "200")
	pinGCPercent(t, 123)

	srv, err := newRTID(rtidConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("newRTID: %v", err)
	}
	t.Cleanup(func() { _ = srv.plugins.Close() })
	t.Cleanup(srv.grpcS.Stop)

	if got := currentGCPercent(); got != 123 {
		t.Fatalf("GC percent = %d after bench-path newRTID with GOGC set; want untouched sentinel 123", got)
	}
}

func TestResolveGCPercent_Table(t *testing.T) {
	cases := []struct {
		name    string
		v       int
		gogcSet bool
		want    int
		wantErr bool
	}{
		{"policy, env unset -> product default", 0, false, defaultGCPercent, false},
		{"policy, env set -> no call", 0, true, 0, false},
		{"explicit -1 -> no call even without env", gcPercentUnmanaged, false, 0, false},
		{"explicit -1 -> no call with env", gcPercentUnmanaged, true, 0, false},
		{"explicit value beats env", 777, true, 777, false},
		{"explicit value without env", 50, false, 50, false},
		{"cap value accepted", maxGCPercent, false, maxGCPercent, false},
		{"below -1 rejected", -2, false, 0, true},
		{"above cap rejected", maxGCPercent + 1, false, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveGCPercent(tc.v, tc.gogcSet)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveGCPercent(%d, %v) = %d, nil; want error", tc.v, tc.gogcSet, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveGCPercent(%d, %v): %v", tc.v, tc.gogcSet, err)
			}
			if got != tc.want {
				t.Fatalf("resolveGCPercent(%d, %v) = %d; want %d", tc.v, tc.gogcSet, got, tc.want)
			}
		})
	}
}
