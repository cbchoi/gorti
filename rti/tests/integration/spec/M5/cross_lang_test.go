package m5spec

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestSpec_M5_CrossLanguage_PythonFederateJoinsGoFederation runs the existing
// independent-process fixture in both directions. It checks distinct process
// IDs/handles, exact payloads, TSO-before-grant order, blocked early delivery,
// logical-time agreement, and successful participant shutdown against rtid.
//
// Implements: M5 cross-language end-to-end goal; TASK-081 contract.
func TestSpec_M5_CrossLanguage_PythonFederateJoinsGoFederation(t *testing.T) {
	python := os.Getenv("GORTI_PYTHON")
	if python == "" {
		for _, candidate := range []string{"python3", "python"} {
			if path, err := exec.LookPath(candidate); err == nil {
				python = path
				break
			}
		}
	}
	if python == "" {
		t.Fatal("live M5 test requires Python and pysdk dependencies; set GORTI_PYTHON to that interpreter")
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate M5 fixture")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../.."))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	const script = `
from tests.spec.m21._processes import assert_cross_language_delivery
assert_cross_language_delivery("go", "python")
assert_cross_language_delivery("python", "go")
print("M5 independent Go/Python delivery passed in both directions", flush=True)
`
	cmd := exec.CommandContext(ctx, python, "-B", "-c", script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "pysdk"),
		"PYTHONDONTWRITEBYTECODE=1", "PYTHONUNBUFFERED=1", "PYTHONOPTIMIZE=", "GORTI_RESEARCH_CONFIG=")
	// Normal fixture failures unwind its ExitStack. A hard deadline must also
	// stop only this invocation's child tree, not unrelated test servers.
	cmd.Cancel = func() error {
		if runtime.GOOS == "windows" {
			killCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			return exec.CommandContext(killCtx, "taskkill", "/PID", fmt.Sprint(cmd.Process.Pid), "/T", "/F").Run()
		}
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 15 * time.Second
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("live M5 Go/Python fixture (%s): %v\n%s", python, err, output)
	}
	if !strings.Contains(string(output), "M5 independent Go/Python delivery passed in both directions") {
		t.Fatalf("fixture ended without completing both directions:\n%s", output)
	}
	t.Logf("%s", output)
}
