package mom

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestElapsedMetricSourceReadAndFailedPublicationDoNotAcknowledge(t *testing.T) {
	m, err := New(Options{Outbox: metricOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.FederateJoined(ctx, "elapsed", 1, "a", "t"); err != nil {
		t.Fatal(err)
	}
	acks := 0
	m.SetMetricSources(MetricSources{ElapsedTime: func(context.Context, core.FederationName, core.FederateHandle) (ElapsedTimeMetrics, error) {
		return ElapsedTimeMetrics{Granted: 2500 * time.Microsecond, Advancing: 3750 * time.Microsecond, Acknowledge: func(ElapsedTimeAttributes) error { acks++; return nil }}, nil
	}})
	for i := 0; i < 2; i++ {
		s, err := m.FederateMetricValues(ctx, "elapsed", 1)
		if err != nil {
			t.Fatal(err)
		}
		for name, want := range map[string]uint32{"HLAtimeGrantedTime": 2, "HLAtimeAdvancingTime": 3} {
			b := s.Values[name]
			if len(b) != 4 || binary.BigEndian.Uint32(b) != want {
				t.Fatalf("%s=%v; want integer32BE %d", name, b, want)
			}
		}
	}
	if acks != 0 {
		t.Fatal("query consumed interval")
	}
	if err := m.RefreshMetrics(ctx, "elapsed", 1); err == nil {
		t.Fatal("publication without fanout succeeded")
	}
	if acks != 0 {
		t.Fatal("failed publication consumed interval")
	}
}

func TestElapsedMetricSourceRequiresPublicationAcknowledgement(t *testing.T) {
	m, err := New(Options{Outbox: metricOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.FederateJoined(context.Background(), "elapsed", 1, "a", "t"); err != nil {
		t.Fatal(err)
	}
	m.SetMetricSources(MetricSources{ElapsedTime: func(context.Context, core.FederationName, core.FederateHandle) (ElapsedTimeMetrics, error) {
		return ElapsedTimeMetrics{Granted: time.Second}, nil
	}})
	s, err := m.FederateMetricValues(context.Background(), "elapsed", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Values["HLAtimeGrantedTime"]) != 0 || s.Unavailable["HLAtimeGrantedTime"] == "" {
		t.Fatal("source without publication baseline acknowledgement advertised as complete", s)
	}
}
