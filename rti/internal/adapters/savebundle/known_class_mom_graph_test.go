package savebundle

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func TestKnownClassMOMCallbackHistoryUsesHistoricalAncestor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		object core.ObjectHandle
		valid  bool
	}{
		{"HLAobjectRoot", 7, true}, {"HLAobjectRoot.Base", 7, true}, {"HLAobjectRoot.Base.Child", 7, true},
		{"HLAobjectRoot.Base.Child.Leaf", 7, false}, {"HLAobjectRoot.Sibling", 7, false}, {"unknown", 7, false},
		{"HLAobjectRoot.Base", 8, false}, {"HLAobjectRoot.Base", 6, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest, model, _ := knownClassGraphFixture(t, false)
			class, _ := model.LookupObjectClass(tc.name)
			if tc.name == "unknown" {
				class = 999
			}
			runtime := &momSnapshotRuntime{Federates: []momFederateRuntime{{Handle: 1}, {Handle: 2, RuntimeMetrics: &mom.RuntimeMetricState{
				Counters: map[string]uint32{"HLAobjectInstancesReflected": 1}, ReflectedObjects: map[core.ObjectHandle]core.ObjectClassHandle{tc.object: class},
			}}}}
			manifest.ManagerSnapshots["mom"] = knownClassMOMSnapshot(t, runtime)
			err := (RestoreGraph{}).Validate("fed", manifest, model)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
		})
	}
}

func TestKnownClassMOMTrafficHistoryStillUsesRegisteredClass(t *testing.T) {
	for _, reflected := range []bool{false, true} {
		for _, ancestor := range []bool{false, true} {
			manifest, model, state := knownClassGraphFixture(t, false)
			class := state.Objects[0].Class
			if ancestor {
				class = state.Discoveries[0].KnownClass
			}
			history := &mom.ObjectHistoryState{}
			entry := []mom.ObjectHistoryEntry{{Object: 7, Class: class}}
			if reflected {
				history.Reflected = entry
			} else {
				history.Updated = entry
			}
			runtime := &momSnapshotRuntime{Federates: []momFederateRuntime{{Handle: 1}, {Handle: 2}}, Traffic: &mom.TrafficState{Federation: "fed", Federates: []mom.TrafficFederateState{{Handle: 2, ObjectHistory: history}}}}
			manifest.ManagerSnapshots["mom"] = knownClassMOMSnapshot(t, runtime)
			err := (RestoreGraph{}).Validate("fed", manifest, model)
			if (err == nil) == ancestor {
				t.Fatalf("traffic reflected=%t ancestor=%t err=%v", reflected, ancestor, err)
			}
		}
	}
}

func knownClassMOMSnapshot(t *testing.T, runtime *momSnapshotRuntime) []byte {
	t.Helper()
	wire := graphProto(t, &rtiv1.MOMManagerState{Federation: &rtiv1.MOMFederationRecord{Name: "fed", FederateHandles: []uint64{1, 2}}, Federates: []*rtiv1.MOMFederateRecord{{Handle: 1, Name: "producer"}, {Handle: 2, Name: "recipient"}}})
	return append([]byte(momSnapshotMagic), graphJSON(t, momSnapshotEnvelope{Version: 1, LegacyProtoBytes: wire, Runtime: runtime})...)
}
