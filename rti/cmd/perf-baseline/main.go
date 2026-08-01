//go:build perf

// Command perf-baseline is the standalone TASK-080 runner. Builds with
// the `perf` build tag (so the default `go build ./...` skips it) and
// invokes perf.Manager.RunBaseline at sizes 2 / 5 / 25 / 100 in
// sequence, encoding a JSON array of perf.Result to stdout.
//
// Run:
//
//	go build -tags=perf -o /tmp/perf-baseline ./rti/cmd/perf-baseline
//	/tmp/perf-baseline > docs/reports/M5/perf-baseline.json
//
// Or:
//
//	go run -tags=perf ./rti/cmd/perf-baseline > docs/reports/M5/perf-baseline.json
//
// Placement note: the performance contract calls for
// examples/go-pingpong/perf_main.go, but that location can't import
// rti/internal/perf under Go's internal-package rule. Hosting the
// runner at rti/cmd/perf-baseline keeps the same single-binary
// reproducibility while satisfying the import boundary.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"

	"github.com/cbchoi/gorti/rti/internal/perf"
)

func main() {
	durFlag := flag.Duration("duration", 10*time.Second, "per-size workload duration")
	onlyFlag := flag.String("only", "", "comma-separated subset of sizes to run (e.g. \"2,25\"); empty = all four")
	flag.Parse()

	sizes := []perf.FederationSize{perf.Size2, perf.Size5, perf.Size25, perf.Size100}
	if *onlyFlag != "" {
		sizes = parseOnly(*onlyFlag)
	}

	results := make([]perf.Result, 0, len(sizes))
	for _, sz := range sizes {
		mgr, err := perf.New(perf.Options{Size: sz, Duration: *durFlag})
		if err != nil {
			log.Fatalf("perf.New(size=%d): %v", sz, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), *durFlag+30*time.Second)
		res, err := mgr.RunBaseline(ctx)
		cancel()
		if err != nil {
			log.Fatalf("RunBaseline(size=%d): %v", sz, err)
		}
		results = append(results, res)
		log.Printf("size=%d sent=%d throughput=%.0f/s p50=%.3fms p99=%.3fms duration=%.2fs notes=%q",
			sz, res.InteractionsSent, res.ThroughputPerSecond,
			res.LatencyP50Ms, res.LatencyP99Ms, res.DurationSeconds, res.Notes)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		log.Fatalf("encode: %v", err)
	}
}

// parseOnly parses a comma-separated list of integer sizes into the
// matching perf.FederationSize values. Unknown sizes are silently
// skipped (so a typo doesn't kill the run).
func parseOnly(spec string) []perf.FederationSize {
	want := map[int]perf.FederationSize{
		2:   perf.Size2,
		5:   perf.Size5,
		25:  perf.Size25,
		100: perf.Size100,
	}
	out := make([]perf.FederationSize, 0, 4)
	cur := 0
	flush := func() {
		if cur == 0 {
			return
		}
		if sz, ok := want[cur]; ok {
			out = append(out, sz)
		}
		cur = 0
	}
	for i := 0; i < len(spec); i++ {
		ch := spec[i]
		switch {
		case ch >= '0' && ch <= '9':
			cur = cur*10 + int(ch-'0')
		case ch == ',' || ch == ' ':
			flush()
		}
	}
	flush()
	return out
}
