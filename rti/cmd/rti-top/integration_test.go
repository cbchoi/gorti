// Integration smoke test — spawn `rtid` (the daemon binary) as a
// subprocess, wait for it to bind its admin listener, dial it from
// rti-top's client package, fetch Snapshot + Status, and assert the
// model's renderHeader / renderFederationsView reflect the live
// (empty) state.
//
// We do NOT drive a full bubbletea Program loop here — that's brittle
// against terminal-detection on CI. The test exercises everything
// from the dial-edge through render, leaving only `tea.Program.Run`
// (which is exhaustively covered by bubbletea's own tests) untested.
//
// Skips when `go build ./rti/cmd/rtid` is unavailable from the
// working directory (i.e. running tests outside the repo) or when
// the smoke port is already in use.

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/cmd/rti-top/internal/client"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// TestIntegration_SpawnRtidAndRenderEmptySnapshot is the after-final-
// commit harness specified in the Phase-2 plan. Builds rtid, runs it
// with a free admin port, dials AdminService from rti-top's client,
// verifies Snapshot returns 0 federations (no joiners), and feeds
// the response through model rendering to assert the header + body
// strings are populated.
func TestIntegration_SpawnRtidAndRenderEmptySnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("rtid build path not yet validated on windows")
	}

	repoRoot := repoRoot(t)
	binDir := t.TempDir()
	rtidBin := filepath.Join(binDir, "rtid")

	build := exec.Command("go", "build", "-o", rtidBin, "./rti/cmd/rtid")
	build.Dir = repoRoot
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("could not build rtid for integration test (%v): %s", err, out)
	}

	listenPort, adminPort := freePort(t), freePort(t)
	listenAddr := fmt.Sprintf("127.0.0.1:%d", listenPort)
	adminAddr := fmt.Sprintf("127.0.0.1:%d", adminPort)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, rtidBin,
		"--listen", listenAddr,
		"--admin-listen", adminAddr,
		"--metrics-listen", "127.0.0.1:0",
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start rtid: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// Poll the admin port until it accepts a connection.
	if !waitForPort(adminAddr, 5*time.Second) {
		t.Fatalf("rtid admin listener never bound %s", adminAddr)
	}

	cli, err := client.Dial(adminAddr)
	if err != nil {
		t.Fatalf("client.Dial: %v", err)
	}
	defer cli.Close()

	st, err := cli.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.GetRtidVersion() == "" {
		t.Errorf("Status.RtidVersion empty")
	}

	resp, err := cli.Snapshot(ctx, "", 3*time.Second)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(resp.GetFederations()) != 0 {
		t.Errorf("expected 0 federations on a fresh rtid; got %d", len(resp.GetFederations()))
	}

	// Drive the model with the live snapshot and verify rendering.
	m := initialModel(ctx, cli, st, 1*time.Second)
	m.last = resp
	m.width = 100
	m.height = 30

	header := m.renderHeader()
	if !strings.Contains(header, "gorti rtid") {
		t.Errorf("header missing version banner: %q", header)
	}
	body := m.renderFederationsView()
	if !strings.Contains(body, "no federations") {
		t.Errorf("empty body should mention 'no federations'; got %q", body)
	}
}

// repoRoot walks up from the package directory until it finds the
// go.mod, returning that path.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("could not find repo root from cwd")
	return ""
}

// freePort returns a port number that's unused at the time of the
// call. There's a TOCTOU window between this returning and the
// caller binding it; rtid handles bind-failure by exiting, which the
// test detects via waitForPort.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// waitForPort returns true once a TCP connection succeeds within d.
func waitForPort(addr string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// silence unused-import warnings when the test is short-circuited.
var _ = rtiv1.WireVersion_WIRE_VERSION_V1
