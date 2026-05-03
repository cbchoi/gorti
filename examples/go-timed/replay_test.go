//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSpec_M3_Replay_TimeAdvanceEventsByteIdentical is the M3
// milestone-gate test (TASK-049).
//
// Strategy:
//  1. Run the timed demo deterministically with --log-dir=A,
//     producing A/timed-replay.log.
//  2. Drive that log through rtid -mode=replay-from-log with
//     --log-dir=B, which uses eventlog.NewReplayer to feed each
//     record through a fresh CapturingSink. The sink writes
//     B/timed-replay.log.
//  3. Assert A/timed-replay.log == B/timed-replay.log byte-for-byte
//     (header included — the replay path explicitly reuses the source
//     header's CreatedAtNs to make the file fully byte-equal,
//     matching M2's pingpong replay-test contract).
//
// This is the production-equivalent of the spec test
// rti/spec/M3/replay_test.go::TestSpec_M3_Replay_TimeAdvanceEventsByteIdentical,
// but driven through the rtid binary so the example layer cannot
// import rti/internal/eventlog directly (Go internal-package rule).
//
// Implements: FR-EVT-3, NFR-DET-2; M3 exit criterion #3; M3 milestone
// gate.
func TestSpec_M3_Replay_TimeAdvanceEventsByteIdentical(t *testing.T) {
	bin := buildRtidOnce(t)
	const ticks = 50

	dirA := t.TempDir()
	dirB := t.TempDir()

	// Step 1: produce reference log.
	{
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := runExample(ctx, exampleArgs{
			FederationName: "timed-replay",
			Ticks:          ticks,
			LogDir:         dirA,
			Deterministic:  true,
			RtidBinary:     bin,
		}); err != nil {
			t.Fatalf("produce reference log: %v", err)
		}
	}

	srcPath := filepath.Join(dirA, "timed-replay.log")
	if _, err := os.Stat(srcPath); err != nil {
		t.Fatalf("stat reference log: %v", err)
	}

	// Step 2: replay through fresh rtid.
	{
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := runReplay(ctx, replayArgs{
			InputLogPath: srcPath,
			OutputDir:    dirB,
			RtidBinary:   bin,
		}); err != nil {
			t.Fatalf("replay: %v", err)
		}
	}

	// Step 3: byte-compare.
	srcBytes, err := os.ReadFile(srcPath) //nolint:gosec // path constructed from t.TempDir
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	capPath := filepath.Join(dirB, "timed-replay.log")
	capBytes, err := os.ReadFile(capPath) //nolint:gosec
	if err != nil {
		t.Fatalf("read captured: %v", err)
	}
	if !bytes.Equal(srcBytes, capBytes) {
		srcSum := sha256.Sum256(srcBytes)
		capSum := sha256.Sum256(capBytes)
		t.Errorf("replay sha256 mismatch:\n  source:    %x  (%d bytes)\n  captured:  %x  (%d bytes)",
			srcSum, len(srcBytes), capSum, len(capBytes))
	}
}

// TestSpec_M3_Replay_TwoRunsIdentical is a tighter sibling check: two
// independent runs of the timed demo with the same args produce
// byte-identical logs (same input → same output, NFR-DET-1).
//
// This complements the determinism harness's 20-scenario check by
// asserting the SAME default-triple workload reproduces twice.
func TestSpec_M3_Replay_TwoRunsIdentical(t *testing.T) {
	bin := buildRtidOnce(t)
	const ticks = 50

	dirA := t.TempDir()
	dirB := t.TempDir()

	for i, dir := range []string{dirA, dirB} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err := runExample(ctx, exampleArgs{
			FederationName: "timed-twin",
			Ticks:          ticks,
			LogDir:         dir,
			Deterministic:  true,
			RtidBinary:     bin,
		})
		cancel()
		if err != nil {
			t.Fatalf("run %c: %v", 'A'+rune(i), err)
		}
	}

	bodyA, err := readLogBody(filepath.Join(dirA, "timed-twin.log"))
	if err != nil {
		t.Fatalf("read A: %v", err)
	}
	bodyB, err := readLogBody(filepath.Join(dirB, "timed-twin.log"))
	if err != nil {
		t.Fatalf("read B: %v", err)
	}

	if !bytes.Equal(bodyA, bodyB) {
		sumA := sha256.Sum256(bodyA)
		sumB := sha256.Sum256(bodyB)
		t.Errorf("two-runs body sha256 mismatch:\n  A: %x\n  B: %x", sumA, sumB)
	}
}
