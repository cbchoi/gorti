package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestTimed_Smoke runs the example end-to-end with a small tick budget.
// The full 100-tick budget is exercised by TestTimed_FullBudget; this
// smoke just confirms the wiring (subprocess + rtid timed-demo + on-
// disk log file) is intact.
func TestTimed_Smoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go-timed smoke in -short mode")
	}
	bin := buildRtidOnce(t)
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stats, err := runExample(ctx, exampleArgs{
		FederationName: "timed-smoke",
		Ticks:          20,
		LogDir:         dir,
		RtidBinary:     bin,
	})
	if err != nil {
		t.Fatalf("runExample: %v", err)
	}
	if stats.Ticks != 20 {
		t.Errorf("ticks = %d, want 20", stats.Ticks)
	}
	logPath := filepath.Join(dir, "timed-smoke.log")
	st, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if st.Size() == 0 {
		t.Errorf("log file is empty; expected header + records")
	}
}

// TestTimed_FullBudget runs the full 100-tick example and asserts the
// runtime is under a generous 10s budget. The M3 brief sets 100 ticks
// as the reference workload (3 federates over 100 logical ticks).
func TestTimed_FullBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go-timed full-budget in -short mode")
	}
	bin := buildRtidOnce(t)
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	stats, err := runExample(ctx, exampleArgs{
		FederationName: "timed-full",
		Ticks:          100,
		LogDir:         dir,
		RtidBinary:     bin,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("runExample: %v", err)
	}
	if stats.Ticks != 100 {
		t.Errorf("ticks = %d, want 100", stats.Ticks)
	}
	if elapsed > 10*time.Second {
		t.Errorf("timed subprocess took %v; budget is 10s", elapsed)
	}
}

// buildRtidOnce compiles the rtid binary into a temp dir on first call
// and returns the path. Subsequent calls reuse the same binary so each
// test in the package doesn't pay the build cost. Compiling once is
// also necessary for the determinism harness to compare bytes — the
// binary must be identical across the runs.
var (
	rtidBinOnce sync.Once
	rtidBinPath string
	rtidBinErr  error
)

func buildRtidOnce(t *testing.T) string {
	t.Helper()
	rtidBinOnce.Do(func() {
		root, err := repoRoot()
		if err != nil {
			rtidBinErr = err
			return
		}
		dir, err := os.MkdirTemp("", "rtid-bin-*")
		if err != nil {
			rtidBinErr = err
			return
		}
		bin := filepath.Join(dir, "rtid")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", bin, "./rti/cmd/rtid")
		cmd.Dir = root
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		if err := cmd.Run(); err != nil {
			rtidBinErr = err
			return
		}
		rtidBinPath = bin
	})
	if rtidBinErr != nil {
		t.Fatalf("buildRtidOnce: %v", rtidBinErr)
	}
	return rtidBinPath
}

// repoRoot walks up from the test file's directory until it finds the
// directory containing go.mod. Tests run from anywhere in the tree, so
// we cannot assume a fixed cwd.
func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
