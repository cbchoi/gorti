package mom

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func newEntryCounterManager(t *testing.T) (*Manager, *TrafficAccumulator) {
	t.Helper()
	m := newTestMOM(t, "f")
	a := NewTrafficAccumulator()
	if err := m.SetTrafficAccumulator(a); err != nil {
		t.Fatal(err)
	}
	if err := m.SetRuntimeMetricsEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateJoined(context.Background(), "f", 7, "seven", "test"); err != nil {
		t.Fatal(err)
	}
	return m, a
}

func TestMOMDataEntryAtomicFailureThenRetry(t *testing.T) {
	for _, bad := range []DataCallbackEntry{
		{Service: "receiveInteraction", Transportation: core.TransportTypeReliable},
		{Service: "receiveInteraction", InteractionClass: 9, Transportation: core.TransportType(99)},
		{Service: "reflectAttributeValues", ObjectClass: 9, Transportation: core.TransportTypeReliable},
	} {
		m, _ := newEntryCounterManager(t)
		before := m.ExportState("f")
		if err := m.RecordDataCallbackEntry("f", 7, bad); err == nil {
			t.Fatal("invalid entry accepted")
		}
		if !reflect.DeepEqual(before, m.ExportState("f")) {
			t.Fatal("failed entry mutated state")
		}
		if err := m.RecordDataCallbackEntry("f", 7, DataCallbackEntry{Service: "receiveInteraction", InteractionClass: 9, Transportation: core.TransportTypeReliable}); err != nil {
			t.Fatal(err)
		}
		if got := m.fed["f"].federates[7].runtimeMetrics.Counters["HLAinteractionsReceived"]; got != 1 {
			t.Fatalf("retry count=%d", got)
		}
	}
}

func TestMOMDataEntryPartitionOverflowIsAtomic(t *testing.T) {
	m, a := newEntryCounterManager(t)
	a.federations["f"][7].counts[InteractionsReceived] = map[uint64]uint32{9: math.MaxInt32}
	before := m.ExportState("f")
	entry := DataCallbackEntry{Service: "receiveInteraction", InteractionClass: 9, Transportation: core.TransportTypeReliable}
	if err := m.RecordDataCallbackEntry("f", 7, entry); !errors.Is(err, ErrCounterOverflow) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("overflow partially incremented counters")
	}
	a.federations["f"][7].counts[InteractionsReceived][9] = 0
	if err := m.RecordDataCallbackEntry("f", 7, entry); err != nil {
		t.Fatal(err)
	}
	if got := m.fed["f"].federates[7].runtimeMetrics.Counters["HLAinteractionsReceived"]; got != 1 {
		t.Fatal(got)
	}
}

func TestMOMDataEntryReflectionCountsInvocationNotAttributes(t *testing.T) {
	m, a := newEntryCounterManager(t)
	entry := DataCallbackEntry{Service: "reflectAttributeValues", Object: 5, ObjectClass: 9, Transportation: core.TransportTypeReliable}
	for i := 0; i < 2; i++ {
		if err := m.RecordDataCallbackEntry("f", 7, entry); err != nil {
			t.Fatal(err)
		}
	}
	r := m.fed["f"].federates[7].runtimeMetrics
	if r.Counters["HLAreflectionsReceived"] != 2 || r.Counters["HLAobjectInstancesReflected"] != 1 {
		t.Fatal(r)
	}
	rows, err := a.Snapshot(context.Background(), "f", 7, ReflectionsReceived)
	if err != nil || rows[0].Counts[9] != 2 {
		t.Fatal(rows, err)
	}
	history, err := a.ObjectInstanceCounts(context.Background(), "f", 7, ObjectsReflected)
	if err != nil || history[9] != 1 {
		t.Fatal(history, err)
	}
}

func TestMOMCallbackCompletionExplicitEntryFlag(t *testing.T) {
	m, _ := newEntryCounterManager(t)
	entry := DataCallbackEntry{Service: "receiveInteraction", InteractionClass: 9, Transportation: core.TransportTypeReliable}
	if err := m.RecordDataCallbackEntry("f", 7, entry); err != nil {
		t.Fatal(err)
	}
	inv := CallbackInvocation{Service: "receiveInteraction"}
	if err := m.RecordCallbackCompletion("f", 7, CallbackCompletion{Invocation: inv, DataCountedAtEntry: true}); err != nil {
		t.Fatal(err)
	}
	if got := m.fed["f"].federates[7].runtimeMetrics.Counters["HLAinteractionsReceived"]; got != 1 {
		t.Fatal("completion double counted", got)
	}
	if err := m.RecordCallbackCompletion("f", 7, CallbackCompletion{Invocation: inv}); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordCallbackCompletion("f", 7, CallbackCompletion{Invocation: CallbackInvocation{Service: "discoverObjectInstance"}, DataCountedAtEntry: true}); err != nil {
		t.Fatal(err)
	}
	r := m.fed["f"].federates[7].runtimeMetrics
	if r.Counters["HLAinteractionsReceived"] != 2 || r.Counters["HLAobjectInstancesDiscovered"] != 1 {
		t.Fatal("legacy/non-data compatibility", r)
	}
}
