package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/eventlog"
)

// TestPingpongDemo_RunsToCompletion: a small in-process run completes
// without error and reports all rounds done.
func TestPingpongDemo_RunsToCompletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stats, err := runPingpongDemo(ctx, pingpongConfig{
		FederationName: "demo-test",
		Rounds:         25,
		Deterministic:  true,
	})
	if err != nil {
		t.Fatalf("runPingpongDemo: %v", err)
	}
	if stats.RoundsCompleted != 25 {
		t.Errorf("RoundsCompleted = %d, want 25", stats.RoundsCompleted)
	}
}

// TestPingpongDemo_WritesLogFile: with --log-dir set, the demo persists
// a per-federation log file under the dir.
func TestPingpongDemo_WritesLogFile(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := runPingpongDemo(ctx, pingpongConfig{
		FederationName: "demo-log",
		Rounds:         5,
		LogDir:         dir,
		Deterministic:  true,
	}); err != nil {
		t.Fatalf("runPingpongDemo: %v", err)
	}
	logPath := eventlog.GenerationLogPath(dir, "demo-log", 0)
	rdr, err := openTestReader(t, logPath)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer func() { _ = rdr.Close() }()
	if rdr.Header().Federation != "demo-log" {
		t.Errorf("header federation = %q, want demo-log", rdr.Header().Federation)
	}
}

// TestPingpongDemo_RejectsInvalidConfig.
func TestPingpongDemo_RejectsInvalidConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  pingpongConfig
	}{
		{"zero rounds", pingpongConfig{FederationName: "x", Rounds: 0}},
		{"negative rounds", pingpongConfig{FederationName: "x", Rounds: -1}},
		{"empty federation", pingpongConfig{FederationName: "", Rounds: 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runPingpongDemo(context.Background(), tc.cfg)
			if err == nil {
				t.Errorf("got nil error, want config rejection")
			}
		})
	}
}

// TestRunReplayFromFile_ReproducesSourceBytes: a deterministic
// pingpong run + replay produces a byte-identical second log.
func TestRunReplayFromFile_ReproducesSourceBytes(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := runPingpongDemo(ctx, pingpongConfig{
		FederationName: "replay-unit",
		Rounds:         10,
		LogDir:         srcDir,
		Deterministic:  true,
	}); err != nil {
		t.Fatalf("source run: %v", err)
	}

	srcPath := eventlog.GenerationLogPath(srcDir, "replay-unit", 0)
	if err := runReplayFromFile(ctx, srcPath, dstDir); err != nil {
		t.Fatalf("runReplayFromFile: %v", err)
	}

	dstPath := eventlog.GenerationLogPath(dstDir, "replay-unit", 0)
	srcBody, err := readFileBytes(srcPath)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	dstBody, err := readFileBytes(dstPath)
	if err != nil {
		t.Fatalf("read replay: %v", err)
	}
	if !equalBytes(srcBody, dstBody) {
		t.Errorf("replay bytes differ from source (lens %d vs %d)", len(srcBody), len(dstBody))
	}
}

// TestRunReplayFromFile_RejectsMissingInput.
func TestRunReplayFromFile_RejectsMissingInput(t *testing.T) {
	dst := t.TempDir()
	err := runReplayFromFile(context.Background(), filepath.Join(t.TempDir(), "no-such-file.log"), dst)
	if err == nil {
		t.Errorf("got nil error opening missing input")
	}
}

// TestBuildLogger_KnownLevels.
func TestBuildLogger_KnownLevels(t *testing.T) {
	for _, lvl := range []string{"debug", "info", "warn", "error", "unknown"} {
		for _, fmt := range []string{"json", "text"} {
			lg := buildLogger(lvl, fmt)
			if lg == nil {
				t.Errorf("buildLogger(%s,%s) = nil", lvl, fmt)
			}
		}
	}
}

// TestNewMultiplexLog_DiscardWhenEmptyDir: with no dir, the writer
// uses the discard sink.
func TestNewMultiplexLog_DiscardWhenEmptyDir(t *testing.T) {
	mw, err := newMultiplexLog("", testRealClock())
	if err != nil {
		t.Fatalf("newMultiplexLog: %v", err)
	}
	defer func() { _ = mw.Close() }()
}

// TestNewMultiplexLog_FileBackingWhenDir: with a dir, the writer
// produces a file per federation.
func TestNewMultiplexLog_FileBackingWhenDir(t *testing.T) {
	dir := t.TempDir()
	mw, err := newMultiplexLog(dir, testRealClock())
	if err != nil {
		t.Fatalf("newMultiplexLog: %v", err)
	}
	defer func() { _ = mw.Close() }()
}

// TestZeroCounters: the zero-stub counters are stable returns.
func TestZeroCounters(t *testing.T) {
	if (zeroObjectCounter{}).ObjectCount("x") != 0 {
		t.Errorf("zeroObjectCounter.ObjectCount = nonzero")
	}
	if (zeroSeqSource{}).EventLogSeq("x") != 0 {
		t.Errorf("zeroSeqSource.EventLogSeq = nonzero")
	}
}

// TestPingpongFOMRepo_Permissive: the permissive FOM repo returns valid
// handles for any name.
func TestPingpongFOMRepo_Permissive(t *testing.T) {
	r := newPingpongFOMRepo()
	h, err := r.Load(context.Background(), nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !h.IsValid() {
		t.Errorf("handle.IsValid() = false")
	}
	if _, ok := h.LookupObjectClass("Anything"); !ok {
		t.Errorf("LookupObjectClass: not found")
	}
	if _, ok := h.LookupInteractionClass("Anything"); !ok {
		t.Errorf("LookupInteractionClass: not found")
	}
	if _, ok := h.LookupAttribute(1, "x"); !ok {
		t.Errorf("LookupAttribute: not found")
	}
	if _, ok := h.LookupParameter(1, "x"); !ok {
		t.Errorf("LookupParameter: not found")
	}
	got, err := r.Get(context.Background(), "any-fed")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.IsValid() {
		t.Errorf("Get handle.IsValid() = false")
	}
}

// TestSyncOutbox_DropsWithoutSubscriber.
func TestSyncOutbox_DropsWithoutSubscriber(t *testing.T) {
	o := newSyncOutbox()
	if err := o.Send(context.Background(), "fed", 1, &fakeOutboundEvent{seq: 1}); err != nil {
		t.Errorf("Send no-subscriber: %v", err)
	}
}

// TestSyncOutbox_DoubleSubscribeRejected.
func TestSyncOutbox_DoubleSubscribeRejected(t *testing.T) {
	o := newSyncOutbox()
	_, cancel, err := o.Subscribe(context.Background(), "fed", 1)
	if err != nil {
		t.Fatalf("first subscribe: %v", err)
	}
	defer func() { _ = cancel() }()
	if _, _, err := o.Subscribe(context.Background(), "fed", 1); err == nil {
		t.Errorf("second subscribe to (fed, 1) returned nil error")
	}
}
