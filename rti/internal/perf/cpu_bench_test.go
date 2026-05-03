//go:build cpuprof

// CPU-profile capture helper. Build tag `cpuprof` keeps it out of
// the default test set so plain `go test ./...` ignores it.
//
// To reproduce the encoding-share numbers in
// docs/reports/M5/agent-a.md (TASK-084 decision input):
//
//	go test -tags=cpuprof -bench=Size25 -benchtime=1x \
//	    -cpuprofile=/tmp/cpu25.out -run=^$ ./rti/internal/perf/
//	go tool pprof -list 'binary\.|protowire' /tmp/cpu25.out
//
// Same for Size100. The encoding-related entries should sum to well
// under 5% (size 25) and 10% (size 100); anything above either bar is
// the trigger for re-opening TASK-084.

package perf

import (
	"context"
	"testing"
	"time"
)

func BenchmarkRunBaseline_Size25(b *testing.B) {
	mgr, err := New(Options{Size: Size25, Duration: 5 * time.Second})
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		if _, err := mgr.RunBaseline(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunBaseline_Size100(b *testing.B) {
	mgr, err := New(Options{Size: Size100, Duration: 5 * time.Second})
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		if _, err := mgr.RunBaseline(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}
