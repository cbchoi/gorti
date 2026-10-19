package savebundle

import (
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

func TestOwnershipSnapshotPreservesTerminalObligations(t *testing.T) {
	manager, err := ownership.New(ownership.Options{Outbox: objectStateOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	recipient := core.FederateHandle(1<<32 | 2)
	state := &ownership.ManagerState{CallbackSessions: []ownership.CallbackSessionState{{Recipient: recipient, FederationGeneration: 1, CallbackEpoch: 3, Enabled: true, LastCallbackID: 3, CompletedThrough: 1, CompletedIDs: []uint64{3}}}, PendingCallbacks: []ownership.TerminalCallbackState{{Recipient: recipient, CallbackID: 2, Kind: ownership.CallbackAcquisitionNotification, Object: 7, Attributes: []core.AttributeHandle{1}, Tag: []byte{0, 255}, Federate: recipient}}}
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
	restored, err := ownership.New(ownership.Options{Outbox: objectStateOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := (Ownership{Manager: restored}).Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.ExportState("fed"), state) {
		t.Fatalf("terminal obligations lost: %+v", restored.ExportState("fed"))
	}
	rebound, err := codec.RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) {
		return core.FederateHandle(9<<32 | uint64(h)&0xffffffff), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := (Ownership{Manager: restored}).Unmarshal("fed", rebound); err != nil {
		t.Fatal(err)
	}
	got := restored.ExportState("fed")
	if len(got.CallbackSessions) != 1 || got.CallbackSessions[0].FederationGeneration != 9 || got.CallbackSessions[0].Recipient != 9<<32|2 || got.PendingCallbacks[0].Federate != 9<<32|2 {
		t.Fatalf("terminal scope not rebound: %+v", got)
	}
}
