package time

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

func BenchmarkTARRoundTwoRegulators(b *testing.B) {
	manager, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	if err := manager.EnableRegulation(ctx, "benchmark", 1, 1); err != nil {
		b.Fatal(err)
	}
	if err := manager.EnableRegulation(ctx, "benchmark", 2, 1); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		target := core.LogicalTime(i + 1)
		if err := manager.TimeAdvanceRequest(ctx, "benchmark", 1, target); err != nil {
			b.Fatal(err)
		}
		if err := manager.TimeAdvanceRequest(ctx, "benchmark", 2, target); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecideGrantTAR(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		decision := decideGrant(ModeTAR, 1, 2, 3, false, 0, false)
		if !decision.fire || decision.time != 2 || !decision.clearPending {
			b.Fatal("unexpected TAR decision")
		}
	}
}
