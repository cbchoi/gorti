package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestPingpong_Smoke runs the example end-to-end with a small round
// budget. The full <5s budget is exercised by TestPingpong_FullBudget;
// this smoke just confirms the wiring (subprocess + rtid pingpong-demo
// + on-disk log file) is intact.
func TestPingpong_Smoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go-pingpong smoke in -short mode")
	}
	bin := buildRtidOnce(t)
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stats, err := runExample(ctx, exampleArgs{
		FederationName: "pingpong-smoke",
		Rounds:         50,
		LogDir:         dir,
		RtidBinary:     bin,
	})
	if err != nil {
		t.Fatalf("runExample: %v", err)
	}
	if stats.Rounds != 50 {
		t.Errorf("rounds = %d, want 50", stats.Rounds)
	}
	logPath := filepath.Join(
		dir,
		hex.EncodeToString([]byte("pingpong-smoke")),
		fmt.Sprintf("%016x.log", uint64(0)),
	)
	st, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if st.Size() == 0 {
		t.Errorf("log file is empty; expected header + records")
	}
}

// TestPingpong_FullBudget runs the full 1000-round example and asserts
// the runtime is under the 5s budget set by srs.md §10.2 M2 exit
// criterion 1.
func TestPingpong_FullBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go-pingpong full-budget in -short mode")
	}
	bin := buildRtidOnce(t)
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	stats, err := runExample(ctx, exampleArgs{
		FederationName: "pingpong-full",
		Rounds:         1000,
		LogDir:         dir,
		RtidBinary:     bin,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("runExample: %v", err)
	}
	if stats.Rounds != 1000 {
		t.Errorf("rounds = %d, want 1000", stats.Rounds)
	}
	if elapsed > 5*time.Second {
		t.Errorf("pingpong subprocess took %v; M2 budget is 5s (includes any go-build overhead)", elapsed)
	}
}

// buildRtidOnce compiles the rtid binary into a temp dir on first call
// and returns the path. Subsequent calls reuse the same binary so each
// test in the package doesn't pay the build cost. Compiling once is
// also necessary for the determinism harness to compare bytes — the
// binary must be identical across the 10 runs.
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
