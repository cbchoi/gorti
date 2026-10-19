package server

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestMOMLocalLRCAdmissionReplacesReaderForActualResign(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "single-operation-batch"}[batch], func(t *testing.T) {
			var deps momServiceAdmissionDependencies
			r := newMOMExecutionRPC(t, momServiceResignFederationExecution, func(_ *grpcadapter.Options, admission *momServiceAdmissionDependencies) { deps = *admission })
			req := r.request(t, momServiceResignFederationExecution, r.f.target, map[string][]byte{"HLAresignAction": catalogRPCInt(6)})
			r.publish(t, req)
			interaction := &rtiv1.LocalLRCInteraction{Sequence: 1, InteractionClassHandle: req.InteractionClassHandle, Parameters: req.Parameters}
			frame := &rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Interaction{Interaction: interaction}}
			if batch {
				frame.Body = &rtiv1.LocalLRCRequest_Batch{Batch: &rtiv1.LocalLRCBatch{Operations: []*rtiv1.LocalLRCOperation{{Body: &rtiv1.LocalLRCOperation_Interaction{Interaction: interaction}}}}}
			}
			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, string(r.senderToken)))
			ctx, release, handled, err := newMOMLocalLRCFrameAdmission(deps)(ctx, "catalog-provider", r.f.sender, frame)
			if err != nil || !handled || release == nil {
				t.Fatalf("handled=%t error=%v", handled, err)
			}
			defer release()
			if err := r.f.deps.Objects.SendInteractionWire(ctx, "catalog-provider", r.f.sender, core.InteractionClassHandle(req.InteractionClassHandle), req.Parameters, nil); err != nil {
				t.Fatal("actual wire-applier resign failed:", err)
			}
			if _, err := r.f.deps.Federations.MemberIdentity("catalog-provider", r.f.target); !errors.Is(err, core.ErrFederateNotJoined) {
				t.Fatal("target not retired", err)
			}
		})
	}
}

func TestMOMLocalLRCMixedBatchRejectedBeforeServiceEffects(t *testing.T) {
	var deps momServiceAdmissionDependencies
	r := newMOMExecutionRPC(t, momServiceResignFederationExecution, func(_ *grpcadapter.Options, admission *momServiceAdmissionDependencies) { deps = *admission })
	req := r.request(t, momServiceResignFederationExecution, r.f.target, map[string][]byte{"HLAresignAction": catalogRPCInt(6)})
	r.publish(t, req)
	frame := &rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Batch{Batch: &rtiv1.LocalLRCBatch{Operations: []*rtiv1.LocalLRCOperation{
		{Body: &rtiv1.LocalLRCOperation_AttributeUpdate{AttributeUpdate: &rtiv1.LocalLRCAttributeUpdate{Sequence: 1, ObjectHandle: 999}}},
		{Body: &rtiv1.LocalLRCOperation_Interaction{Interaction: &rtiv1.LocalLRCInteraction{Sequence: 2, InteractionClassHandle: req.InteractionClassHandle, Parameters: req.Parameters}}},
	}}}}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, string(r.senderToken)))
	_, release, handled, err := newMOMLocalLRCFrameAdmission(deps)(ctx, "catalog-provider", r.f.sender, frame)
	if !handled || release != nil || status.Code(err) != codes.InvalidArgument {
		t.Fatalf("handled=%t release present=%t error=%v", handled, release != nil, err)
	}
	if _, err := r.f.deps.Federations.MemberIdentity("catalog-provider", r.f.target); err != nil {
		t.Fatal("rejected batch retired target:", err)
	}
}
