//go:build perfcompare

// Throughput-compare helper. Build tag `perfcompare` keeps it out of
// the default test set. Used to measure throughput before/after an
// optimization change.
//
//	go test -tags=perfcompare -run TestThroughput_Size25 -v ./rti/internal/perf/

package perf

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestThroughput_Size25(t *testing.T) {
	mgr, err := New(Options{Size: Size25, Duration: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	res, err := mgr.RunBaseline(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("size=%d duration=%.2fs sent=%d throughput=%.0f/s p50=%.2fms p99=%.2fms\n",
		res.FederationSize, res.DurationSeconds, res.InteractionsSent,
		res.ThroughputPerSecond, res.LatencyP50Ms, res.LatencyP99Ms)
}

func TestThroughput_Size5(t *testing.T) {
	mgr, err := New(Options{Size: Size5, Duration: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	res, err := mgr.RunBaseline(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("size=%d duration=%.2fs sent=%d throughput=%.0f/s p50=%.2fms p99=%.2fms\n",
		res.FederationSize, res.DurationSeconds, res.InteractionsSent,
		res.ThroughputPerSecond, res.LatencyP50Ms, res.LatencyP99Ms)
}

func TestThroughput_Size100(t *testing.T) {
	mgr, err := New(Options{Size: Size100, Duration: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	res, err := mgr.RunBaseline(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("size=%d duration=%.2fs sent=%d throughput=%.0f/s p50=%.2fms p99=%.2fms\n",
		res.FederationSize, res.DurationSeconds, res.InteractionsSent,
		res.ThroughputPerSecond, res.LatencyP50Ms, res.LatencyP99Ms)
}
