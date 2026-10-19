package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func newMOMLocalLRCRPC(t *testing.T, service string, saveDisabled bool) *momExecutionRPC {
	t.Helper()
	return newMOMExecutionRPC(t, service, func(options *grpcadapter.Options, deps *momServiceAdmissionDependencies) {
		if saveDisabled {
			options.CheckpointAdmission, deps.Checkpoint = nil, nil
		}
		options.EnableLocalLRC = true
		options.Membership = newMOMLocalLRCMembership(*deps)
	})
}

func sendMOMLocalLRCRequest(r *momExecutionRPC, req *rtiv1.SendInteractionRequest, token []byte, batch bool) (result error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if token != nil {
		ctx = metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(token))
	}
	stream, err := rtiv1.NewLocalLRCServiceClient(r.conn).Exchange(ctx)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, stream.CloseSend()) }()
	generation, _ := r.f.deps.Federations.GenerationFor(core.FederationName(req.FederationName))
	if err := stream.Send(&rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Open{Open: &rtiv1.LocalLRCOpen{WireVersion: 1, FederationName: req.FederationName, FederateHandle: req.FederateHandle, ExpectedFederationGeneration: generation, AckEvery: 1}}}); err != nil {
		return err
	}
	if _, err := stream.Recv(); err != nil {
		return err
	}
	interaction := &rtiv1.LocalLRCInteraction{Sequence: 1, InteractionClassHandle: req.InteractionClassHandle, Parameters: req.Parameters}
	frame := &rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Interaction{Interaction: interaction}}
	if batch {
		frame.Body = &rtiv1.LocalLRCRequest_Batch{Batch: &rtiv1.LocalLRCBatch{Operations: []*rtiv1.LocalLRCOperation{{Body: &rtiv1.LocalLRCOperation_Interaction{Interaction: interaction}}}}}
	}
	if err := stream.Send(frame); err != nil {
		return err
	}
	ack, err := stream.Recv()
	if err != nil {
		return err
	}
	if ack.CommittedThrough != 1 {
		return errors.New("MOM frame did not advance the cumulative ACK")
	}
	return nil
}

func TestMOMLocalLRCStreamActualResignAndCheckpointAdmission(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, service := range []string{momServiceResignFederationExecution, momServiceFederateSaveBegun, momServiceFederateSaveComplete, momServiceFederateRestoreComplete} {
			for _, saveDisabled := range []bool{false, true} {
				if saveDisabled && service != momServiceResignFederationExecution {
					continue
				}
				t.Run(service+map[bool]string{false: "/single", true: "/batch"}[batch]+map[bool]string{false: "/save-enabled", true: "/save-disabled"}[saveDisabled], func(t *testing.T) {
					r := newMOMLocalLRCRPC(t, service, saveDisabled)
					params := map[string][]byte{}
					if service == momServiceResignFederationExecution {
						params["HLAresignAction"] = catalogRPCInt(6)
					} else if service != momServiceFederateSaveBegun {
						params["HLAsuccessIndicator"] = catalogRPCInt(1)
					}
					req := r.request(t, service, r.f.target, params)
					r.publish(t, req)
					if err := sendMOMLocalLRCRequest(r, req, r.senderToken, batch); err != nil {
						t.Fatal("actual LocalLRC control failed or deadlocked:", err)
					}
					if service == momServiceResignFederationExecution {
						if _, err := r.f.deps.Federations.MemberIdentity("catalog-provider", r.f.target); !errors.Is(err, core.ErrFederateNotJoined) {
							t.Fatal("target did not resign", err)
						}
					}
				})
			}
		}
	}
}

func TestMOMLocalLRCStreamRetainsAuthenticationAndOrdinarySaveBan(t *testing.T) {
	for _, failure := range []string{"missing-token", "recipient-token", testUnpublishedLabel, "ordinary-during-save"} {
		t.Run(failure, func(t *testing.T) {
			service := momServiceEnableAsynchronousDelivery
			if failure == "ordinary-during-save" {
				service = momServiceFederateSaveBegun
			}
			r := newMOMLocalLRCRPC(t, service, false)
			req := r.request(t, momServiceEnableAsynchronousDelivery, r.f.target, nil)
			if failure != testUnpublishedLabel {
				r.publish(t, req)
			}
			token := r.senderToken
			want := codes.FailedPrecondition
			if failure == "missing-token" {
				token, want = nil, codes.Unauthenticated
			}
			if failure == "recipient-token" {
				token, want = r.targetToken, codes.PermissionDenied
			}
			if err := sendMOMLocalLRCRequest(r, req, token, false); status.Code(err) != want {
				t.Fatalf("error=%v want=%v", err, want)
			}
			if r.f.time.Metrics("catalog-provider", r.f.target).AsynchronousDelivery {
				t.Fatal("rejected request changed target state")
			}
		})
	}
}
