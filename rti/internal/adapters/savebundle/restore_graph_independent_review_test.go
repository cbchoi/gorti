package savebundle

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func TestRestoreGraphChecksActualCallbackReflectedObjectHistory(t *testing.T) {
	repo := composition.NewFOMRepository()
	model, err := repo.Load(context.Background(), []core.FOMModule{{Path: "history.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>Position</name></attribute></objectClass></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	cls, _ := model.LookupObjectClass("HLAobjectRoot.Vehicle")
	root, _ := model.LookupObjectClass("HLAobjectRoot")
	const member core.FederateHandle = 1<<32 | 1
	gen := uint64(1)
	for _, tc := range []struct {
		name   string
		object core.ObjectHandle
		class  core.ObjectClassHandle
		valid  bool
	}{
		{"deleted-object-history", 2, cls, true},
		{"future-class", 2, 999, false},
		{"never-allocated-object", 3, cls, false},
		{"live-object-ancestor-history", 1, root, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &momSnapshotRuntime{Federates: []momFederateRuntime{{
				Handle: member,
				RuntimeMetrics: &mom.RuntimeMetricState{
					Counters:         map[string]uint32{"HLAobjectInstancesReflected": 1},
					ReflectedObjects: map[core.ObjectHandle]core.ObjectClassHandle{tc.object: tc.class},
				},
			}}}
			legacy := graphProto(t, &rtiv1.MOMManagerState{
				Federation: &rtiv1.MOMFederationRecord{Name: "f", FederateHandles: []uint64{uint64(member)}},
				Federates:  []*rtiv1.MOMFederateRecord{{Handle: uint64(member), Name: "one"}},
			})
			momBytes := append([]byte(momSnapshotMagic), graphJSON(t, momSnapshotEnvelope{Version: 1, LegacyProtoBytes: legacy, Runtime: runtime})...)
			manifest := core.CheckpointManifest{
				Federation: "f", FederationGeneration: &gen,
				Federates: []core.FederateHandle{member}, FederateNames: []string{"one"},
				ManagerSnapshots: map[string][]byte{
					"object": graphJSON(t, objectSnapshotV1{Version: 1, State: &object.RegistryState{
						LastObjectHandle: 2, Objects: []object.InstanceState{{Handle: 1, Class: cls, Name: "live", Registrant: member}},
					}}),
					"ownership": nil, "declaration": graphJSON(t, declarationSnapshot{Version: 3}),
					"time": graphJSON(t, timeSnapshot{Version: 1}), "ddm": nil, "sync": nil, "mom": momBytes,
					"retractions": graphJSON(t, retractionSnapshot{Version: 1, History: &timepkg.RetractionHistory{Complete: true}}),
				},
			}
			err := (RestoreGraph{}).Validate("f", manifest, model)
			if tc.valid && err != nil {
				t.Fatalf("valid historical reference rejected: %v", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("invalid actual-callback object history accepted")
			}
		})
	}
}
