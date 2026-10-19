package mom

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func reflectedHistoryManager(t *testing.T) (*Manager, *TrafficAccumulator) {
	t.Helper()
	m, err := New(Options{Outbox: metricOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetRuntimeMetricsEnabled(true); err != nil {
		t.Fatal(err)
	}
	traffic := NewTrafficAccumulator()
	if err := m.SetTrafficAccumulator(traffic); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{1, 2} {
		if err := m.FederateJoined(context.Background(), "fed", h, "model", "type"); err != nil {
			t.Fatal(err)
		}
	}
	return m, traffic
}

func reflectedMetricCount(t *testing.T, m *Manager, h core.FederateHandle, name string, want uint32) {
	t.Helper()
	got, err := m.FederateMetricValues(context.Background(), "fed", h)
	if err != nil {
		t.Fatal(err)
	}
	if value := got.Values[name]; len(value) != 4 || binary.BigEndian.Uint32(value) != want || got.Unavailable[name] != "" {
		t.Fatalf("%s = %v, unavailable=%q; want %d", name, value, got.Unavailable[name], want)
	}
}

func TestRuntimeReflectedHistoryAcceptsKnownClassRediscovery(t *testing.T) {
	m, traffic := reflectedHistoryManager(t)
	ctx := context.Background()
	const obj core.ObjectHandle = 5
	const actual core.ObjectClassHandle = 90
	if err := m.RecordServiceInvocation("fed", 1, ServiceInvocation{Service: "registerObjectInstance", Success: true}); err != nil {
		t.Fatal(err)
	}
	// The object layer can rediscover the same actual instance under a different
	// known ancestor after local deletion. Replay the callback-side observations.
	for i, known := range []core.ObjectClassHandle{7, actual, actual, 7} {
		if i != 2 {
			if i != 0 {
				if err := m.RecordServiceInvocation("fed", 2, ServiceInvocation{Service: "localDeleteObjectInstance", Success: true}); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.RecordCallbackInvocation("fed", 2, CallbackInvocation{Service: "discoverObjectInstance", Object: obj, ObjectClass: known}); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.RecordServiceInvocation("fed", 1, ServiceInvocation{Service: "updateAttributeValues", Success: true}); err != nil {
			t.Fatal(err)
		}
		for _, incoming := range []bool{false, true} {
			h, kind := core.FederateHandle(1), UpdatesSent
			if incoming {
				h, kind = 2, ReflectionsReceived
			}
			if err := traffic.Record("fed", h, kind, uint64(actual), core.TransportTypeReliable); err != nil {
				t.Fatal(err)
			}
			if err := traffic.RecordObjectTraffic("fed", h, obj, actual, incoming); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.RecordCallbackInvocation("fed", 2, CallbackInvocation{Service: "reflectAttributeValues", Object: obj, ObjectClass: known}); err != nil {
			t.Fatalf("valid known-class observation %d rejected: %v", known, err)
		}
		reflectedMetricCount(t, m, 2, "HLAobjectInstancesReflected", 1)
		reflectedMetricCount(t, m, 2, "HLAreflectionsReceived", uint32(i+1))
		state := m.ExportState("fed")
		if state.Federates[1].RuntimeMetrics.ReflectedObjects[obj] != known {
			t.Fatal("latest observed class not retained")
		}
		if err := state.Validate("fed"); err != nil {
			t.Fatal(err)
		}
	}
	reflectedMetricCount(t, m, 1, "HLAobjectInstancesRegistered", 1)
	reflectedMetricCount(t, m, 1, "HLAupdatesSent", 4)
	reflectedMetricCount(t, m, 2, "HLAobjectInstancesDiscovered", 3)
	reflectedMetricCount(t, m, 2, "HLAobjectInstancesRemoved", 0)
	for _, incoming := range []bool{false, true} {
		h, kind, instances := core.FederateHandle(1), UpdatesSent, ObjectsUpdated
		if incoming {
			h, kind, instances = 2, ReflectionsReceived, ObjectsReflected
		}
		counts, err := traffic.Snapshot(ctx, "fed", h, kind)
		if err != nil || len(counts) != 1 || !reflect.DeepEqual(counts[0].Counts, map[uint64]uint32{uint64(actual): 4}) {
			t.Fatalf("actual-class traffic changed: %v %v", counts, err)
		}
		objects, err := traffic.ObjectInstanceCounts(ctx, "fed", h, instances)
		if err != nil || !reflect.DeepEqual(objects, map[uint64]uint32{uint64(actual): 1}) {
			t.Fatalf("registered-class history changed: %v %v", objects, err)
		}
	}
	if err := traffic.RecordObjectTraffic("fed", 1, obj, actual+1, false); err == nil {
		t.Fatal("changed registered class accepted")
	}
}

func TestRuntimeReflectedHistoryRestorePreservesObservedClass(t *testing.T) {
	source, _ := reflectedHistoryManager(t)
	if err := source.RecordCallbackInvocation("fed", 2, CallbackInvocation{Service: "reflectAttributeValues", Object: 5, ObjectClass: 7}); err != nil {
		t.Fatal(err)
	}
	saved := source.ExportState("fed")
	target, _ := reflectedHistoryManager(t)
	commit, err := target.PrepareImportState("fed", saved)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	for _, service := range []string{"discoverObjectInstance", "reflectAttributeValues"} {
		if err := target.RecordCallbackInvocation("fed", 2, CallbackInvocation{Service: service, Object: 5, ObjectClass: 90}); err != nil {
			t.Fatal(err)
		}
	}
	reflectedMetricCount(t, target, 2, "HLAobjectInstancesReflected", 1)
	reflectedMetricCount(t, target, 2, "HLAreflectionsReceived", 2)
	if saved.Federates[1].RuntimeMetrics.ReflectedObjects[5] != 7 {
		t.Fatal("restore aliases snapshot history")
	}
	if got := target.ExportState("fed"); got.Federates[1].RuntimeMetrics.ReflectedObjects[5] != 90 {
		t.Fatal("restored history did not accept known-class transition")
	}
}

func TestRuntimeReflectedHistoryRejectsInvalidIdentity(t *testing.T) {
	for _, inv := range []CallbackInvocation{
		{Service: "reflectAttributeValues", Object: 0, ObjectClass: 7},
		{Service: "reflectAttributeValues", Object: 5, ObjectClass: 0},
	} {
		m, _ := reflectedHistoryManager(t)
		if err := m.RecordCallbackInvocation("fed", 2, inv); err == nil {
			t.Fatal("invalid reflection identity accepted")
		}
		if got := m.ExportState("fed"); len(got.Federates[1].RuntimeMetrics.ReflectedObjects) != 0 {
			t.Fatal("invalid observation fabricated object history")
		}
		for _, class := range []core.ObjectClassHandle{7, 90} {
			if err := m.RecordCallbackInvocation("fed", 2, CallbackInvocation{Service: "reflectAttributeValues", Object: 5, ObjectClass: class}); err != nil {
				t.Fatal(err)
			}
		}
		got, err := m.FederateMetricValues(context.Background(), "fed", 2)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got.Values["HLAobjectInstancesReflected"]; ok || got.Unavailable["HLAobjectInstancesReflected"] != "callback object identity unavailable" {
			t.Fatal("valid transition fabricated recovery of invalid historical baseline")
		}
		if err := m.RecordCallbackInvocation("fed", 99, inv); !errors.Is(err, core.ErrFederateNotJoined) {
			t.Fatal(err)
		}
		state := m.ExportState("fed")
		state.Federates[1].RuntimeMetrics.ReflectedObjects[5] = 0
		if err := state.Validate("fed"); err == nil {
			t.Fatal("zero class accepted in snapshot history")
		}
	}
}
