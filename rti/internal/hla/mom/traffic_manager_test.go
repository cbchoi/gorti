package mom

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func newTrafficManager(t *testing.T) (*Manager, *TrafficAccumulator) {
	t.Helper()
	m := newTestMOM(t, "f")
	a := NewTrafficAccumulator()
	if err := m.SetTrafficAccumulator(a); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateJoined(context.Background(), "f", 7, "seven", "test"); err != nil {
		t.Fatal(err)
	}
	return m, a
}

func TestMOMTrafficManagerLifecycle(t *testing.T) {
	m, a := newTrafficManager(t)
	if err := a.Record("f", 7, UpdatesSent, 2, core.TransportTypeReliable); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateJoined(context.Background(), "f", 7, "again", "test"); err == nil {
		t.Fatal("duplicate join reset history")
	}
	if err := m.SetTrafficAccumulator(NewTrafficAccumulator()); err == nil {
		t.Fatal("late accumulator replacement lost history")
	}
	if err := m.FederateResigned(context.Background(), "f", 7); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Snapshot(context.Background(), "f", 7, UpdatesSent); !errors.Is(err, core.ErrFederateNotJoined) {
		t.Fatalf("resign retained counters: %v", err)
	}
	if err := m.FederateJoined(context.Background(), "f", 7, "again", "test"); err != nil {
		t.Fatal(err)
	}
	if got := trafficCount(t, a, "f", 7, UpdatesSent, 2); got != 0 {
		t.Fatal("rejoin retained counters")
	}
	if err := m.FederationDestroyed(context.Background(), "f"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Snapshot(context.Background(), "f", 7, UpdatesSent); !errors.Is(err, core.ErrFederateNotJoined) {
		t.Fatalf("destroy retained counters: %v", err)
	}
}

func TestMOMTrafficManagerPreparedRestore(t *testing.T) {
	m, a := newTrafficManager(t)
	if err := a.Record("f", 7, UpdatesSent, 2, core.TransportTypeReliable); err != nil {
		t.Fatal(err)
	}
	m.IncrementUpdatesSent("f", 7)
	saved := m.ExportState("f")
	if saved.Traffic == nil {
		t.Fatal("MOM snapshot omitted traffic")
	}
	commit, err := m.PrepareImportState("f", saved)
	if err != nil {
		t.Fatal(err)
	}
	saved.Federates[0].UpdatesSent = 99
	saved.Traffic.Federates[0].Counts[0].Count = 99
	if err := a.Record("f", 7, UpdatesSent, 2, core.TransportTypeReliable); err != nil {
		t.Fatal(err)
	}
	m.IncrementUpdatesSent("f", 7)
	commit()
	attrs, _ := m.QueryFederateAttributes("f", 7)
	if attrs.UpdatesSent != 1 || trafficCount(t, a, "f", 7, UpdatesSent, 2) != 1 {
		t.Fatal("combined restore was not detached")
	}
	before := m.ExportState("f")
	invalid := m.ExportState("f")
	invalid.Federates[0].Name = "changed"
	invalid.Traffic.Federates[0].Counts[0].Count = 0
	if _, err := m.PrepareImportState("f", invalid); err == nil {
		t.Fatal("invalid traffic accepted")
	}
	if !reflect.DeepEqual(m.ExportState("f"), before) {
		t.Fatal("invalid traffic partially changed MOM")
	}
	invalid = m.ExportState("f")
	invalid.Traffic.Federates[0].Handle = 9
	if _, err := m.PrepareImportState("f", invalid); err == nil {
		t.Fatal("mismatched traffic roster accepted")
	}
}

func TestMOMTrafficManagerLegacyBaselineUnavailable(t *testing.T) {
	m, a := newTrafficManager(t)
	legacy := m.ExportState("f")
	legacy.Traffic = nil
	legacy.Federates[0].InteractionsSent = 3
	commit, err := m.PrepareImportState("f", legacy)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	attrs, _ := m.QueryFederateAttributes("f", 7)
	if attrs.InteractionsSent != 3 {
		t.Fatal("legacy aggregate not restored")
	}
	for _, kind := range []CounterKind{InteractionsSent, InteractionsReceived, UpdatesSent, ReflectionsReceived} {
		if _, err := a.Snapshot(context.Background(), "f", 7, kind); !errors.Is(err, ErrCounterDataUnavailable) {
			t.Fatalf("legacy %s fabricated history: %v", kind, err)
		}
	}
}
