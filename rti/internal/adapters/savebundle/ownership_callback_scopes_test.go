package savebundle

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

func TestOwnershipCallbackScopesExactAndReadOnly(t *testing.T) {
	manager, err := ownership.New(ownership.Options{Outbox: objectStateOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	state := &ownership.ManagerState{
		CallbackSessions: []ownership.CallbackSessionState{
			{Recipient: 7<<32 | 2, FederationGeneration: 7, CallbackEpoch: 9007199254740993, Enabled: true, LastCallbackID: 2, CompletedThrough: 1},
			{Recipient: 7<<32 | 3, FederationGeneration: 7, CallbackEpoch: ^uint64(0), Enabled: true},
			{Recipient: 7<<32 | 4, LastCallbackID: 1},
		},
		PendingCallbacks: []ownership.TerminalCallbackState{
			{Recipient: 7<<32 | 2, CallbackID: 2, Kind: ownership.CallbackUnavailable, Object: 5, Attributes: []core.AttributeHandle{1}},
			{Recipient: 7<<32 | 4, CallbackID: 1, Kind: ownership.CallbackUnavailable, Object: 5, Attributes: []core.AttributeHandle{1}},
		},
	}
	commit, err := manager.PrepareImportState("fed", state)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	codec := Ownership{Manager: manager}
	data, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	beforeData := bytes.Clone(data)
	beforeState := manager.ExportState("fed")
	got, err := OwnershipCallbackScopes(data)
	if err != nil {
		t.Fatal(err)
	}
	want := map[core.FederateHandle]core.OwnershipCallbackScope{
		7<<32 | 2: {Recipient: 7<<32 | 2, FederationGeneration: 7, CallbackEpoch: 9007199254740993},
		7<<32 | 3: {Recipient: 7<<32 | 3, FederationGeneration: 7, CallbackEpoch: ^uint64(0)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes = %#v, want %#v", got, want)
	}
	got[7<<32|2] = core.OwnershipCallbackScope{}
	again, err := OwnershipCallbackScopes(data)
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatalf("returned map aliases snapshot: %#v, %v", again, err)
	}
	if !bytes.Equal(data, beforeData) || !reflect.DeepEqual(manager.ExportState("fed"), beforeState) {
		t.Fatal("scope extraction mutated bytes or live ownership state")
	}
}

func TestOwnershipCallbackScopesLegacy(t *testing.T) {
	v1, err := marshalOwnershipSnapshot(&rtiv1.OwnershipManagerState{
		PendingDivests: []*rtiv1.PendingDivestEntry{{ObjectHandle: 5, AttributeHandle: 1, OwnerFederate: 2, TwoPhase: true}},
	}, []bool{false})
	if err != nil {
		t.Fatal(err)
	}
	v2, err := marshalOwnershipSnapshot(&rtiv1.OwnershipManagerState{}, nil,
		[]ownership.UnownedOfferState{{Object: 5, Attribute: 1, Divester: 2}})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := marshalOwnershipSnapshotComplete(&rtiv1.OwnershipManagerState{}, nil, nil,
		[]ownership.CallbackSessionState{{Recipient: 2}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"nil": nil, "empty": {}, "protobuf": {8, 1}, "v1": v1, "v2": v2, "disabled-v3": disabled} {
		t.Run(name, func(t *testing.T) {
			got, err := OwnershipCallbackScopes(data)
			if err != nil || got == nil || len(got) != 0 {
				t.Fatalf("legacy scopes = %#v, %v", got, err)
			}
		})
	}
}

func TestOwnershipCallbackScopesRejectInvalidSnapshot(t *testing.T) {
	valid := ownershipSnapshotEnvelope{Version: 3, ConfirmationRequested: []bool{},
		CallbackSessions: []ownership.CallbackSessionState{{Recipient: 2, FederationGeneration: 7, Enabled: true}}}
	cases := map[string]func(*ownershipSnapshotEnvelope){
		"duplicate": func(e *ownershipSnapshotEnvelope) {
			e.CallbackSessions = append(e.CallbackSessions, e.CallbackSessions[0])
		},
		"missing-generation": func(e *ownershipSnapshotEnvelope) { e.CallbackSessions[0].FederationGeneration = 0 },
		"zero-recipient":     func(e *ownershipSnapshotEnvelope) { e.CallbackSessions[0].Recipient = 0 },
		"hole":               func(e *ownershipSnapshotEnvelope) { e.CallbackSessions[0].LastCallbackID = 1 },
		"invalid-disabled": func(e *ownershipSnapshotEnvelope) {
			e.CallbackSessions = append(e.CallbackSessions, ownership.CallbackSessionState{})
		},
		"orphan-callback": func(e *ownershipSnapshotEnvelope) {
			e.PendingCallbacks = []ownership.TerminalCallbackState{{Recipient: 3, CallbackID: 1}}
		},
		"missing-history": func(e *ownershipSnapshotEnvelope) { e.ConfirmationRequested = nil },
		"bad-protobuf":    func(e *ownershipSnapshotEnvelope) { e.LegacyProtoBytes = []byte{255} },
		"bad-version":     func(e *ownershipSnapshotEnvelope) { e.Version = 99 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := valid
			e.CallbackSessions = append([]ownership.CallbackSessionState(nil), valid.CallbackSessions...)
			mutate(&e)
			payload, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			got, err := OwnershipCallbackScopes(append([]byte(ownershipSnapshotMagic), payload...))
			if err == nil || got != nil {
				t.Fatalf("invalid snapshot accepted: %#v, %v", got, err)
			}
		})
	}
	for _, data := range [][]byte{{255}, []byte(ownershipSnapshotMagic + "{"), []byte(ownershipSnapshotMagic + "{} {}")} {
		got, err := OwnershipCallbackScopes(data)
		if err == nil || got != nil {
			t.Fatalf("malformed snapshot accepted: %#v, %v", got, err)
		}
	}
}
