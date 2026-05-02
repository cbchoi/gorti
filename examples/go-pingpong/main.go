// Command go-pingpong runs the M2 reference example: two in-process
// federates exchange 1000 interactions through a single rtid. See
// docs/srs.md §10.2 M2 exit criterion 1 for the runtime budget (<5s).
//
// Architecture: the actual federation/declaration/object/eventlog
// stack and the two federate goroutines all live inside the rtid
// binary (see rti/cmd/rtid/pingpong.go). The Go internal-package rule
// prevents the example from importing rti/internal/* directly, so
// this main is a thin shim that spawns "rtid -mode=pingpong-demo".
//
// Run:
//
//	go run ./examples/go-pingpong
//
// Flags:
//
//	-rounds         number of ping<->pong round-trips (default 1000)
//	-federation     federation name (default "pingpong")
//	-log-dir        directory for the per-federation event log file
//	                (default empty = no persistence)
//	-rtid-bin       path to the rtid binary (default: build via go run)
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
	rounds := flag.Int("rounds", 1000, "number of ping->pong round-trips")
	federation := flag.String("federation", "pingpong", "federation name")
	logDir := flag.String("log-dir", "", "directory for per-federation event log files")
	rtidBin := flag.String("rtid-bin", "", "path to a prebuilt rtid binary; empty uses 'go run ./rti/cmd/rtid'")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stats, err := runExample(ctx, exampleArgs{
		FederationName: *federation,
		Rounds:         *rounds,
		LogDir:         *logDir,
		RtidBinary:     *rtidBin,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "go-pingpong: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "go-pingpong: %d rounds in %v\n", stats.Rounds, stats.Elapsed)
}

// exampleArgs configures runExample. Extracted so tests can drive the
// subprocess without touching the flag set.
type exampleArgs struct {
	FederationName string
	Rounds         int
	LogDir         string
	// Deterministic forces rtid to use a FakeClock so the captured
	// event-log body is byte-identical across runs. The determinism
	// harness sets this; production runs leave it false.
	Deterministic bool
	// RtidBinary, when set, points at a pre-built rtid binary on disk.
	// When empty, the example invokes "go run ./rti/cmd/rtid" relative
	// to the module root (which the test discovers via repoRoot()).
	RtidBinary string
}

// exampleStats summarizes a successful run.
type exampleStats struct {
	Rounds  int
	Elapsed time.Duration
}

// runExample shells out to the rtid binary in pingpong-demo mode. The
// rtid process is responsible for the in-process federate exchange and
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
		return exampleStats{}, fmt.Errorf("rtid pingpong-demo: %w", err)
	}
	return exampleStats{
		Rounds:  args.Rounds,
		Elapsed: time.Since(start),
	}, nil
}

// rtidCommand builds the (path, args) tuple for invoking rtid. When
// args.RtidBinary is set, the binary at that path is invoked directly;
// otherwise the test/example uses "go run ./rti/cmd/rtid" so a fresh
// build is implicit.
func rtidCommand(args exampleArgs) (string, []string, error) {
	common := []string{
		"-mode=pingpong-demo",
		"-pingpong-rounds=" + strconv.Itoa(args.Rounds),
		"-pingpong-federation=" + args.FederationName,
		"-log-format=text",
	}
	if args.LogDir != "" {
		common = append(common, "-log-dir="+args.LogDir)
	}
	if args.Deterministic {
		common = append(common, "-pingpong-deterministic")
	}
	if args.RtidBinary != "" {
		return args.RtidBinary, common, nil
	}
	return "go", append([]string{"run", "./rti/cmd/rtid"}, common...), nil
}
