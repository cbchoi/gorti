package main

import (
	"context"
	"errors"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/eventlog"
)

// TestTimedDemo_RunsToCompletion: a small in-process run completes
// without error and reports the configured tick count.
func TestTimedDemo_RunsToCompletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*stdtime.Second)
	defer cancel()
	stats, err := runTimedDemo(ctx, timedConfig{
		FederationName: "timed-test",
		Ticks:          10,
		Deterministic:  true,
	})
	if err != nil {
		t.Fatalf("runTimedDemo: %v", err)
	}
	if stats.TicksCompleted != 10 {
		t.Errorf("TicksCompleted = %d, want 10", stats.TicksCompleted)
	}
	if stats.FederateCount != len(defaultTimedLookaheads) {
		t.Errorf("FederateCount = %d, want %d", stats.FederateCount, len(defaultTimedLookaheads))
	}
	if stats.GrantsObserved == 0 {
		t.Errorf("GrantsObserved = 0, expected at least one TimeAdvanceGrant for the 3-federate demo")
	}
}

// TestTimedDemo_AcceptsCustomLookaheads: passing an explicit lookahead
// vector overrides the default and determines the federate count.
func TestTimedDemo_AcceptsCustomLookaheads(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*stdtime.Second)
	defer cancel()
	custom := []core.LogicalTime{0.25, 0.5, 0.75, 1.0}
	stats, err := runTimedDemo(ctx, timedConfig{
		FederationName: "timed-custom",
		Ticks:          5,
		Lookaheads:     custom,
		Deterministic:  true,
	})
	if err != nil {
		t.Fatalf("runTimedDemo: %v", err)
	}
	if stats.FederateCount != len(custom) {
		t.Errorf("FederateCount = %d, want %d", stats.FederateCount, len(custom))
	}
	if stats.TicksCompleted != 5 {
		t.Errorf("TicksCompleted = %d, want 5", stats.TicksCompleted)
	}
}

// TestTimedDemo_WritesLogFile: with --log-dir set, the demo persists a
// per-federation log file under the dir.
func TestTimedDemo_WritesLogFile(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*stdtime.Second)
	defer cancel()
	if _, err := runTimedDemo(ctx, timedConfig{
		FederationName: "timed-log",
		Ticks:          3,
		LogDir:         dir,
		Deterministic:  true,
	}); err != nil {
		t.Fatalf("runTimedDemo: %v", err)
	}
	logPath := eventlog.GenerationLogPath(dir, "timed-log", 0)
	rdr, err := openTestReader(t, logPath)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer func() { _ = rdr.Close() }()
	if rdr.Header().Federation != "timed-log" {
		t.Errorf("header federation = %q, want timed-log", rdr.Header().Federation)
	}
}

// TestTimedDemo_RejectsInvalidConfig.
func TestTimedDemo_RejectsInvalidConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  timedConfig
	}{
		{"zero ticks", timedConfig{FederationName: "x", Ticks: 0}},
		{"negative ticks", timedConfig{FederationName: "x", Ticks: -1}},
		{"empty federation", timedConfig{FederationName: "", Ticks: 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runTimedDemo(context.Background(), tc.cfg)
			if err == nil {
				t.Errorf("got nil error, want config rejection")
			}
		})
	}
}

// TestTimedDemo_DeterministicRunsByteIdentical: two consecutive runs of
// runTimedDemo with the same config produce a byte-identical event log
// body. Validates the runner's internal determinism before the
// example-level harness exercises it through subprocess.
func TestTimedDemo_DeterministicRunsByteIdentical(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*stdtime.Second)
	defer cancel()
	dirA := t.TempDir()
	dirB := t.TempDir()
	cfg := timedConfig{
		FederationName: "timed-det",
		Ticks:          8,
		Deterministic:  true,
	}
	cfg.LogDir = dirA
	if _, err := runTimedDemo(ctx, cfg); err != nil {
		t.Fatalf("run A: %v", err)
	}
	cfg.LogDir = dirB
	if _, err := runTimedDemo(ctx, cfg); err != nil {
		t.Fatalf("run B: %v", err)
	}
	a, err := readFileBytes(eventlog.GenerationLogPath(dirA, "timed-det", 0))
	if err != nil {
		t.Fatalf("read A: %v", err)
	}
	b, err := readFileBytes(eventlog.GenerationLogPath(dirB, "timed-det", 0))
	if err != nil {
		t.Fatalf("read B: %v", err)
	}
	// Strip the header (CreatedAtNs is wall-stamped from FakeClock and
	// therefore equal here, but eventLogHeaderSize-style stripping
	// matches the example-level harness.)
	const headerSize = 64
	if len(a) < headerSize || len(b) < headerSize {
		t.Fatalf("logs too short: lenA=%d lenB=%d", len(a), len(b))
	}
	if !equalBytes(a[headerSize:], b[headerSize:]) {
		t.Errorf("log bodies differ: lenA=%d lenB=%d", len(a)-headerSize, len(b)-headerSize)
	}
}

// TestTimedDemo_StallHaltsFederation: with SkipFederateIndex set so one
// federate never NERs, advancing the FakeClock past StallTimeout and
// invoking CheckStalls produces at least one FederationHalted event.
//
// Validates the runtime's AdvanceClockAndCheckStalls helper which the
// stall harness uses.
func TestTimedDemo_StallHaltsFederation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*stdtime.Second)
	defer cancel()
	clk := core.NewFakeClock(stdtime.Unix(0, 0))
	rt, cleanup, err := buildTimedRuntime(timedConfig{
		FederationName: "timed-stall",
		Ticks:          1,
		Lookaheads:     []core.LogicalTime{1.0, 1.0},
		StallTimeout:   2 * stdtime.Second,
		Clock:          clk,
	})
	if err != nil {
		t.Fatalf("buildTimedRuntime: %v", err)
	}
	defer cleanup()
	// Manually drive: enable both, NER fed1 only, leave fed2 silent.
	if err := rt.tm.EnableRegulation(ctx, "timed-stall", 1, core.LogicalTime(1.0)); err != nil {
		t.Fatalf("Enable fed1: %v", err)
	}
	if err := rt.tm.EnableRegulation(ctx, "timed-stall", 2, core.LogicalTime(1.0)); err != nil {
		t.Fatalf("Enable fed2: %v", err)
	}
	if err := rt.tm.NextMessageRequest(ctx, "timed-stall", 1, core.LogicalTime(10.0)); err != nil {
		t.Fatalf("NER fed1: %v", err)
	}
	halted := rt.AdvanceClockAndCheckStalls(ctx, 10*stdtime.Second)
	if halted < 1 {
		t.Errorf("CheckStalls past timeout: halted = %d, want ≥1", halted)
	}
	_, halts := rt.outbox.Counts()
	if halts == 0 {
		t.Errorf("recordingOutbox.halts = 0, want ≥1 FederationHalted event")
	}
}

// TestRecordingOutbox_ClassifiesEvents: the recording outbox correctly
// counts grant vs halt events and exposes Sent/SentTo for assertions.
func TestRecordingOutbox_ClassifiesEvents(t *testing.T) {
	ob := newRecordingOutbox()
	ctx := context.Background()
	if err := ob.Send(ctx, "fed", 1, &fakeOutboundEvent{seq: 1}); err != nil {
		t.Fatalf("Send unknown: %v", err)
	}
	g, h := ob.Counts()
	if g != 0 || h != 0 {
		t.Errorf("unknown event counted: grants=%d halts=%d, want 0/0", g, h)
	}
	if got := len(ob.Sent()); got != 1 {
		t.Errorf("Sent length = %d, want 1", got)
	}
	if got := len(ob.SentTo("fed", 1)); got != 1 {
		t.Errorf("SentTo length = %d, want 1", got)
	}
	if got := len(ob.SentTo("fed", 2)); got != 0 {
		t.Errorf("SentTo other handle length = %d, want 0", got)
	}
}

// TestTimedDemo_ContextCancellation: a canceled context aborts the run
// loop with ctx.Err.
func TestTimedDemo_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runTimedDemo(ctx, timedConfig{
		FederationName: "timed-ctx",
		Ticks:          1000,
		Deterministic:  true,
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want ctx.Canceled", err)
	}
}
