package save_restore_test

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestProductionSaveDisabledReturnsUnimplemented(t *testing.T) {
	f := startFederationWithOptions(t, processOptions{saveDisabled: true, adminEnabled: true})
	assertSnapshot := func() {
		t.Helper()
		for _, filter := range []string{"", f.name} {
			snapshot, err := f.admin.Snapshot(f.ctx, &rtiv1.SnapshotRequest{WireVersion: 1, FederationName: filter})
			check(t, err)
			if len(snapshot.Federations) != 1 {
				t.Fatalf("admin snapshot omitted the live federation: %v", snapshot)
			}
			fed := snapshot.Federations[0]
			if fed.Name != f.name || fed.FederatesJoined != 2 || len(fed.Federates) != 2 {
				t.Fatalf("admin snapshot lost the live roster: %v", fed)
			}
			seen := map[uint64]bool{}
			for _, member := range fed.Federates {
				seen[member.Handle] = true
			}
			for _, h := range f.handles {
				if !seen[h] {
					t.Errorf("joined handle %d missing from admin snapshot", h)
				}
			}
		}
	}
	// This calls the separate production admin server with a non-empty roster,
	// so a typed-nil SavepointCoordinator cannot hide behind an empty snapshot.
	assertSnapshot()
	participant := f.handles[0]
	saveResponse := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: f.name, FederateHandle: participant}
	restoreResponse := &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: f.name, FederateHandle: participant}
	requests := map[string]proto.Message{
		"RequestFederationSave": &rtiv1.RequestFederationSaveRequest{WireVersion: 1,
			FederationName: f.name, FederateHandle: participant, Label: "disabled"},
		"FederateSaveBegun":       saveResponse,
		"FederateSaveComplete":    saveResponse,
		"FederateSaveNotComplete": saveResponse,
		"QuerySaveState": &rtiv1.QuerySaveStateRequest{WireVersion: 1,
			FederationName: f.name, Label: "disabled"},
		"RequestFederationRestore": &rtiv1.RequestFederationRestoreRequest{WireVersion: 1,
			FederationName: f.name, FederateHandle: participant, Label: "disabled"},
		"FederateRestoreComplete":    restoreResponse,
		"FederateRestoreNotComplete": restoreResponse,
		"QueryRestoreState": &rtiv1.QueryRestoreStateRequest{WireVersion: 1,
			FederationName: f.name, Label: "disabled"},
		"AbortFederationSave": &rtiv1.AbortFederationSaveRequest{WireVersion: 1,
			FederationName: f.name, FederateHandle: &participant},
		"AbortFederationRestore": &rtiv1.AbortFederationRestoreRequest{WireVersion: 1,
			FederationName: f.name, FederateHandle: &participant},
	}
	descriptor := rtiv1.SavepointService_ServiceDesc
	if len(descriptor.Methods) != len(requests) || len(descriptor.Streams) != 0 {
		t.Fatal("update the disabled-service regression to cover the current Savepoint RPC surface")
	}
	for _, method := range descriptor.Methods {
		t.Run(method.MethodName, func(t *testing.T) {
			request, ok := requests[method.MethodName]
			if !ok {
				t.Fatalf("missing request for Savepoint RPC %s", method.MethodName)
			}
			// No response message exists for a disabled service. Empty also makes
			// an accidental successful response fail the status-code assertion.
			err := f.conn.Invoke(f.ctx, "/"+descriptor.ServiceName+"/"+method.MethodName, request, &rtiv1.Empty{})
			if status.Code(err) != codes.Unimplemented {
				t.Errorf("--save-dir= must disable %s: got %v, want Unimplemented", method.MethodName, err)
			}
		})
	}
	assertSnapshot()
	// The disabled subsystem must not prevent ordinary RTI operations.
	f.declare(0, 1)
	mark := f.callbacks[1].mark()
	payload := []byte{0, 0, 0, 9}
	check(t, f.send(0, payload, nil))
	f.expectInteraction(1, mark, payload)
	f.barrier()
}
