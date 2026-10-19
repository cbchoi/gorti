package savebundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestKnownClassCodecRequiresV4OnlyForExtendedHistory(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state *object.RegistryState
		want  uint32
	}{
		{"legacy", &object.RegistryState{}, 1},
		{"ownership", &object.RegistryState{Deletions: []object.DeletionState{{OwnershipSnapshot: true}}}, 2},
		{"tombstone", &object.RegistryState{Deletions: []object.DeletionState{{ObjectClass: 3}}}, 3},
		{"discovery", &object.RegistryState{Discoveries: []object.DiscoveryState{{KnownClass: 2}}}, 4},
		{"recipient", &object.RegistryState{Deletions: []object.DeletionState{{Recipients: []object.DeletionRecipientState{{KnownClass: 2}}}}}, 4},
		{"mixed-deletions", &object.RegistryState{Deletions: []object.DeletionState{{Recipients: []object.DeletionRecipientState{{KnownClass: 2}}}, {ObjectClass: 3}}}, 4},
		{"mixed-discovery", &object.RegistryState{Discoveries: []object.DiscoveryState{{KnownClass: 2}}, Deletions: []object.DeletionState{{ObjectClass: 3}}}, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := objectSnapshotVersion(tc.state); got != tc.want {
				t.Fatalf("version=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestKnownClassCodecStrictPresenceAndNoMutation(t *testing.T) {
	for version := 1; version <= 4; version++ {
		for _, location := range []string{"discovery", "recipient"} {
			for _, key := range []string{"KnownClass", "KNOWNCLASS", "kNoWnClAsS"} {
				for _, value := range []string{"0", "2", "null"} {
					t.Run(fmt.Sprintf("v%d/%s/%s/%s", version, location, key, value), func(t *testing.T) {
						state := fmt.Sprintf(`{"LastObjectHandle":4,"Objects":[{"Handle":4,"Name":"saved","Class":3,"Registrant":1}],"Discoveries":[{"Object":4,"Federate":2,%q:%s}]}`, key, value)
						if location == "recipient" {
							state = fmt.Sprintf(`{"LastObjectHandle":4,"Deletions":[{"Object":4,"Producer":1,"Timestamp":5,"Committed":true,"ObjectClass":3,"ObjectName":"saved","Recipients":[{"Recipient":2,"ReceivedTSO":true,%q:%s}]}]}`, key, value)
						}
						data := []byte(fmt.Sprintf(`{"version":%d,"state":%s}`, version, state))
						codec := objectStateCodec(t)
						seedObjectCodec(t, codec)
						before, err := codec.Marshal("fed")
						if err != nil {
							t.Fatal(err)
						}
						commit, err := codec.PrepareRestore("fed", data)
						valid := version == 4 && value != "null"
						if valid && (err != nil || commit == nil) {
							t.Fatalf("valid v4 rejected: %v", err)
						}
						if !valid && (err == nil || commit != nil) {
							t.Fatal("extension accepted without valid v4 presence")
						}
						after, marshalErr := codec.Marshal("fed")
						if marshalErr != nil || !bytes.Equal(before, after) {
							t.Fatalf("prepare mutated live state: %v", marshalErr)
						}
						_, rewriteErr := (Object{}).RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) { return h + 10, nil })
						if (rewriteErr == nil) != valid {
							t.Fatalf("rebind bypassed version/presence validation: %v", rewriteErr)
						}
					})
				}
			}
		}
	}
}

func TestKnownClassCodecLegacyAndV4RoundTrips(t *testing.T) {
	for _, version := range []uint32{1, 2, 3, 4} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			state := codecObjectState()
			if version >= 2 {
				state.Deletions = []object.DeletionState{{Object: state.Objects[0].Handle, Producer: 1, Timestamp: 5, OwnershipSnapshot: true}}
			}
			if version >= 3 {
				state.Deletions[0].ObjectName, state.Deletions[0].ObjectClass = testSaved, 3
				state.Deletions[0].Recipients = []object.DeletionRecipientState{{Recipient: 2, ReceivedTSO: true}}
			}
			wantClass := core.ObjectClassHandle(3)
			if version == 4 {
				wantClass = 2
				state.Discoveries[0].KnownClass = wantClass
				state.Deletions[0].Recipients[0].KnownClass = wantClass
			}
			codec := objectStateCodec(t)
			if err := codec.Unmarshal("fed", graphJSON(t, objectSnapshotV1{Version: version, State: state})); err != nil {
				t.Fatal(err)
			}
			_, known, ok := codec.Manager.LookupKnownObjectInstance("fed", 2, state.Objects[0].Handle)
			if !ok || known != wantClass {
				t.Fatalf("known class=%d,%t want=%d", known, ok, wantClass)
			}
			data, err := codec.Marshal("fed")
			if err != nil {
				t.Fatal(err)
			}
			var wire objectSnapshotV1
			if err := json.Unmarshal(data, &wire); err != nil || wire.Version != version {
				t.Fatalf("version changed: %d %v", wire.Version, err)
			}
			if version < 4 && bytes.Contains(data, []byte(`"KnownClass"`)) {
				t.Fatal("legacy actual class gained extension fields")
			}
			if err := codec.Unmarshal("fed", data); err != nil {
				t.Fatal(err)
			}
			again, err := codec.Marshal("fed")
			if err != nil || !bytes.Equal(data, again) {
				t.Fatalf("unstable round trip: %v", err)
			}
		})
	}
}

const testSaved = "saved"
