//go:build perf

// perf_main.go — thin TASK-080 shim that delegates to the real
// perf-baseline binary (rti/cmd/perf-baseline). Mirrors main.go's
// "exec the rtid binary" pattern: examples/* cannot import
// rti/internal/* under Go's internal-package rule, so this file does
// nothing more than spawn the perf-baseline binary and forward stdout.
//
// Build:
//
//	go build -tags=perf -o /tmp/perf-baseline-shim ./examples/go-pingpong
//
// Or (one-liner):
//
//	go run -tags=perf ./examples/go-pingpong > docs/reports/M5/perf-baseline.json
//
// This shim is intentionally tiny — the production runner lives at
// rti/cmd/perf-baseline. Documented in engineering/verification/performance-milestones.md.

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
)

// init replaces the default `examples/go-pingpong` main when the perf
// tag is set: instead of running the rtid pingpong demo, dispatch to
// the perf-baseline binary. The Go build system links this file ONLY
// when -tags=perf is supplied, so the default build path is
// unaffected.
func init() {
	// Explicit os.Args manipulation: drop `flag`'s normal parsing
	// (main.go's flag.Parse runs first under the default tag, but
	// when the perf tag is set we want to pass flags through to the
	// child binary verbatim). The simplest reliable approach is to
	// shell out via `go run`, mirroring main.go's rtid spawn.
	args := []string{"run", "-tags=perf", "./rti/cmd/perf-baseline"}
	args = append(args, os.Args[1:]...)
	cmd := exec.Command("go", args...) //nolint:gosec // perf runner is dev-only
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "perf-baseline shim: %v\n", err)
		os.Exit(1)
	}
	// Suppress the default main from also running once init returns.
	os.Exit(0)
}

// Reference flag so the package compiles cleanly even when the real
// main.go is excluded by some other build tag combo.
var _ = flag.CommandLine
