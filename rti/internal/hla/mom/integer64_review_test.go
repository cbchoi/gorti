package mom

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

func TestReviewMOMExactIntegerAndLegacyFloatMetrics(t *testing.T) {
	ctx := context.Background()
	manager, err := New(Options{Outbox: metricOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FederateJoined(ctx, "integer", 1, "one", "model"); err != nil {
		t.Fatal(err)
	}
	for _, at := range []int64{1<<53 + 1, 1<<53 + 2, math.MaxInt64} {
		value, _ := logicaltime.Integer64(at)
		interval, _ := logicaltime.Integer64Interval(at)
		encoded, _ := value.MarshalBinary()
		ahead, _ := interval.MarshalBinary()
		manager.SetMetricSources(MetricSources{
			Federate: func(context.Context, core.FederationName, core.FederateHandle) (FederateMetrics, error) {
				return FederateMetrics{Time: &TimeMetrics{
					LogicalTimeValue: &value,
					Lookahead:        LogicalTimeMetric{Defined: true, ExactValue: ahead},
					GALT:             LogicalTimeMetric{Defined: true, ExactValue: encoded},
					LITS:             LogicalTimeMetric{Defined: false, ExactValue: encoded},
				}}, nil
			},
			Federation: func(context.Context, core.FederationName) (FederationMetrics, error) {
				return FederationMetrics{
					LastSaveTime: &LogicalTimeMetric{Defined: true, ExactValue: encoded},
					NextSaveTime: &LogicalTimeMetric{Defined: true, ExactValue: encoded},
				}, nil
			},
		})
		federate, err := manager.FederateMetricValues(ctx, "integer", 1)
		if err != nil {
			t.Fatal(err)
		}
		federation, err := manager.FederationMetricValues(ctx, "integer")
		if err != nil {
			t.Fatal(err)
		}
		for name, payload := range map[string][]byte{
			"HLAlogicalTime": federate.Values["HLAlogicalTime"], "HLAlookahead": federate.Values["HLAlookahead"], "HLAGALT": federate.Values["HLAGALT"],
			"HLAlastSaveTime": federation.Values["HLAlastSaveTime"], "HLAnextSaveTime": federation.Values["HLAnextSaveTime"],
		} {
			if len(payload) != 12 || binary.BigEndian.Uint32(payload) != 8 || binary.BigEndian.Uint64(payload[4:]) != uint64(at) {
				t.Fatalf("%s encoded %d as %x", name, at, payload)
			}
		}
		if payload := federate.Values["HLALITS"]; len(payload) != 4 || binary.BigEndian.Uint32(payload) != 0 {
			t.Fatalf("unbounded LITS fabricated finite value: %x", payload)
		}
	}
	for _, at := range []float64{0, math.SmallestNonzeroFloat64, 1 << 53, math.MaxFloat64, math.Inf(1)} {
		payload := logicalMetric(LogicalTimeMetric{Defined: true, Value: core.LogicalTime(at)})
		if len(payload) != 12 || binary.BigEndian.Uint64(payload[4:]) != math.Float64bits(at) {
			t.Fatalf("legacy metric %v became %x", at, payload)
		}
	}
}

func TestReviewMOMExactStatePreparedImportIsDetached(t *testing.T) {
	ctx := context.Background()
	manager, err := New(Options{Outbox: metricOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FederateJoined(ctx, "integer", 1, "one", "model"); err != nil {
		t.Fatal(err)
	}
	value, _ := logicaltime.Integer64(1<<53 + 1)
	interval, _ := logicaltime.Integer64Interval(math.MaxInt64)
	if err := manager.TimeStateChangedValue(ctx, "integer", 1, true, true, interval, value); err != nil {
		t.Fatal(err)
	}
	state := manager.ExportState("integer")
	commit, err := manager.PrepareImportState("integer", state)
	if err != nil {
		t.Fatal(err)
	}
	zero, _ := logicaltime.Integer64(0)
	zeroInterval, _ := logicaltime.Integer64Interval(0)
	*state.Federates[0].LogicalTimeValue = zero
	*state.Federates[0].LookaheadValue = zeroInterval
	commit()
	attrs, ok := manager.QueryFederate("integer", 1)
	if !ok || attrs.LogicalTimeValue == nil || attrs.LookaheadValue == nil {
		t.Fatal("missing exact restored time")
	}
	current, _ := attrs.LogicalTimeValue.Integer64()
	lookahead, _ := attrs.LookaheadValue.Integer64()
	if current != 1<<53+1 || lookahead != math.MaxInt64 || attrs.LogicalTime != 0 || attrs.Lookahead != 0 {
		t.Fatalf("prepared state aliased or narrowed: %+v", attrs)
	}
	*attrs.LogicalTimeValue = zero
	if again, _ := manager.QueryFederate("integer", 1); *again.LogicalTimeValue != value {
		t.Fatal("query exposed mutable authoritative typed state")
	}
}

func TestReviewMOMImportRejectsConflictingFloat64ExactPair(t *testing.T) {
	ctx := context.Background()
	manager, err := New(Options{Outbox: metricOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FederateJoined(ctx, "float", 1, "one", "model"); err != nil {
		t.Fatal(err)
	}
	value, _ := logicaltime.Float64(3)
	interval, _ := logicaltime.Float64Interval(2)
	for _, row := range []struct{ current, lookahead core.LogicalTime }{{4, 2}, {3, 4}, {0, 0}, {core.LogicalTime(math.Inf(1)), 2}} {
		state := manager.ExportState("float")
		entry := &state.Federates[0]
		entry.LogicalTime, entry.Lookahead = row.current, row.lookahead
		entry.LogicalTimeValue, entry.LookaheadValue = &value, &interval
		commit, err := manager.PrepareImportState("float", state)
		if err == nil || commit != nil {
			t.Errorf("conflicting pair accepted: legacy=%v/%v exact=3/2", row.current, row.lookahead)
		}
		if attrs, _ := manager.QueryFederate("float", 1); attrs.LogicalTime != 0 || attrs.LogicalTimeValue != nil {
			t.Fatal("failed import mutated live state")
		}
	}
	state := manager.ExportState("float")
	entry := &state.Federates[0]
	entry.LogicalTime, entry.Lookahead = 3, 2
	entry.LogicalTimeValue, entry.LookaheadValue = &value, &interval
	commit, err := manager.PrepareImportState("float", state)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	attrs, _ := manager.QueryFederate("float", 1)
	if attrs.LogicalTime != 3 || attrs.Lookahead != 2 || *attrs.LogicalTimeValue != value || *attrs.LookaheadValue != interval {
		t.Fatal("consistent Float64 exact/legacy pair did not survive import")
	}
}

func TestReviewMOMMalformedExactMetricDoesNotFallBackToLegacy(t *testing.T) {
	ctx := context.Background()
	manager, err := New(Options{Outbox: metricOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FederateJoined(ctx, "integer", 1, "one", "model"); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 1, 7, 9} {
		for _, name := range []string{"HLAlookahead", "HLAGALT", "HLALITS", "HLAlastSaveTime", "HLAnextSaveTime"} {
			t.Run(name+"/"+string(rune('0'+size)), func(t *testing.T) {
				malformed := LogicalTimeMetric{Defined: true, Value: 123, ExactValue: make([]byte, size)}
				manager.SetMetricSources(MetricSources{
					Federate: func(context.Context, core.FederationName, core.FederateHandle) (FederateMetrics, error) {
						time := &TimeMetrics{}
						switch name {
						case "HLAlookahead":
							time.Lookahead = malformed
						case "HLAGALT":
							time.GALT = malformed
						case "HLALITS":
							time.LITS = malformed
						}
						return FederateMetrics{Time: time}, nil
					},
					Federation: func(context.Context, core.FederationName) (FederationMetrics, error) {
						metrics := FederationMetrics{}
						if name == "HLAlastSaveTime" {
							metrics.LastSaveTime = &malformed
						} else {
							metrics.NextSaveTime = &malformed
						}
						return metrics, nil
					},
				})
				var snapshot MetricSnapshot
				var err error
				if name == "HLAlastSaveTime" || name == "HLAnextSaveTime" {
					snapshot, err = manager.FederationMetricValues(ctx, "integer")
				} else {
					snapshot, err = manager.FederateMetricValues(ctx, "integer", 1)
				}
				if !errors.Is(err, logicaltime.ErrInvalidEncoding) || snapshot.Values[name] != nil {
					t.Fatalf("malformed exact %s len=%d fabricated legacy payload=%x error=%v", name, size, snapshot.Values[name], err)
				}
				if got := logicalMetric(malformed); got != nil {
					t.Fatalf("raw logical metric silently fell back to legacy: %x", got)
				}
			})
		}
	}
}
