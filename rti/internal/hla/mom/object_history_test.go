package mom_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func TestMOMObjectHistoryDistinctLifecycleAndRestore(t *testing.T) {
	a := mom.NewTrafficAccumulator()
	ctx := context.Background()
	for _, h := range []core.FederateHandle{7, 8} {
		if err := a.FederateJoined("fed", h); err != nil {
			t.Fatal(err)
		}
	}
	for _, event := range []struct {
		h     core.FederateHandle
		obj   core.ObjectHandle
		class core.ObjectClassHandle
		in    bool
	}{{7, 1, 90, false}, {7, 1, 90, false}, {7, 2, 90, false}, {7, 3, 91, false}, {7, 1, 90, true}, {8, 1, 90, true}} {
		if err := a.RecordObjectTraffic("fed", event.h, event.obj, event.class, event.in); err != nil {
			t.Fatal(err)
		}
	}
	check := func(a *mom.TrafficAccumulator, h core.FederateHandle, k mom.ObjectInstanceCountKind, want map[uint64]uint32) {
		t.Helper()
		got, err := a.ObjectInstanceCounts(ctx, "fed", h, k)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%d %s got %v, %v; want %v", h, k, got, err, want)
		}
	}
	check(a, 7, mom.ObjectsUpdated, map[uint64]uint32{90: 2, 91: 1})
	check(a, 7, mom.ObjectsReflected, map[uint64]uint32{90: 1})
	check(a, 8, mom.ObjectsUpdated, map[uint64]uint32{})
	check(a, 8, mom.ObjectsReflected, map[uint64]uint32{90: 1})
	state := a.ExportState("fed")
	b := mom.NewTrafficAccumulator()
	commit, err := b.PrepareRestore("fed", state)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	commit()
	if err := a.RecordObjectTraffic("fed", 7, 4, 90, false); err != nil {
		t.Fatal(err)
	}
	check(b, 7, mom.ObjectsUpdated, map[uint64]uint32{90: 2, 91: 1})
	if err := b.RecordObjectTraffic("fed", 7, 1, 90, false); err != nil {
		t.Fatal(err)
	}
	check(b, 7, mom.ObjectsUpdated, map[uint64]uint32{90: 2, 91: 1})
	b.FederateResigned("fed", 7)
	if _, err := b.ObjectInstanceCounts(ctx, "fed", 7, mom.ObjectsUpdated); !errors.Is(err, core.ErrFederateNotJoined) {
		t.Fatal(err)
	}
	if err := b.FederateJoined("fed", 7); err != nil {
		t.Fatal(err)
	}
	check(b, 7, mom.ObjectsUpdated, map[uint64]uint32{})
	b.FederationDestroyed("fed")
	if _, err := b.ObjectInstanceCounts(ctx, "fed", 8, mom.ObjectsReflected); !errors.Is(err, core.ErrFederateNotJoined) {
		t.Fatal(err)
	}
}

func TestMOMObjectHistoryLegacyBaselineUnavailable(t *testing.T) {
	a := mom.NewTrafficAccumulator()
	legacy := &mom.TrafficState{Version: 1, Federation: "fed", Federates: []mom.TrafficFederateState{{Handle: 7, Counts: []mom.TrafficCountState{{Kind: mom.UpdatesSent, Class: 90, Count: 5}}}}}
	if err := a.ImportState("fed", legacy); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordObjectTraffic("fed", 7, 1, 90, false); !errors.Is(err, mom.ErrCounterDataUnavailable) {
		t.Fatalf("legacy history accepted: %v", err)
	}
	for _, k := range []mom.ObjectInstanceCountKind{mom.ObjectsUpdated, mom.ObjectsReflected} {
		if _, err := a.ObjectInstanceCounts(context.Background(), "fed", 7, k); !errors.Is(err, mom.ErrCounterDataUnavailable) {
			t.Fatalf("legacy fabricated baseline %v", err)
		}
	}
}

func TestMOMObjectHistoryRejectsConflictingIdentity(t *testing.T) {
	a := mom.NewTrafficAccumulator()
	if err := a.FederateJoined("fed", 7); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordObjectTraffic("fed", 7, 1, 90, false); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordObjectTraffic("fed", 7, 1, 91, false); err == nil {
		t.Fatal("registered class changed")
	}
	if _, err := a.ObjectInstanceCounts(context.Background(), "fed", 7, mom.ObjectsUpdated); !errors.Is(err, mom.ErrCounterDataUnavailable) {
		t.Fatal("conflict did not poison ambiguous history", err)
	}
	if _, err := a.ObjectInstanceCounts(context.Background(), "fed", 7, mom.ObjectsDeletable); err == nil {
		t.Fatal("historical set fabricated live ownership")
	}
}
