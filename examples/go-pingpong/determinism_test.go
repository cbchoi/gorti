//go:build integration

package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// eventLogHeaderSize mirrors the on-disk header layout from
// rti/internal/eventlog/format.go. The header includes a CreatedAtNs
// field stamped from the real clock, so per-run headers naturally
// differ; the determinism contract (NFR-DET-1) compares the BODY bytes
// (records after the header) for byte-equality.
//
// This duplicates the constant rather than importing eventlog (which
// is internal and unreachable from examples/). The duplication is
// guarded by the M2 spec test rti/spec/M2/eventlog_test.go which
// pins the on-disk layout — if the header size changes there, this
// test fails noisily and gets updated in the same PR.
const eventLogHeaderSize = 64

// TestSpec_M2_Determinism_TenRuns runs the pingpong example 10
// consecutive times with the same seed (and same federation name) and
// asserts the captured event-log bodies are byte-identical.
//
// Implements: NFR-DET-1; M2 exit criterion #2.
func TestSpec_M2_Determinism_TenRuns(t *testing.T) {
	bin := buildRtidOnce(t)
	const runs = 10
	const rounds = 100

	hashes := make([][32]byte, runs)
	for i := 0; i < runs; i++ {
		dir := t.TempDir()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err := runExample(ctx, exampleArgs{
			FederationName: "pingpong-determinism",
			Rounds:         rounds,
			LogDir:         dir,
			Deterministic:  true,
			RtidBinary:     bin,
		})
		cancel()
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		body, err := readLogBody(filepath.Join(dir, "pingpong-determinism.log"))
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

// readLogBody reads the on-disk pingpong.log file and returns its body
// (everything after the eventLogHeaderSize-byte header). The header is
// stripped because it carries a per-run CreatedAtNs timestamp.
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
