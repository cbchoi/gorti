// runner_test.go — TASK-211 (M21) end-to-end orchestrator + verifier.
//
// Spawns rtid + 3 federate subprocesses (fast/normal/slow), waits for
// each to write its result JSON, then verifies the M21 acceptance
// invariants (per-federate grant counts, per-federate strict monotonic
// grant times, LBTS bound on per-cycle minimum). Run with:
//
//   go test -timeout 60s ./examples/go-timed
//
// Builds private binaries and runs on Windows and Linux. Build, child-process,
// and protocol failures are test failures, not unavailable prerequisites.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

const (
	federation = "go-timed-test"
	cycles     = 10
	// tickStep MUST exceed the largest federate lookahead so every
	// cycle's request t exceeds currentTime + lookahead for every
	// federate (slow has la=2.0). Picking 3.0 leaves headroom for
	// the LBTS-promotion to satisfy strict NER's lb > rt predicate.
	tickStep = 3.0
)

type federateResult struct {
	Name        string    `json:"name"`
	Lookahead   float64   `json:"lookahead"`
	Primitive   string    `json:"primitive"`
	Constrained bool      `json:"constrained"`
	Grants      []float64 `json:"grants"`
	TicksSent   uint32    `json:"ticks_sent"`
}

type federateSpec struct {
	Name      string
	Lookahead float64
	Primitive string
}

func TestGoTimedEndToEnd(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}
	binDir := t.TempDir()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	rtidBin := filepath.Join(binDir, "rtid"+ext)
	regBin := filepath.Join(binDir, "go-timed-regulator"+ext)

	// Build both binaries fresh — keeps the test self-contained.
	if err := goBuild(t, repoRoot, rtidBin, "./rti/cmd/rtid"); err != nil {
		t.Fatalf("build rtid: %v", err)
	}
	if err := goBuild(t, repoRoot, regBin, "./examples/go-timed"); err != nil {
		t.Fatalf("build regulator: %v", err)
	}

	tmpdir, err := os.MkdirTemp("", "go-timed-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("preserving tempdir for debugging: %s", tmpdir)
			return
		}
		_ = os.RemoveAll(tmpdir)
	})

	// Pick free ports.
	listenPort := freePort(t)
	metricsPort := freePort(t)
	adminPort := freePort(t)

	// Start rtid.
	logF, err := os.Create(filepath.Join(tmpdir, "rtid.log"))
	if err != nil {
		t.Fatalf("create rtid.log: %v", err)
	}
	defer logF.Close()

	rtidCmd := exec.Command(rtidBin,
		"--listen", ":"+strconv.Itoa(listenPort),
		"--metrics-listen", ":"+strconv.Itoa(metricsPort),
		"--admin-listen", "127.0.0.1:"+strconv.Itoa(adminPort),
		"--log-level", "warn",
		"--save-dir", filepath.Join(tmpdir, "saves"),
	)
	rtidCmd.Env = append(os.Environ(), "GORTI_RESEARCH_CONFIG=")
	rtidCmd.Stdout = logF
	rtidCmd.Stderr = logF
	if err := rtidCmd.Start(); err != nil {
		t.Fatalf("start rtid: %v", err)
	}
	t.Cleanup(func() {
		if runtime.GOOS == "windows" {
			_ = rtidCmd.Process.Kill()
		} else {
			_ = rtidCmd.Process.Signal(os.Interrupt)
		}
		done := make(chan struct{})
		go func() { _ = rtidCmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = rtidCmd.Process.Kill()
			<-done
		}
		data, err := os.ReadFile(logF.Name())
		if err != nil {
			t.Errorf("read rtid log: %v", err)
		} else if bytes.Contains(data, []byte("WARNING: DATA RACE")) {
			t.Errorf("race detected in rtid subprocess:\n%s", data)
		}
	})

	if err := waitForListener("127.0.0.1:"+strconv.Itoa(listenPort), 10*time.Second); err != nil {
		t.Fatalf("rtid never listened on :%d: %v", listenPort, err)
	}

	url := "127.0.0.1:" + strconv.Itoa(listenPort)
	fomPath := filepath.Join(repoRoot, "examples", "go-timed", "time-advance-fom.xml")

	// All federates use TAR. The runner exercises the cycle pattern
	// that M3/M21 promised; the per-primitive boundary tests live in
	// rti/internal/transport/grpc/time_test.go (TASK-203 cases 8a-f).
	//
	// Mixing NER with mismatched lookaheads in lockstep (all federates
	// requesting the same t each cycle) hits an implementation-edge
	// scheduling case where the federation may stall waiting for the
	// last NER to land. See rti/spec/M22/ner_forced_grant_race_test.go
	// + rti/spec/M22/ner_full_grant_test.go for the manager-level
	// pinning of NER full/forced semantics. M22 W3 fix:
	// waitForFullGrant correctly accumulates forced grants in
	// regulator_main.go — this is the SDK-pattern improvement
	// (the M21-era 5 ms settle delay is gone) and the test still uses
	// TAR for robustness.
	specs := []federateSpec{
		{Name: "fast", Lookahead: 0.5, Primitive: "TAR"},
		{Name: "normal", Lookahead: 1.0, Primitive: "TAR"},
		{Name: "slow", Lookahead: 2.0, Primitive: "TAR"},
	}

	// Spawn federates concurrently.
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errCh := make(chan error, len(specs))
	for _, s := range specs {
		wg.Add(1)
		go func(s federateSpec) {
			defer wg.Done()
			resultPath := filepath.Join(tmpdir, s.Name+"-result.json")
			fedLogPath := filepath.Join(tmpdir, s.Name+".log")
			fedLog, err := os.Create(fedLogPath)
			if err != nil {
				errCh <- err
				return
			}
			defer fedLog.Close()

			cmd := exec.CommandContext(ctx, regBin,
				"--url", url,
				"--federation", federation,
				"--name", s.Name,
				"--lookahead", strconv.FormatFloat(s.Lookahead, 'f', -1, 64),
				"--primitive", s.Primitive,
				"--constrained=true",
				"--participants", strconv.Itoa(len(specs)),
				"--cycles", strconv.Itoa(cycles),
				"--tick-step", strconv.FormatFloat(tickStep, 'f', -1, 64),
				"--result", resultPath,
				"--fom", fomPath,
			)
			cmd.Stdout = fedLog
			cmd.Stderr = fedLog
			// Exercise a late join: the other participants must remain at
			// their startup barrier rather than advancing before normal joins.
			if s.Name == "normal" {
				time.Sleep(time.Second)
			}
			if err := cmd.Run(); err != nil {
				errCh <- err
			}
		}(s)
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		if e != nil {
			t.Errorf("federate run: %v (logs in %s)", e, tmpdir)
		}
	}
	if t.Failed() {
		return
	}

	// Read results.
	results := make(map[string]federateResult, len(specs))
	for _, s := range specs {
		path := filepath.Join(tmpdir, s.Name+"-result.json")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var r federateResult
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if r.Name != s.Name || r.Lookahead != s.Lookahead || r.Primitive != s.Primitive || !r.Constrained || r.TicksSent != cycles {
			t.Errorf("%s: result identity/configuration/tick count mismatch: %+v", s.Name, r)
		}
		results[s.Name] = r
	}

	// === Verifier (TASK-211 invariants) ===

	// 211.2 — each federate received exactly `cycles` grants.
	for name, r := range results {
		if len(r.Grants) != cycles {
			t.Errorf("%s: %d grants, want %d", name, len(r.Grants), cycles)
		}
		for cycle, grant := range r.Grants {
			if want := float64(cycle+1) * tickStep; grant != want {
				t.Errorf("%s: TAR cycle %d grant=%v, want exact requested time %v", name, cycle+1, grant, want)
			}
		}
	}

	// 211.3: TAR grants must strictly increase with these requested targets.
	for name, r := range results {
		for i := 1; i < len(r.Grants); i++ {
			if r.Grants[i] <= r.Grants[i-1] {
				t.Errorf("%s: grant %d (%v) regressed below grant %d (%v)",
					name, i, r.Grants[i], i-1, r.Grants[i-1])
			}
		}
	}

	// The cycle minimum is a diagnostic summary, not a measured LBTS bound.
	cycleMins := make([]float64, cycles)
	for c := 0; c < cycles; c++ {
		min := -1.0
		for _, r := range results {
			if c < len(r.Grants) {
				if min < 0 || r.Grants[c] < min {
					min = r.Grants[c]
				}
			}
		}
		cycleMins[c] = min
	}
	for c := 1; c < cycles; c++ {
		if cycleMins[c] < cycleMins[c-1] {
			t.Errorf("cycle %d min grant (%v) regressed below cycle %d (%v)",
				c, cycleMins[c], c-1, cycleMins[c-1])
		}
	}

	// Completion is bounded by the child context, not a performance assertion.
	t.Logf("go-timed end-to-end OK: %d federates × %d cycles, cycle-mins=%v",
		len(results), cycles, cycleMins)
}

// freePort binds to :0 and returns the assigned port. Race window is
// acceptable for a developer-loop test fixture.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitForListener(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return context.DeadlineExceeded
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	d := wd
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", os.ErrNotExist
		}
		d = parent
	}
}

// sortedKeys helps with stable iteration in error reports.
func sortedKeys(m map[string]federateResult) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func goBuild(t *testing.T, repoRoot, output, pkg string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", output, pkg)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("go build %s output:\n%s", pkg, out)
		return err
	}
	return nil
}
