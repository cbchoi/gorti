package savebundle

import (
	"context"
	"errors"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func TestOwnershipContinuationGraphAndIdentityRewrite(t *testing.T) {
	model, err := composition.NewFOMRepository().Load(context.Background(), []core.FOMModule{{Path: "offers.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Item</name><attribute><name>value</name></attribute></objectClass></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	class, _ := model.LookupObjectClass("HLAobjectRoot.Item")
	attr, _ := model.LookupAttribute(class, "value")
	const saved core.FederateHandle = 1<<32 | 1
	const rebound core.FederateHandle = 2<<32 | 9
	for _, tc := range []struct {
		name     string
		object   core.ObjectHandle
		attr     core.AttributeHandle
		divester core.FederateHandle
		valid    bool
	}{
		{"member", 1, attr, saved, true},
		{"departed-divester", 1, attr, 0, true},
		{"unknown-object", 999, attr, saved, false},
		{"undefined-attribute", 1, 999, saved, false},
		{"foreign-divester", 1, attr, rebound, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := marshalOwnershipSnapshot(&rtiv1.OwnershipManagerState{}, nil,
				[]ownership.UnownedOfferState{{Object: tc.object, Attribute: tc.attr, Divester: tc.divester}})
			if err != nil {
				t.Fatal(err)
			}
			generation := uint64(1)
			manifest := savepoint.Manifest{Federation: "f", FederationGeneration: &generation, Federates: []core.FederateHandle{saved}, FederateNames: []string{"member"}, ManagerSnapshots: map[string][]byte{
				"object":    graphJSON(t, objectSnapshotV1{Version: 1, State: &object.RegistryState{LastObjectHandle: 1, Objects: []object.InstanceState{{Handle: 1, Class: class, Name: "item", Registrant: saved}}}}),
				"ownership": data, "declaration": graphJSON(t, declarationSnapshot{Version: 3}),
				"time": graphJSON(t, timeSnapshot{Version: 1}), "ddm": nil, "sync": nil, "mom": nil,
				"retractions": graphJSON(t, retractionSnapshot{Version: 1, History: &timepkg.RetractionHistory{Complete: true}}),
			}}
			if err := (RestoreGraph{}).Validate("f", core.CheckpointManifest(manifest), model); (err == nil) != tc.valid {
				t.Fatalf("graph valid=%t: %v", tc.valid, err)
			}
			if !tc.valid {
				return
			}
			calls := 0
			rewritten, err := (Ownership{}).RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) {
				calls++
				if h != saved {
					t.Fatalf("rewriting non-member %d", h)
				}
				return rebound, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			_, _, offers, err := decodeOwnershipSnapshotState(rewritten)
			if err != nil || len(offers) != 1 {
				t.Fatalf("lost continuation: %v, %v", offers, err)
			}
			want, wantCalls := rebound, 1
			if tc.divester == 0 {
				want, wantCalls = 0, 0
			}
			if offers[0].Object != 1 || offers[0].Attribute != attr || offers[0].Divester != want || calls != wantCalls {
				t.Fatalf("incorrect identity rewrite: %+v, calls=%d", offers[0], calls)
			}
			if tc.divester != 0 {
				rejected := errors.New("unmapped saved participant")
				_, err := (Ownership{}).RewriteFederates(data, func(core.FederateHandle) (core.FederateHandle, error) { return 0, rejected })
				if !errors.Is(err, rejected) {
					t.Fatalf("rewrite lost validation failure: %v", err)
				}
			}
		})
	}
}
