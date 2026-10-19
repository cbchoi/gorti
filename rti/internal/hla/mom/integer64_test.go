package mom

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

func TestMOMIntegerTimeStateAndSnapshotRemainExact(t *testing.T) {
	m := &Manager{fed: map[core.FederationName]*momState{"integer": newMOMState()}}
	m.fed["integer"].federates[1] = &federateSnapshot{handle: 1}
	current, _ := logicaltime.Integer64(1<<53 + 19)
	lookahead, _ := logicaltime.Integer64Interval(3)
	if err := m.TimeStateChangedValue(context.Background(), "integer", 1, true, true, lookahead, current); err != nil {
		t.Fatal(err)
	}
	attrs, ok := m.QueryFederate("integer", 1)
	if !ok || attrs.LogicalTimeValue == nil || attrs.LookaheadValue == nil {
		t.Fatal("exact time unavailable")
	}
	got, _ := attrs.LogicalTimeValue.Integer64()
	if got != 1<<53+19 {
		t.Fatalf("logical time=%d", got)
	}
	if attrs.LogicalTime != 0 || attrs.Lookahead != 0 {
		t.Fatal("integer state narrowed into legacy fields")
	}
	snapshot := m.ExportState("integer")
	saved, _ := snapshot.Federates[0].LogicalTimeValue.Integer64()
	if saved != got {
		t.Fatal("snapshot lost precision")
	}
	binaryValue, _ := attrs.LogicalTimeValue.MarshalBinary()
	metric := logicalMetric(LogicalTimeMetric{Defined: true, ExactValue: binaryValue})
	if len(metric) != 12 || binary.BigEndian.Uint64(metric[4:]) != uint64(got) {
		t.Fatalf("metric=%x", metric)
	}
}

func TestMOMRejectsMixedExactTimeValues(t *testing.T) {
	integer, _ := logicaltime.Integer64(1)
	floating, _ := logicaltime.Float64Interval(1)
	if err := validateTimeValues(&integer, &floating); err == nil {
		t.Fatal("mixed time values accepted")
	}
	if err := validateTimeValues(&integer, nil); err == nil {
		t.Fatal("incomplete time values accepted")
	}
}
