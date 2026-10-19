package server

import (
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func sendMOMStreamRequest(t *testing.T, r *momExecutionRPC, transport string, req *rtiv1.SendInteractionRequest, token []byte) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(token))
	if transport == "confirmed" {
		stream, err := rtiv1.NewConfirmedObjectServiceClient(r.conn).Exchange(ctx)
		if err != nil {
			return err
		}
		id, err := r.f.deps.Federations.MemberIdentity("catalog-provider", r.f.sender)
		if err != nil {
			return err
		}
		if err := stream.Send(&rtiv1.ConfirmedObjectRequest{Operation: &rtiv1.ConfirmedObjectRequest_Open{Open: &rtiv1.ConfirmedObjectOpen{WireVersion: 1, FederationName: "catalog-provider", FederateHandle: uint64(r.f.sender), ExpectedFederationGeneration: id.Generation}}}); err != nil {
			return err
		}
		if _, err := stream.Recv(); err != nil {
			return err
		}
		if err := stream.Send(&rtiv1.ConfirmedObjectRequest{Sequence: 1, Operation: &rtiv1.ConfirmedObjectRequest_Interaction{Interaction: req}}); err != nil {
			return err
		}
		ack, err := stream.Recv()
		if err != nil {
			return err
		}
		if len(ack.ErrorStatus) > 0 {
			return status.Error(codes.FailedPrecondition, "confirmed operation returned an error")
		}
		return stream.CloseSend()
	}
	stream, err := r.conn.NewStream(ctx, &grpc.StreamDesc{ClientStreams: true, ServerStreams: true}, momInteractionStreamMethod)
	if err != nil {
		return err
	}
	if err := stream.SendMsg(req); err != nil {
		return err
	}
	if err := stream.RecvMsg(&rtiv1.Empty{}); err != nil {
		return err
	}
	return stream.CloseSend()
}

func TestMOMServiceStreamActualResignAndCheckpointAdmission(t *testing.T) {
	for _, transport := range []string{"confirmed", "interaction"} {
		for _, service := range []string{momServiceResignFederationExecution, momServiceFederateSaveBegun, momServiceFederateSaveComplete, momServiceFederateRestoreComplete} {
			t.Run(transport+"/"+service, func(t *testing.T) {
				r := newMOMExecutionRPC(t, service)
				params := map[string][]byte{}
				if service == momServiceResignFederationExecution {
					params["HLAresignAction"] = catalogRPCInt(6)
				} else if service != momServiceFederateSaveBegun {
					params["HLAsuccessIndicator"] = catalogRPCInt(1)
				}
				req := r.request(t, service, r.f.target, params)
				r.publish(t, req)
				if err := sendMOMStreamRequest(t, r, transport, req, r.senderToken); err != nil {
					t.Fatal("MOM stream operation failed or deadlocked:", err)
				}
				if service == momServiceResignFederationExecution {
					if _, err := r.f.deps.Federations.MemberIdentity("catalog-provider", r.f.target); err == nil {
						t.Fatal("target remains joined")
					}
				}
			})
		}
	}
}

func TestMOMServiceStreamRejectsRecipientTokenAndOrdinaryCheckpointBypass(t *testing.T) {
	for _, transport := range []string{"confirmed", "interaction"} {
		for _, failure := range []string{"recipient-token", "ordinary-during-save"} {
			t.Run(transport+"/"+failure, func(t *testing.T) {
				service := momServiceEnableAsynchronousDelivery
				if failure == "ordinary-during-save" {
					service = momServiceFederateSaveBegun
				}
				r := newMOMExecutionRPC(t, service)
				req := r.request(t, momServiceEnableAsynchronousDelivery, r.f.target, nil)
				r.publish(t, req)
				token := r.senderToken
				want := codes.FailedPrecondition
				if failure == "recipient-token" {
					token = r.targetToken
					want = codes.PermissionDenied
				}
				if err := sendMOMStreamRequest(t, r, transport, req, token); status.Code(err) != want {
					t.Fatalf("error=%v want=%v", err, want)
				}
				if r.f.time.Metrics(core.FederationName("catalog-provider"), r.f.target).AsynchronousDelivery {
					t.Fatal("rejected stream operation changed state")
				}
			})
		}
	}
}
