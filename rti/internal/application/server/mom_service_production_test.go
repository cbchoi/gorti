package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestProductionMOMResignStreamsAuthenticateAndCompleteLifecycleWriter(t *testing.T) {
	for _, transport := range []string{"confirmed", "interaction", "local-lrc", "local-lrc-batch"} {
		t.Run(transport, func(t *testing.T) {
			runtime, conn, _ := integerProductionServer(t, t.TempDir(), 231)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			const fed = "production-mom-resign"
			created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
			if err != nil {
				t.Fatal(err)
			}
			sender := momCompletionReviewJoin(ctx, t, conn, fed, "manager", "", created.FederationGeneration)
			target := momCompletionReviewJoin(ctx, t, conn, fed, "target", "", created.FederationGeneration)
			model, err := runtime.foms.Get(ctx, fed)
			if err != nil {
				t.Fatal(err)
			}
			class, ok := model.LookupInteractionClass(mom.ServiceClassPrefix + "HLAresignFederationExecution")
			if !ok {
				t.Fatal("standard MIM service absent from production FOM")
			}
			targetParameter, ok := model.LookupParameter(class, "HLAfederate")
			if !ok {
				t.Fatal("MIM target parameter missing")
			}
			actionParameter, ok := model.LookupParameter(class, "HLAresignAction")
			if !ok {
				t.Fatal("MIM action parameter missing")
			}
			auth := metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(sender.SessionToken))
			if _, err := rtiv1.NewDeclarationServiceClient(conn).PublishInteractionClass(auth, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: fed, FederateHandle: sender.FederateHandle, InteractionClassHandle: uint64(class)}); err != nil {
				t.Fatal(err)
			}
			request := &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: fed, FederateHandle: sender.FederateHandle, InteractionClassHandle: uint64(class), Parameters: map[uint64][]byte{uint64(targetParameter): catalogRPCHandle(target.FederateHandle), uint64(actionParameter): catalogRPCInt(6)}}
			send := func(token []byte) error {
				call, stop := context.WithTimeout(context.Background(), 3*time.Second)
				defer stop()
				if token != nil {
					call = metadata.AppendToOutgoingContext(call, session.MetadataKey, string(token))
				}
				if transport == "confirmed" {
					stream, err := rtiv1.NewConfirmedObjectServiceClient(conn).Exchange(call)
					if err != nil {
						return err
					}
					defer func() {
						if err := stream.CloseSend(); err != nil {
							t.Error(err)
						}
					}()
					if err := stream.Send(&rtiv1.ConfirmedObjectRequest{Operation: &rtiv1.ConfirmedObjectRequest_Open{Open: &rtiv1.ConfirmedObjectOpen{WireVersion: 1, FederationName: fed, FederateHandle: sender.FederateHandle, ExpectedFederationGeneration: created.FederationGeneration}}}); err != nil {
						return err
					}
					if _, err := stream.Recv(); err != nil {
						return err
					}
					if err := stream.Send(&rtiv1.ConfirmedObjectRequest{Sequence: 1, Operation: &rtiv1.ConfirmedObjectRequest_Interaction{Interaction: request}}); err != nil {
						return err
					}
					result, err := stream.Recv()
					if err != nil {
						return err
					}
					if len(result.ErrorStatus) != 0 {
						return errors.New("MOM resign returned a confirmed operation error")
					}
					return nil
				}
				if transport == "local-lrc" || transport == "local-lrc-batch" {
					stream, err := rtiv1.NewLocalLRCServiceClient(conn).Exchange(call)
					if err != nil {
						return err
					}
					defer func() {
						if err := stream.CloseSend(); err != nil {
							t.Error(err)
						}
					}()
					if err := stream.Send(&rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Open{Open: &rtiv1.LocalLRCOpen{WireVersion: 1, FederationName: fed, FederateHandle: sender.FederateHandle, ExpectedFederationGeneration: created.FederationGeneration, AckEvery: 1}}}); err != nil {
						return err
					}
					if _, err := stream.Recv(); err != nil {
						return err
					}
					interaction := &rtiv1.LocalLRCInteraction{Sequence: 1, InteractionClassHandle: request.InteractionClassHandle, Parameters: request.Parameters}
					frame := &rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Interaction{Interaction: interaction}}
					if transport == "local-lrc-batch" {
						frame.Body = &rtiv1.LocalLRCRequest_Batch{Batch: &rtiv1.LocalLRCBatch{Operations: []*rtiv1.LocalLRCOperation{{Body: &rtiv1.LocalLRCOperation_Interaction{Interaction: interaction}}}}}
					}
					if err := stream.Send(frame); err != nil {
						return err
					}
					result, err := stream.Recv()
					if err != nil {
						return err
					}
					if result.CommittedThrough != 1 {
						return errors.New("LocalLRC ACK did not commit the actual MOM operation")
					}
					return nil
				}
				stream, err := conn.NewStream(call, &grpc.StreamDesc{ClientStreams: true, ServerStreams: true}, momInteractionStreamMethod)
				if err != nil {
					return err
				}
				defer func() {
					if err := stream.CloseSend(); err != nil {
						t.Error(err)
					}
				}()
				if err := stream.SendMsg(request); err != nil {
					return err
				}
				return stream.RecvMsg(&rtiv1.Empty{})
			}
			if err := send(nil); status.Code(err) != codes.Unauthenticated {
				t.Fatalf("missing token error=%v", err)
			}
			if _, err := runtime.fedMgr.MemberIdentity(fed, core.FederateHandle(target.FederateHandle)); err != nil {
				t.Fatal("rejected request resigned target:", err)
			}
			if err := send(sender.SessionToken); err != nil {
				t.Fatal("production stream resign failed or deadlocked:", err)
			}
			if _, err := runtime.fedMgr.MemberIdentity(fed, core.FederateHandle(target.FederateHandle)); !errors.Is(err, core.ErrFederateNotJoined) {
				t.Fatalf("target remains joined: %v", err)
			}
			if _, err := runtime.fedMgr.MemberIdentity(fed, core.FederateHandle(sender.FederateHandle)); err != nil {
				t.Fatal("managing sender lost membership:", err)
			}
			if members := runtime.fedMgr.MembersOf(fed); len(members) != 1 {
				t.Fatalf("roster after actual resign=%v", members)
			}
		})
	}
}
