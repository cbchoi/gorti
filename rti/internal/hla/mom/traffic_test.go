package mom

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func trafficCount(t *testing.T, a *TrafficAccumulator, fed core.FederationName, target core.FederateHandle, kind CounterKind, class uint64) uint32 {
	t.Helper()
	rows, err := a.Snapshot(context.Background(), fed, target, kind)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Transportation != "HLAreliable" {
		t.Fatalf("not the actual reliable-only profile: %+v", rows)
	}
	return rows[0].Counts[class]
}

func TestMOMTrafficAccumulatorPartitionsAndCopies(t *testing.T) {
	a := NewTrafficAccumulator()
	for _, fed := range []core.FederationName{"f", "other"} {
		for _, h := range []core.FederateHandle{7, 0x100000009} {
			if err := a.FederateJoined(fed, h); err != nil {
				t.Fatal(err)
			}
		}
	}
	for kindIndex, kind := range []CounterKind{InteractionsSent, InteractionsReceived, UpdatesSent, ReflectionsReceived} {
		for i := 0; i <= kindIndex; i++ {
			if err := a.Record("f", 0x100000009, kind, 0x10000000b, core.TransportTypeReliable); err != nil {
				t.Fatal(err)
			}
		}
		if got := trafficCount(t, a, "f", 0x100000009, kind, 0x10000000b); got != uint32(kindIndex+1) {
			t.Fatalf("%s count = %d", kind, got)
		}
		for _, other := range []struct {
			fed   core.FederationName
			h     core.FederateHandle
			class uint64
		}{{"f", 7, 0x10000000b}, {"other", 0x100000009, 0x10000000b}, {"f", 0x100000009, 12}} {
			if got := trafficCount(t, a, other.fed, other.h, kind, other.class); got != 0 {
				t.Fatalf("counter leaked: %d", got)
			}
		}
	}
	rows, err := a.Snapshot(context.Background(), "f", 0x100000009, InteractionsSent)
	if err != nil {
		t.Fatal(err)
	}
	rows[0].Counts[0x10000000b] = 99
	if got := trafficCount(t, a, "f", 0x100000009, InteractionsSent, 0x10000000b); got != 1 {
		t.Fatal("snapshot aliases live data")
	}
	if err := a.FederateJoined("f", 0x100000009); err == nil {
		t.Fatal("duplicate join silently reset history")
	}
}

func TestMOMTrafficAccumulatorLifecycle(t *testing.T) {
	a := NewTrafficAccumulator()
	if err := a.Record("f", 7, UpdatesSent, 2, core.TransportTypeReliable); !errors.Is(err, core.ErrFederateNotJoined) {
		t.Fatalf("untracked event: %v", err)
	}
	if _, err := a.Snapshot(context.Background(), "f", 7, UpdatesSent); err == nil {
		t.Fatal("untracked target fabricated zero")
	}
	for _, fed := range []core.FederationName{"f", "other"} {
		if err := a.FederateJoined(fed, 7); err != nil {
			t.Fatal(err)
		}
		if err := a.Record(fed, 7, UpdatesSent, 2, core.TransportTypeReliable); err != nil {
			t.Fatal(err)
		}
	}
	a.FederateResigned("f", 7)
	if err := a.Record("f", 7, UpdatesSent, 2, core.TransportTypeReliable); !errors.Is(err, core.ErrFederateNotJoined) {
		t.Fatalf("resigned event: %v", err)
	}
	if err := a.FederateJoined("f", 7); err != nil {
		t.Fatal(err)
	}
	if got := trafficCount(t, a, "f", 7, UpdatesSent, 2); got != 0 {
		t.Fatal("rejoin retained stale history")
	}
	a.FederationDestroyed("f")
	if _, err := a.Snapshot(context.Background(), "f", 7, UpdatesSent); err == nil {
		t.Fatal("destroyed federation still visible")
	}
	if got := trafficCount(t, a, "other", 7, UpdatesSent, 2); got != 1 {
		t.Fatal("cleanup changed other federation")
	}
}

func TestMOMTrafficAccumulatorOverflowAndUnsupportedTransport(t *testing.T) {
	for _, tc := range []struct {
		name      string
		count     uint32
		transport core.TransportType
		want      error
	}{
		{"overflow", math.MaxInt32, core.TransportTypeReliable, ErrCounterOverflow},
		{"unknownTransportIsNotDelivery", 1, core.TransportType(99), ErrCounterTransportUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewTrafficAccumulator()
			state := &TrafficState{Version: 1, Federation: "f", Federates: []TrafficFederateState{{Handle: 7, Counts: []TrafficCountState{{Kind: InteractionsSent, Class: 2, Count: tc.count}}}}}
			if err := a.ImportState("f", state); err != nil {
				t.Fatal(err)
			}
			if err := a.Record("f", 7, InteractionsSent, 2, tc.transport); !errors.Is(err, tc.want) {
				t.Fatalf("record = %v, want %v", err, tc.want)
			}
			if _, err := a.Snapshot(context.Background(), "f", 7, InteractionsSent); !errors.Is(err, tc.want) {
				t.Fatalf("incomplete history silently reported: %v", err)
			}
			if got := trafficCount(t, a, "f", 7, UpdatesSent, 2); got != 0 {
				t.Fatal("unrelated kind affected")
			}
			b := NewTrafficAccumulator()
			if err := b.ImportState("f", a.ExportState("f")); err != nil {
				t.Fatal(err)
			}
			if _, err := b.Snapshot(context.Background(), "f", 7, InteractionsSent); !errors.Is(err, tc.want) {
				t.Fatalf("restore lost sticky failure: %v", err)
			}
		})
	}
}

func TestMOMTrafficAccumulatorRestoreAtomicDetached(t *testing.T) {
	a := NewTrafficAccumulator()
	if err := a.FederateJoined("f", 7); err != nil {
		t.Fatal(err)
	}
	if err := a.Record("f", 7, UpdatesSent, 2, core.TransportTypeReliable); err != nil {
		t.Fatal(err)
	}
	saved := a.ExportState("f")
	if saved == nil {
		t.Fatal("missing state")
	}
	commit, err := a.PrepareRestore("f", saved)
	if err != nil {
		t.Fatal(err)
	}
	saved.Federates[0].Counts[0].Count = 99
	if err := a.Record("f", 7, UpdatesSent, 2, core.TransportTypeReliable); err != nil {
		t.Fatal(err)
	}
	if got := trafficCount(t, a, "f", 7, UpdatesSent, 2); got != 2 {
		t.Fatal("prepare changed live counters")
	}
	commit()
	if got := trafficCount(t, a, "f", 7, UpdatesSent, 2); got != 1 {
		t.Fatal("commit aliases input")
	}
	if err := a.Record("f", 7, UpdatesSent, 2, core.TransportTypeReliable); err != nil {
		t.Fatal(err)
	}
	commit()
	if got := trafficCount(t, a, "f", 7, UpdatesSent, 2); got != 2 {
		t.Fatal("second commit replayed snapshot")
	}
	before := a.ExportState("f")
	for name, mutate := range map[string]func(*TrafficState){
		"version": func(s *TrafficState) { s.Version = 99 }, "federation": func(s *TrafficState) { s.Federation = "other" },
		"duplicateFederate": func(s *TrafficState) { s.Federates = append(s.Federates, s.Federates[0]) },
		"zeroFederate":      func(s *TrafficState) { s.Federates[0].Handle = 0 },
		"duplicateCount":    func(s *TrafficState) { s.Federates[0].Counts = append(s.Federates[0].Counts, s.Federates[0].Counts[0]) },
		"zeroClass":         func(s *TrafficState) { s.Federates[0].Counts[0].Class = 0 },
		"zeroCount":         func(s *TrafficState) { s.Federates[0].Counts[0].Count = 0 },
		"countRange":        func(s *TrafficState) { s.Federates[0].Counts[0].Count = math.MaxUint32 },
		"unknownKind":       func(s *TrafficState) { s.Federates[0].Counts[0].Kind = "other" },
		"unknownFailure": func(s *TrafficState) {
			s.Federates[0].Failures = []TrafficFailureState{{Kind: UpdatesSent, Reason: "other"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			invalid := a.ExportState("f")
			mutate(invalid)
			if _, err := a.PrepareRestore("f", invalid); err == nil {
				t.Fatal("malformed state accepted")
			}
			if !reflect.DeepEqual(a.ExportState("f"), before) {
				t.Fatal("failed restore changed state")
			}
		})
	}
}

func TestMOMTrafficAccumulatorConcurrent(t *testing.T) {
	a := NewTrafficAccumulator()
	if err := a.FederateJoined("f", 7); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if err := a.Record("f", 7, UpdatesSent, 2, core.TransportTypeReliable); err != nil {
					t.Error(err)
					return
				}
				_, _ = a.Snapshot(context.Background(), "f", 7, UpdatesSent)
				_ = a.ExportState("f")
			}
		}()
	}
	wg.Wait()
	if got := trafficCount(t, a, "f", 7, UpdatesSent, 2); got != 800 {
		t.Fatalf("lost updates: %d", got)
	}
}
