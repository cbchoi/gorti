package time

import (
	"context"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func BenchmarkElapsedMeasureAcknowledge(b *testing.B) {
	m, err := New(Options{Clock: core.NewManualClock(stdtime.Unix(0, 0)), Outbox: nopOutbox{}})
	if err != nil {
		b.Fatal(err)
	}
	if err := m.OnFederateJoined("elapsed", 1); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x, err := m.MeasureElapsed("elapsed", 1)
		if err != nil {
			b.Fatal(err)
		}
		if err := m.AcknowledgeElapsed("elapsed", 1, x, ElapsedAttributes{Granted: true, Advancing: true}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkElapsedAdvanceRoundTrip(b *testing.B) {
	m, err := New(Options{Clock: core.NewManualClock(stdtime.Unix(0, 0)), Outbox: nopOutbox{}})
	if err != nil {
		b.Fatal(err)
	}
	if err := m.OnFederateJoined("elapsed", 1); err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	if err := m.EnableConstrained(ctx, "elapsed", 1); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := m.TimeAdvanceRequest(ctx, "elapsed", 1, core.LogicalTime(i)); err != nil {
			b.Fatal(err)
		}
	}
}
