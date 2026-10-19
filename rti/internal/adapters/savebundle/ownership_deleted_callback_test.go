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

func TestDeletedOwnershipCallbackSnapshotV4RoundTrip(t *testing.T) {
	sessions := []ownership.CallbackSessionState{{Recipient: 1<<32 | 2, FederationGeneration: 1, Enabled: true, LastCallbackID: 1}}
	callbacks := []ownership.TerminalCallbackState{{Recipient: 1<<32 | 2, CallbackID: 1, Kind: ownership.CallbackUnavailable, Object: 5, Attributes: []core.AttributeHandle{1}, RetainedProjection: &ownership.CallbackProjection{RegisteredClass: 7, KnownClass: 3, Attributes: []core.AttributeHandle{9}}}}
	data, err := marshalOwnershipSnapshotComplete(&rtiv1.OwnershipManagerState{}, nil, nil, sessions, callbacks)
	if err != nil {
		t.Fatal(err)
	}
	var envelope ownershipSnapshotEnvelope
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte(ownershipSnapshotMagic)), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Version != 4 {
		t.Fatal("retained projection used legacy snapshot version")
	}
	_, _, _, restoredSessions, restoredCallbacks, err := decodeOwnershipSnapshotComplete(data)
	if err != nil || !reflect.DeepEqual(restoredSessions, sessions) || !reflect.DeepEqual(restoredCallbacks, callbacks) {
		t.Fatalf("v4 roundtrip: %+v %v", restoredCallbacks, err)
	}
	envelope.Version = 3
	downgraded, _ := json.Marshal(envelope)
	if _, _, _, _, _, err := decodeOwnershipSnapshotComplete(append([]byte(ownershipSnapshotMagic), downgraded...)); err == nil {
		t.Fatal("v3 accepted v4-only projection")
	}
}
