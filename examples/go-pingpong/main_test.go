package main

import (
	"context"
	"testing"
	"time"
)

// TestPingpong_Smoke runs the pingpong scenario with a small round-trip
// budget and a tight timeout. The full <5s budget is exercised by the
// determinism harness; this smoke just confirms the example wires up
// correctly and exits without error.
func TestPingpong_Smoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := pingpongConfig{
		FederationName: "pingpong-smoke",
		Rounds:         50,
		LogDir:         "",
	}
	stats, err := runPingpong(ctx, cfg)
	if err != nil {
		t.Fatalf("runPingpong: %v", err)
	}
	if stats.RoundsCompleted != cfg.Rounds {
		t.Errorf("rounds completed = %d, want %d", stats.RoundsCompleted, cfg.Rounds)
	}
}

// TestPingpong_FullBudget runs the full 1000-round target and asserts the
// runtime is under the 5s budget set by srs.md §10.2 M2 exit criterion 1.
func TestPingpong_FullBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full-budget run in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := pingpongConfig{
		FederationName: "pingpong-full",
		Rounds:         1000,
		LogDir:         "",
	}
	start := time.Now()
	stats, err := runPingpong(ctx, cfg)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("runPingpong: %v", err)
	}
	if stats.RoundsCompleted != cfg.Rounds {
		t.Errorf("rounds completed = %d, want %d", stats.RoundsCompleted, cfg.Rounds)
	}
	if elapsed > 5*time.Second {
		t.Errorf("pingpong took %v; M2 budget is 5s", elapsed)
	}
}
