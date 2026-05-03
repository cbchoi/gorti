// Command go-timed runs the M3 reference example: three regulating
// federates with lookaheads {1.0, 2.0, 0.5} advance via NER over a
// configurable tick budget. See docs/srs.md §10.2 M3 exit criterion 1.
//
// Architecture: the actual time-management stack and the federate
// goroutines all live inside the rtid binary (see
// rti/cmd/rtid/timed.go). The Go internal-package rule prevents the
// example from importing rti/internal/* directly, so this main is a
// thin shim that spawns "rtid -mode=timed-demo".
//
// Run:
//
//	go run ./examples/go-timed
//
// Flags:
//
//	-ticks       per-federate NER count (default 100)
//	-federation  federation name (default "timed")
//	-log-dir     directory for the per-federation event log file
//	             (default empty = no persistence)
//	-rtid-bin    path to the rtid binary (default: build via go run)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func main() {
	ticks := flag.Int("ticks", 100, "per-federate NER count")
	federation := flag.String("federation", "timed", "federation name")
	logDir := flag.String("log-dir", "", "directory for per-federation event log files")
	rtidBin := flag.String("rtid-bin", "", "path to a prebuilt rtid binary; empty uses 'go run ./rti/cmd/rtid'")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stats, err := runExample(ctx, exampleArgs{
		FederationName: *federation,
		Ticks:          *ticks,
		LogDir:         *logDir,
		RtidBinary:     *rtidBin,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "go-timed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "go-timed: %d ticks in %v\n", stats.Ticks, stats.Elapsed)
}

// exampleArgs configures runExample. Extracted so tests can drive the
// subprocess without touching the flag set.
type exampleArgs struct {
	FederationName string
	Ticks          int
	LogDir         string
	// Deterministic forces rtid to use a FakeClock so the captured
	// event-log body is byte-identical across runs. The determinism
	// harness sets this; production runs leave it false.
	Deterministic bool
	// RtidBinary, when set, points at a pre-built rtid binary on
	// disk. When empty, the example invokes "go run ./rti/cmd/rtid"
	// relative to the module root (which the test discovers via
	// repoRoot()).
	RtidBinary string
}

// exampleStats summarizes a successful run.
type exampleStats struct {
	Ticks   int
	Elapsed time.Duration
}

// runExample shells out to the rtid binary in timed-demo mode. The rtid
// process is responsible for the in-process federate exchange and
// returns 0 on success.
func runExample(ctx context.Context, args exampleArgs) (exampleStats, error) {
	cmdLine, cmdArgs, err := rtidCommand(args)
	if err != nil {
		return exampleStats{}, err
	}
	cmd := exec.CommandContext(ctx, cmdLine, cmdArgs...) //nolint:gosec // controlled args
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	start := time.Now()
	if err := cmd.Run(); err != nil {
		return exampleStats{}, fmt.Errorf("rtid timed-demo: %w", err)
	}
	return exampleStats{
		Ticks:   args.Ticks,
		Elapsed: time.Since(start),
	}, nil
}

// rtidCommand builds the (path, args) tuple for invoking rtid. When
// args.RtidBinary is set, the binary at that path is invoked directly;
// otherwise the test/example uses "go run ./rti/cmd/rtid" so a fresh
// build is implicit.
func rtidCommand(args exampleArgs) (string, []string, error) {
	common := []string{
		"-mode=timed-demo",
		"-timed-ticks=" + strconv.Itoa(args.Ticks),
		"-timed-federation=" + args.FederationName,
		"-log-format=text",
	}
	if args.LogDir != "" {
		common = append(common, "-log-dir="+args.LogDir)
	}
	if args.Deterministic {
		common = append(common, "-timed-deterministic")
	}
	if args.RtidBinary != "" {
		return args.RtidBinary, common, nil
	}
	return "go", append([]string{"run", "./rti/cmd/rtid"}, common...), nil
}

// replayArgs configures runReplay: drive an existing event-log file
// through rtid -mode=replay-from-log and capture the new stream into
// outputDir.
type replayArgs struct {
	InputLogPath string
	OutputDir    string
	RtidBinary   string
}

// runReplay shells out to "rtid -mode=replay-from-log" so the replay
// path goes through the production eventlog.NewReplayer. Returns when
// the subprocess exits.
func runReplay(ctx context.Context, args replayArgs) error {
	rtidArgs := []string{
		"-mode=replay-from-log",
		"-replay-input=" + args.InputLogPath,
		"-log-dir=" + args.OutputDir,
		"-log-format=text",
	}
	var name string
	var fullArgs []string
	if args.RtidBinary != "" {
		name = args.RtidBinary
		fullArgs = rtidArgs
	} else {
		name = "go"
		fullArgs = append([]string{"run", "./rti/cmd/rtid"}, rtidArgs...)
	}
	cmd := exec.CommandContext(ctx, name, fullArgs...) //nolint:gosec // controlled args
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd.Run()
}
