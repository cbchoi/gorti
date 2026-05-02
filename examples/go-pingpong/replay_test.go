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

// TestSpec_M2_Replay_ByteIdentical is the M2 milestone-gate test.
//
// Strategy:
//  1. Run the pingpong demo deterministically with --log-dir=A,
//     producing A/pingpong-replay.log.
//  2. Drive that log through rtid -mode=replay-from-log with
//     --log-dir=B, which uses eventlog.NewReplayer to feed each
//     record through a fresh CapturingSink. The sink writes
//     B/pingpong-replay.log.
//  3. Assert A/pingpong-replay.log == B/pingpong-replay.log
//     byte-for-byte (header included — the replay path explicitly
//     reuses the source header's CreatedAtNs to make the file fully
//     byte-equal, going beyond the W2B documented header-exclusion).
//
// This is the production-equivalent of the spec test
// rti/spec/M2/replay_test.go::TestSpec_M2_Replay_ByteIdentical, but
// driven through the rtid binary so the example layer cannot import
// rti/internal/eventlog directly (Go internal-package rule).
//
// Implements: NFR-DET-2; M2 exit criterion #3; M2 milestone gate.
func TestSpec_M2_Replay_ByteIdentical(t *testing.T) {
	bin := buildRtidOnce(t)
	const rounds = 100

	dirA := t.TempDir()
	dirB := t.TempDir()

	// Step 1: produce reference log.
	{
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := runExample(ctx, exampleArgs{
			FederationName: "pingpong-replay",
			Rounds:         rounds,
			LogDir:         dirA,
			Deterministic:  true,
			RtidBinary:     bin,
		}); err != nil {
			t.Fatalf("produce reference log: %v", err)
		}
	}

	srcPath := filepath.Join(dirA, "pingpong-replay.log")
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
	capPath := filepath.Join(dirB, "pingpong-replay.log")
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

// TestSpec_M2_Replay_TwoRunsIdentical is a tighter sibling check: two
// independent runs of the pingpong demo with the same args produce
// byte-identical logs (same input → same output, NFR-DET-1).
func TestSpec_M2_Replay_TwoRunsIdentical(t *testing.T) {
	bin := buildRtidOnce(t)
	const rounds = 100

	dirA := t.TempDir()
	dirB := t.TempDir()

	for i, dir := range []string{dirA, dirB} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err := runExample(ctx, exampleArgs{
			FederationName: "pingpong-twin",
			Rounds:         rounds,
			LogDir:         dir,
			Deterministic:  true,
			RtidBinary:     bin,
		})
		cancel()
		if err != nil {
			t.Fatalf("run %c: %v", 'A'+rune(i), err)
		}
	}

	bodyA, err := readLogBody(filepath.Join(dirA, "pingpong-twin.log"))
	if err != nil {
		t.Fatalf("read A: %v", err)
	}
	bodyB, err := readLogBody(filepath.Join(dirB, "pingpong-twin.log"))
	if err != nil {
		t.Fatalf("read B: %v", err)
	}

	if !bytes.Equal(bodyA, bodyB) {
		sumA := sha256.Sum256(bodyA)
		sumB := sha256.Sum256(bodyB)
		t.Errorf("two-runs body sha256 mismatch:\n  A: %x\n  B: %x", sumA, sumB)
	}
}
