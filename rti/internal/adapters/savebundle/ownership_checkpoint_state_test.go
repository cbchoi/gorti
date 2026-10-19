package savebundle

import (
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"google.golang.org/protobuf/proto"
)

type ownershipCheckpointReadOutbox struct{}

func (ownershipCheckpointReadOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return nil
}

func TestReadOwnershipCheckpointStateMatchesRestoreAndDetached(t *testing.T) {
	owner, err := ownership.New(ownership.Options{Outbox: ownershipCheckpointReadOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	scope := core.OwnershipCallbackScope{Federation: "read", Recipient: 2, FederationGeneration: 5, CallbackEpoch: 7}
	if err := owner.EnableOwnershipCallbackReceipts(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	owner.RegisterInitialOwnership("read", 1, 100, []core.AttributeHandle{1})
	if err := owner.AcquireIfAvailable(context.Background(), "read", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	codec := Ownership{Manager: owner}
	data, err := codec.Marshal("read")
	if err != nil {
		t.Fatal(err)
	}
	before := owner.ExportState("read")
	state, err := ReadOwnershipCheckpointState(data)
	if err != nil || !reflect.DeepEqual(before, state) {
		t.Fatalf("decoded=%+v error=%v", state, err)
	}
	state.PendingCallbacks[0].Attributes[0] = 99
	again, err := ReadOwnershipCheckpointState(data)
	if err != nil || !reflect.DeepEqual(before, again) || !reflect.DeepEqual(before, owner.ExportState("read")) {
		t.Fatal("read checkpoint aliases/mutates input or live state", err)
	}
	commit, err := codec.PrepareRestore("read", data)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	if !reflect.DeepEqual(again, owner.ExportState("read")) {
		t.Fatal("reader and restore interpret state differently")
	}
}

func TestReadOwnershipCheckpointStateLegacyAndMalformed(t *testing.T) {
	wire := &rtiv1.OwnershipManagerState{PendingDivests: []*rtiv1.PendingDivestEntry{{ObjectHandle: 100, AttributeHandle: 1, OwnerFederate: 2, TwoPhase: true}}, PendingAcquires: []*rtiv1.PendingAcquireEntry{{ObjectHandle: 100, AttributeHandle: 1, AcquirerFederate: 3}}}
	data, err := proto.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	state, err := ReadOwnershipCheckpointState(data)
	if err != nil || len(state.CallbackSessions) != 0 || len(state.PendingDivests) != 1 || !state.PendingDivests[0].ConfirmationRequested {
		t.Fatalf("legacy confirmation semantics changed: %+v %v", state, err)
	}
	if state, err := ReadOwnershipCheckpointState([]byte{255}); err == nil || state != nil {
		t.Fatal("malformed snapshot exposed state", state, err)
	}
	if state, err := ReadOwnershipCheckpointState(nil); err != nil || state == nil || len(state.PendingCallbacks) != 0 {
		t.Fatal("legacy empty snapshot rejected", state, err)
	}
}
