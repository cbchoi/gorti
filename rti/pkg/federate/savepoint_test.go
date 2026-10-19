package federate

import (
	"context"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"reflect"
	"testing"
)

type captureSavepointClient struct {
	rtiv1.SavepointServiceClient
	calls   []string
	request proto.Message
	err     error
	save    *rtiv1.QuerySaveStateResponse
	restore *rtiv1.QueryRestoreStateResponse
}

func (c *captureSavepointClient) RequestFederationSave(_ context.Context, req *rtiv1.RequestFederationSaveRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	c.calls = append(c.calls, "RequestFederationSave")
	c.request = req
	return &rtiv1.Empty{}, c.err
}

func (c *captureSavepointClient) FederateSaveBegun(_ context.Context, req *rtiv1.FederateSaveResponseRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	c.calls = append(c.calls, "FederateSaveBegun")
	c.request = req
	return &rtiv1.Empty{}, c.err
}

func (c *captureSavepointClient) FederateSaveComplete(_ context.Context, req *rtiv1.FederateSaveResponseRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	c.calls = append(c.calls, "FederateSaveComplete")
	c.request = req
	return &rtiv1.Empty{}, c.err
}

func (c *captureSavepointClient) FederateSaveNotComplete(_ context.Context, req *rtiv1.FederateSaveResponseRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	c.calls = append(c.calls, "FederateSaveNotComplete")
	c.request = req
	return &rtiv1.Empty{}, c.err
}

func (c *captureSavepointClient) RequestFederationRestore(_ context.Context, req *rtiv1.RequestFederationRestoreRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	c.calls = append(c.calls, "RequestFederationRestore")
	c.request = req
	return &rtiv1.Empty{}, c.err
}

func (c *captureSavepointClient) FederateRestoreComplete(_ context.Context, req *rtiv1.FederateRestoreResponseRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	c.calls = append(c.calls, "FederateRestoreComplete")
	c.request = req
	return &rtiv1.Empty{}, c.err
}

func (c *captureSavepointClient) FederateRestoreNotComplete(_ context.Context, req *rtiv1.FederateRestoreResponseRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	c.calls = append(c.calls, "FederateRestoreNotComplete")
	c.request = req
	return &rtiv1.Empty{}, c.err
}

func (c *captureSavepointClient) AbortFederationSave(_ context.Context, req *rtiv1.AbortFederationSaveRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	c.calls = append(c.calls, "AbortFederationSave")
	c.request = req
	return &rtiv1.Empty{}, c.err
}

func (c *captureSavepointClient) AbortFederationRestore(_ context.Context, req *rtiv1.AbortFederationRestoreRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	c.calls = append(c.calls, "AbortFederationRestore")
	c.request = req
	return &rtiv1.Empty{}, c.err
}

func (c *captureSavepointClient) QuerySaveState(_ context.Context, req *rtiv1.QuerySaveStateRequest, _ ...grpc.CallOption) (*rtiv1.QuerySaveStateResponse, error) {
	c.calls = append(c.calls, "QuerySaveState")
	c.request = req
	return c.save, c.err
}

func (c *captureSavepointClient) QueryRestoreState(_ context.Context, req *rtiv1.QueryRestoreStateRequest, _ ...grpc.CallOption) (*rtiv1.QueryRestoreStateResponse, error) {
	c.calls = append(c.calls, "QueryRestoreState")
	c.request = req
	return c.restore, c.err
}

func TestSavepointSDKForwardsExplicitTransitions(t *testing.T) {
	client := &captureSavepointClient{}
	fed := &Federate{conn: &Connection{savepoint: client}, federationName: testFederationName, federateHandle: 17}
	ctx := context.Background()
	for _, test := range []struct {
		name   string
		invoke func(context.Context) error
	}{
		{"FederateSaveBegun", fed.FederateSaveBegun},
		{"FederateSaveComplete", fed.FederateSaveComplete},
		{"FederateSaveNotComplete", fed.FederateSaveNotComplete},
		{"FederateRestoreComplete", fed.FederateRestoreComplete},
		{"FederateRestoreNotComplete", fed.FederateRestoreNotComplete},
		{"AbortFederationSave", fed.AbortFederationSave},
		{"AbortFederationRestore", fed.AbortFederationRestore},
		{"RequestFederationSave", func(ctx context.Context) error { return fed.RequestFederationSave(ctx, "label", nil) }},
		{"RequestFederationRestore", func(ctx context.Context) error { return fed.RequestFederationRestore(ctx, "label") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			client.calls = nil
			if test.name == "FederateRestoreComplete" {
				if err := fed.stageRuntimeRestore(InitiateFederateRestore{}); err != nil {
					t.Fatal(err)
				}
			}
			if err := test.invoke(ctx); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(client.calls, []string{test.name}) {
				t.Fatalf("extra or wrong RPC: %v", client.calls)
			}
			req := client.request.ProtoReflect()
			fields := req.Descriptor().Fields()
			if req.Get(fields.ByName("wire_version")).Enum() != 1 || req.Get(fields.ByName("federation_name")).String() != testFederationName || req.Get(fields.ByName("federate_handle")).Uint() != 17 {
				t.Fatalf("identity not forwarded: %v", client.request)
			}
		})
	}
	client.calls = nil
	client.err = status.Error(codes.FailedPrecondition, "save has not begun")
	if err := fed.FederateSaveComplete(ctx); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("error not propagated: %v", err)
	}
	if !reflect.DeepEqual(client.calls, []string{"FederateSaveComplete"}) {
		t.Fatalf("completion implicitly began save: %v", client.calls)
	}
}

func TestSavepointSDKCopiesStatusVectors(t *testing.T) {
	client := &captureSavepointClient{
		save:    &rtiv1.QuerySaveStateResponse{State: rtiv1.SaveState_SAVE_STATE_SAVED, ActiveLabel: "save", FederateStatuses: []*rtiv1.FederateSaveStatus{{FederateHandle: 17, Status: rtiv1.FederateSaveProgress_FEDERATE_SAVE_PROGRESS_SAVING}, {FederateHandle: 23, Status: rtiv1.FederateSaveProgress_FEDERATE_SAVE_PROGRESS_WAITING_FOR_FEDERATION_TO_SAVE}}},
		restore: &rtiv1.QueryRestoreStateResponse{State: rtiv1.RestoreState_RESTORE_STATE_FAILED, ActiveLabel: "restore", FederateStatuses: []*rtiv1.FederateRestoreStatus{{PreRestoreHandle: 17, PostRestoreHandle: 4, Status: rtiv1.FederateRestoreProgress_FEDERATE_RESTORE_PROGRESS_RESTORING}}},
	}
	fed := &Federate{conn: &Connection{savepoint: client}, federationName: testFederationName, federateHandle: 17}
	ctx := context.Background()
	save, err := fed.QueryFederationSaveStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	restore, err := fed.QueryFederationRestoreStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client.save.FederateStatuses[0].FederateHandle = 99
	client.restore.FederateStatuses[0].PostRestoreHandle = 99
	if save.ActiveLabel != "save" || !reflect.DeepEqual(save.FederateStatuses, []FederateSaveStatus{{17, FederateSaving}, {23, FederateWaitingForFederationToSave}}) {
		t.Fatalf("save vector lost: %+v", save)
	}
	if restore.ActiveLabel != "restore" || !reflect.DeepEqual(restore.FederateStatuses, []FederateRestoreStatus{{17, 4, FederateRestoring}}) {
		t.Fatalf("restore identities lost: %+v", restore)
	}
	if state, err := fed.QuerySaveState(ctx, "saved-label"); err != nil || state != SaveStateSaved {
		t.Fatalf("save state: %v %v", state, err)
	}
	if req := client.request.(*rtiv1.QuerySaveStateRequest); req.Label != "saved-label" {
		t.Fatal("save label lost")
	}
	if state, err := fed.QueryRestoreState(ctx, "restore-label"); err != nil || state != RestoreStateFailed {
		t.Fatalf("restore state: %v %v", state, err)
	}
	client.err = status.Error(codes.Unavailable, "offline")
	if _, err := fed.QueryFederationSaveStatus(ctx); status.Code(err) != codes.Unavailable {
		t.Fatalf("query error: %v", err)
	}
	if _, err := fed.QueryFederationRestoreStatus(ctx); status.Code(err) != codes.Unavailable {
		t.Fatalf("query error: %v", err)
	}
}
