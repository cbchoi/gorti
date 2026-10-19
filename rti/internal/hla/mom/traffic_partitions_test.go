package mom

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestMOMTrafficReliableAndBestEffortPartitionsRestore(t *testing.T) {
	for _, kind := range []CounterKind{UpdatesSent, ReflectionsReceived, InteractionsSent, InteractionsReceived} {
		t.Run(string(kind), func(t *testing.T) {
			a := NewTrafficAccumulator()
			if err := a.FederateJoined("partition", 7); err != nil {
				t.Fatal(err)
			}
			for _, transport := range []core.TransportType{core.TransportTypeReliable, core.TransportTypeBestEffort, core.TransportTypeBestEffort} {
				if err := a.Record("partition", 7, kind, 11, transport); err != nil {
					t.Fatal("valid actual transport rejected", err)
				}
			}
			want := []TransportationCounts{{Transportation: "HLAreliable", Counts: map[uint64]uint32{11: 1}}, {Transportation: "HLAbestEffort", Counts: map[uint64]uint32{11: 2}}}
			rows, err := a.Snapshot(context.Background(), "partition", 7, kind)
			if err != nil || !reflect.DeepEqual(rows, want) {
				t.Fatalf("partition=%+v error=%v", rows, err)
			}
			b := NewTrafficAccumulator()
			if err := b.ImportState("partition", a.ExportState("partition")); err != nil {
				t.Fatal(err)
			}
			rows, err = b.Snapshot(context.Background(), "partition", 7, kind)
			if err != nil || !reflect.DeepEqual(rows, want) {
				t.Fatalf("restored partition=%+v error=%v", rows, err)
			}
			rows[1].Counts[11] = 99
			rows, _ = b.Snapshot(context.Background(), "partition", 7, kind)
			if !reflect.DeepEqual(rows, want) {
				t.Fatal("detached partition aliases live counts")
			}
		})
	}
}

func TestMOMBestEffortOverflowAndMalformedRestoreRemainAtomic(t *testing.T) {
	m, a := newEntryCounterManager(t)
	state := a.ExportState("f")
	state.Version = 2
	state.Federates[0].BestEffortCounts = []TrafficCountState{{Kind: InteractionsReceived, Class: 9, Count: math.MaxInt32}}
	if err := a.ImportState("f", state); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState("f")
	if err := m.RecordDataCallbackEntry("f", 7, DataCallbackEntry{Service: "receiveInteraction", InteractionClass: 9, Transportation: core.TransportTypeBestEffort}); !errors.Is(err, ErrCounterOverflow) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("best-effort entry overflow partially incremented counters")
	}
	for name, mutate := range map[string]func(*TrafficState){
		"v1WithBestEffort":   func(s *TrafficState) { s.Version = 1 },
		"unsupportedVersion": func(s *TrafficState) { s.Version = 3 },
		"duplicateBestEffort": func(s *TrafficState) {
			s.Federates[0].BestEffortCounts = append(s.Federates[0].BestEffortCounts, s.Federates[0].BestEffortCounts[0])
		},
		"zeroClass":       func(s *TrafficState) { s.Federates[0].BestEffortCounts[0].Class = 0 },
		"zeroCount":       func(s *TrafficState) { s.Federates[0].BestEffortCounts[0].Count = 0 },
		"countOutOfRange": func(s *TrafficState) { s.Federates[0].BestEffortCounts[0].Count = math.MaxUint32 },
		"unsupportedKind": func(s *TrafficState) { s.Federates[0].BestEffortCounts[0].Kind = "unknown" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := a.ExportState("f")
			mutate(invalid)
			if _, err := a.PrepareRestore("f", invalid); err == nil {
				t.Fatal("invalid partition snapshot accepted")
			}
			if !reflect.DeepEqual(before, m.ExportState("f")) {
				t.Fatal("invalid restore changed live partitioned state")
			}
		})
	}
	if err := a.Record("f", 7, InteractionsReceived, 9, core.TransportTypeBestEffort); !errors.Is(err, ErrCounterOverflow) {
		t.Fatal(err)
	}
	restored := NewTrafficAccumulator()
	if err := restored.ImportState("f", a.ExportState("f")); err != nil {
		t.Fatal("sticky best-effort overflow failed to restore", err)
	}
	if _, err := restored.Snapshot(context.Background(), "f", 7, InteractionsReceived); !errors.Is(err, ErrCounterOverflow) {
		t.Fatal("restored overflow fabricated valid report", err)
	}
}

func TestMOMBestEffortCallbackEntryCountsInCorrectPartition(t *testing.T) {
	for _, service := range []string{"receiveInteraction", "reflectAttributeValues"} {
		t.Run(service, func(t *testing.T) {
			m, err := New(Options{Outbox: noopOutbox{}})
			if err != nil {
				t.Fatal(err)
			}
			if err := m.SetRuntimeMetricsEnabled(true); err != nil {
				t.Fatal(err)
			}
			a := NewTrafficAccumulator()
			if err := m.SetTrafficAccumulator(a); err != nil {
				t.Fatal(err)
			}
			if err := m.FederateJoined(context.Background(), "partition", 7, "receiver", "test"); err != nil {
				t.Fatal(err)
			}
			entry := DataCallbackEntry{Service: service, Object: 13, ObjectClass: 11, InteractionClass: 11, Transportation: core.TransportTypeBestEffort}
			if err := m.RecordDataCallbackEntry("partition", 7, entry); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if err := m.RecordCallbackCompletion("partition", 7, CallbackCompletion{Invocation: CallbackInvocation{Service: service}, DataCountedAtEntry: true}); err != nil {
					t.Fatal(err)
				}
			}
			kind := InteractionsReceived
			if service == "reflectAttributeValues" {
				kind = ReflectionsReceived
			}
			rows, err := a.Snapshot(context.Background(), "partition", 7, kind)
			if err != nil || len(rows) != 2 || rows[1].Counts[11] != 1 || rows[0].Counts[11] != 0 {
				t.Fatalf("entry partition=%+v error=%v", rows, err)
			}
		})
	}
}
