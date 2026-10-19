package save_restore_test

import (
	"context"
	"net"
	"testing"
	"time"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// Unused services satisfy composition requirements; invoking them is a test bug.
type unusedFederations struct{ core.FederationStore }
type unusedDeclarations struct{ core.DeclarationManagement }
type unusedObjects struct{ core.ObjectRegistry }

func TestSavepointWireProgressAndParticipantFailure(t *testing.T) {
	s := newStore(t)
	seed(t, s, "restore", nil)
	m, _ := manager(t, s, nil)
	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	composed, err := grpcadapter.NewServer(grpcadapter.Options{
		Federations: unusedFederations{}, Declarations: unusedDeclarations{},
		Objects: unusedObjects{}, Outbox: &outbox{}, Savepoint: m,
	})
	must(t, err)
	must(t, composed.Register(server))
	done := make(chan error, 1)
	go func() { done <- server.Serve(lis) }()
	t.Cleanup(func() { server.Stop(); _ = lis.Close(); <-done })
	conn, err := grpc.NewClient("passthrough:///savepoint", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
	must(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := rtiv1.NewSavepointServiceClient(conn)
	v := rtiv1.WireVersion_WIRE_VERSION_V1
	r1 := &rtiv1.FederateSaveResponseRequest{WireVersion: v, FederationName: "fed", FederateHandle: 1}
	var trailer metadata.MD
	_, err = c.FederateSaveBegun(ctx, r1, grpc.Trailer(&trailer))
	if status.Code(err) != codes.FailedPrecondition || len(trailer.Get("rti-spec-exception")) != 1 || trailer.Get("rti-spec-exception")[0] != "SaveNotInitiated" {
		t.Fatalf("begun exception=%v trailer=%v", err, trailer)
	}
	_, err = c.FederateSaveBegun(ctx, &rtiv1.FederateSaveResponseRequest{WireVersion: rtiv1.WireVersion(99)})
	if err == nil {
		t.Fatal("invalid wire version accepted")
	}
	_, err = c.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: v, FederationName: "fed", FederateHandle: 1, Label: "save"})
	must(t, err)
	_, err = c.FederateSaveComplete(ctx, r1, grpc.Trailer(&trailer))
	if err == nil || trailer.Get("rti-spec-exception")[0] != "FederateHasNotBegunSave" {
		t.Fatalf("premature complete=%v trailer=%v", err, trailer)
	}
	_, err = c.FederateSaveBegun(ctx, r1)
	must(t, err)
	q, err := c.QuerySaveState(ctx, &rtiv1.QuerySaveStateRequest{WireVersion: v, FederationName: "fed"})
	must(t, err)
	if q.ActiveLabel != "save" || len(q.FederateStatuses) != 2 || q.FederateStatuses[0].Status != rtiv1.FederateSaveProgress_FEDERATE_SAVE_PROGRESS_SAVING || q.FederateStatuses[1].Status != rtiv1.FederateSaveProgress_FEDERATE_SAVE_PROGRESS_INSTRUCTED_TO_SAVE {
		t.Fatalf("wire save status=%+v", q)
	}
	_, err = c.FederateSaveComplete(ctx, r1)
	must(t, err)
	r2 := &rtiv1.FederateSaveResponseRequest{WireVersion: v, FederationName: "fed", FederateHandle: 2}
	_, err = c.FederateSaveBegun(ctx, r2)
	must(t, err)
	_, err = c.FederateSaveComplete(ctx, r2)
	must(t, err)
	_, err = c.RequestFederationRestore(ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: v, FederationName: "fed", FederateHandle: 1, Label: "restore"})
	must(t, err)
	_, err = c.FederateRestoreNotComplete(ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: v, FederationName: "fed", FederateHandle: 1})
	must(t, err)
	qr, err := c.QueryRestoreState(ctx, &rtiv1.QueryRestoreStateRequest{WireVersion: v, FederationName: "fed", Label: "restore"})
	must(t, err)
	if qr.State != rtiv1.RestoreState_RESTORE_STATE_INITIATED || len(qr.FederateStatuses) != 2 || qr.FederateStatuses[0].Status != rtiv1.FederateRestoreProgress_FEDERATE_RESTORE_PROGRESS_WAITING_FOR_FEDERATION_TO_RESTORE || qr.FederateStatuses[1].Status != rtiv1.FederateRestoreProgress_FEDERATE_RESTORE_PROGRESS_RESTORING {
		t.Fatalf("wire restore status=%+v", qr)
	}
	_, err = c.FederateRestoreComplete(ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: v, FederationName: "fed", FederateHandle: 2})
	must(t, err)
	qr, err = c.QueryRestoreState(ctx, &rtiv1.QueryRestoreStateRequest{WireVersion: v, FederationName: "fed", Label: "restore"})
	must(t, err)
	if qr.State != rtiv1.RestoreState_RESTORE_STATE_FAILED || qr.FederateStatuses[0].Status != rtiv1.FederateRestoreProgress_FEDERATE_RESTORE_PROGRESS_NO_RESTORE_IN_PROGRESS {
		t.Fatalf("terminal wire status=%+v", qr)
	}
	future := 42.0
	_, err = c.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: v, FederationName: "fed", FederateHandle: 1, Label: "timed", SaveTime: &future})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("timed save pretended success: %v", err)
	}
}
