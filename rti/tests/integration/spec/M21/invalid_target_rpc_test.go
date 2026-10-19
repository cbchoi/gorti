package m21spec

import (
	"context"
	"math"
	"net"
	"reflect"
	"testing"
	"time"

	grpcsvc "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/tests/testkit/grpctest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type targetMembers struct{ core.FederationStore }

func (targetMembers) ValidateMember(fed core.FederationName, h core.FederateHandle) error {
	if fed != "fed" || h != 1 {
		return core.ErrFederateNotJoined
	}
	return nil
}
func (targetMembers) GenerationFor(core.FederationName) (uint64, bool) { return 1, true }

type unusedTargetObjects struct{ core.ObjectRegistry }

func TestAdvanceTargetInvalidWireRequests(t *testing.T) {
	for _, service := range []struct {
		method     string
		newRequest func() proto.Message
	}{
		{"TimeAdvanceRequest", func() proto.Message { return &rtiv1.TARRequest{} }},
		{"TimeAdvanceRequestAvailable", func() proto.Message { return &rtiv1.TARARequest{} }},
		{"NextMessageRequest", func() proto.Message { return &rtiv1.NERRequest{} }},
		{"NextMessageRequestAvailable", func() proto.Message { return &rtiv1.NMRARequest{} }},
		{"FlushQueueRequest", func() proto.Message { return &rtiv1.FQRRequest{} }},
	} {
		t.Run(service.method, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			mgr := newSpecManager(t)
			t.Cleanup(func() { mgr.OnFederateResign(context.Background(), "fed", 1) })
			adapter, err := grpcsvc.NewServer(grpcsvc.Options{Federations: targetMembers{}, Declarations: declaration.New(), Objects: unusedTargetObjects{}, Outbox: nopOutbox{}, Time: mgr})
			if err != nil {
				t.Fatal(err)
			}
			srv := grpc.NewServer(grpc.UnaryInterceptor(adapter.UnaryMembershipInterceptor()))
			if err := adapter.Register(srv); err != nil {
				t.Fatal(err)
			}
			lis := bufconn.Listen(1024 * 1024)
			grpctest.Serve(t, srv, lis)
			conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			client := rtiv1.NewTimeServiceClient(conn)
			if _, err := client.EnableTimeConstrained(ctx, &rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1}); err != nil {
				t.Fatal(err)
			}
			invoke := func(target float64, trailer *metadata.MD) error {
				req := service.newRequest()
				msg := req.ProtoReflect()
				fields := msg.Descriptor().Fields()
				msg.Set(fields.ByName("wire_version"), protoreflect.ValueOfEnum(1))
				msg.Set(fields.ByName("federation_name"), protoreflect.ValueOfString("fed"))
				msg.Set(fields.ByName("federate_handle"), protoreflect.ValueOfUint64(1))
				msg.Set(fields.ByName("logical_time"), protoreflect.ValueOfFloat64(target))
				return conn.Invoke(ctx, "/rti.v1.TimeService/"+service.method, req, &rtiv1.Empty{}, grpc.Trailer(trailer))
			}
			for _, bad := range []struct {
				value     float64
				exception string
			}{{math.NaN(), "InvalidLogicalTime"}, {math.Inf(-1), "LogicalTimeAlreadyPassed"}, {-1, "LogicalTimeAlreadyPassed"}} {
				before := mgr.Snapshot("fed")
				var trailer metadata.MD
				err := invoke(bad.value, &trailer)
				if status.Code(err) != codes.InvalidArgument || !reflect.DeepEqual(trailer.Get("rti-spec-exception"), []string{bad.exception}) {
					t.Fatalf("target=%v: error=%v trailer=%v", bad.value, err, trailer)
				}
				if !reflect.DeepEqual(before, mgr.Snapshot("fed")) {
					t.Fatal("rejected wire request changed time state")
				}
			}
			var trailer metadata.MD
			if err := invoke(3, &trailer); err != nil {
				t.Fatal("valid follow-up failed", err)
			}
			state, err := client.QueryLogicalTime(ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1})
			if err != nil || state.GetLogicalTime() != 3 {
				t.Fatalf("valid follow-up did not progress: %v %v", state, err)
			}
		})
	}
}
