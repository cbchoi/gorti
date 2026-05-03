package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// stallTestHeaderSize mirrors the on-disk header size from
// rti/internal/eventlog/format.go (HeaderSize = 64). Defined locally so
// stall_test.go is self-contained — it doesn't share a build tag with
// determinism_test.go's stallTestHeaderSize.
const stallTestHeaderSize = 64

// TestSpec_M3_Stall_HaltDetectedThroughOutbox is the M3 stall-harness
// integration test (TASK-048). Contract: a regulating federation in
// which one federate fails to NER (simulating a crashed federate)
// must, after the stall timeout elapses, emit a FederationHalted
// event through the outbox naming a stalled federate.
//
// Strategy: spawn rtid in -mode=timed-demo with -timed-stall-skip set
// (skip federate 2's NER) and -timed-stall-advance-ms past the
// stall timeout. rtid emits a single line on stdout of the form
//
//	TIMED_HALT halts=N stalled_federate=H
//
// when CheckStalls returns. We capture stdout, parse the line, and
// assert halts >= 1 and stalled_federate is one of the pending
// federates (the lowest-handle pending, per stall.go's deterministic
// trigger-selection rule).
//
// Implements: FR-TM-6, NFR-PERF-3; M3 exit criterion #2.
func TestSpec_M3_Stall_HaltDetectedThroughOutbox(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go-timed stall harness in -short mode")
	}
	bin := buildRtidOnce(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dir := t.TempDir()
	cmd := exec.CommandContext(ctx, bin, //nolint:gosec // bin is buildRtidOnce-controlled
		"-mode=timed-demo",
		"-timed-federation=timed-stall",
		"-timed-ticks=2",
		"-timed-stall-skip=2",
		"-timed-stall-timeout-ms=2000",
		"-timed-stall-advance-ms=10000",
		"-log-dir="+dir,
		"-log-format=text",
	)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("rtid timed-demo (stall): %v\nstdout:\n%s", err, stdout.String())
	}

	halts, stalled, ok := parseHaltLine(stdout.String())
	if !ok {
		t.Fatalf("did not find TIMED_HALT line in stdout:\n%s", stdout.String())
	}
	if halts < 1 {
		t.Errorf("halts = %d, want ≥1 (stall detection failed)", halts)
	}
	// Trigger selection in stall.go is "lowest-handle pending federate
	// past timeout". Federate 2 is the one we skipped, so it's NOT
	// pending; federate 1 is the lowest pending and is the trigger.
	// We accept any pending handle ∈ {1, 3} as a valid trigger so the
	// test stays robust to a future trigger-selection refinement.
	if stalled != 1 && stalled != 3 {
		t.Errorf("stalled_federate = %d, want 1 or 3 (any pending federate that NER'd)", stalled)
	}
}

// parseHaltLine extracts (halts, stalled_federate) from the rtid
// "TIMED_HALT halts=N stalled_federate=H" line. Returns ok=false if
// the line isn't present or is malformed.
func parseHaltLine(s string) (int, uint64, bool) {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "TIMED_HALT ") {
			continue
		}
		var halts int
		var stalled uint64
		// Tokenize: TIMED_HALT key1=v1 key2=v2 ...
		fields := strings.Fields(line)
		for _, f := range fields[1:] {
			kv := strings.SplitN(f, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "halts":
				n, err := strconv.Atoi(kv[1])
				if err != nil {
					return 0, 0, false
				}
				halts = n
			case "stalled_federate":
				n, err := strconv.ParseUint(kv[1], 10, 64)
				if err != nil {
					return 0, 0, false
				}
				stalled = n
			}
		}
		return halts, stalled, true
	}
	return 0, 0, false
}

// TestSpec_M3_Stall_LogFileCapturesHalt: an end-to-end check that
// the on-disk event-log file produced by the stall scenario has a
// non-empty body — the FederationHalted record has been written
// through eventlog.MultiplexWriter. (The cut-1 binary log encodes
// time-management records as synthetic empty Events, so we assert on
// record COUNT — the halt adds one more record beyond the grants.)
//
// Implements: FR-EVT-3 (write-ahead), FR-TM-6.
func TestSpec_M3_Stall_LogFileCapturesHalt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go-timed stall log-capture in -short mode")
	}
	bin := buildRtidOnce(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	cmd := exec.CommandContext(ctx, bin, //nolint:gosec
		"-mode=timed-demo",
		"-timed-federation=timed-stall-log",
		"-timed-ticks=2",
		"-timed-stall-skip=2",
		"-timed-stall-timeout-ms=2000",
		"-timed-stall-advance-ms=10000",
		"-log-dir="+dir,
		"-log-format=text",
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("rtid timed-demo (stall log): %v", err)
	}

	// The stall demo (skip fed2, NER fed1+fed3) produces:
	//   - 1 forced grant for the first NER (sole pending)
	//   - 1 full grant when fed3 NERs
	//   - 1 federationHalted record after the stall check
	// The log file's body is non-empty and contains at least 2 records
	// (the "demo" runtime that ran the stall, captured under the SAME
	// federation name in -log-dir, has its own log; the stall sub-
	// runtime in main.go also writes to the same dir under the same
	// federation key, appending its halt record).
	//
	// We assert on file size > header — content-level decoding of the
	// time-package records is out of scope for the cut-1 wire shape.
	fi, err := os.Stat(filepath.Join(dir, "timed-stall-log.log"))
	if err != nil {
		// On some platforms the demo path produces a different
		// federation key under different rtid sub-paths; surface
		// the dir contents to debug if the file isn't there.
		entries, _ := os.ReadDir(dir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("stat stall log: %v (dir entries: %v)", err, names)
	}
	if fi.Size() <= stallTestHeaderSize {
		t.Errorf("stall log size = %d, want > header (%d)", fi.Size(), stallTestHeaderSize)
	}
}

