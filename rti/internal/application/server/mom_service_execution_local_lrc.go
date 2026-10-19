package server

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type momLocalLRCMembership struct {
	*federation.Manager
	deps momServiceAdmissionDependencies
}

// Retains all FM membership, lifecycle guard and value-type leaser methods.
// Use this solely as grpc Options.Membership; shared FM/session managers remain
// the original Manager and no authentication token is replaced.
func newMOMLocalLRCMembership(deps momServiceAdmissionDependencies) *momLocalLRCMembership {
	return &momLocalLRCMembership{Manager: deps.Federations, deps: deps}
}

func (m *momLocalLRCMembership) AdmitLocalLRCFrame(ctx context.Context, fed core.FederationName, sender core.FederateHandle, frame *rtiv1.LocalLRCRequest) (context.Context, func(), bool, error) {
	return newMOMLocalLRCFrameAdmission(m.deps)(ctx, fed, sender, frame)
}

// Opening is observational, including during a checkpoint. Every subsequent
// ordinary frame still needs AcquireService; only checkpoint confirmations use
// the special control admission. No ordinary mutation is admitted at Open.
func (m *momLocalLRCMembership) AdmitLocalLRCOpen(ctx context.Context, fed core.FederationName, sender core.FederateHandle, generation uint64) (func(), error) {
	if err := m.deps.Authenticate(ctx, fed, sender); err != nil {
		return nil, err
	}
	checkpointRelease := func() {}
	if m.deps.Checkpoint != nil {
		checkpointRelease = m.deps.Checkpoint.AcquireCheckpoint(fed)
	}
	memberRelease, err := m.Manager.AcquireMember(fed, sender)
	if err != nil {
		checkpointRelease()
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	release := func() { memberRelease(); checkpointRelease() }
	if err := m.deps.Authenticate(ctx, fed, sender); err != nil {
		release()
		return nil, err
	}
	identity, err := m.Manager.MemberIdentity(fed, sender)
	if err != nil {
		release()
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	if identity.Generation != generation {
		release()
		return nil, status.Error(codes.FailedPrecondition, core.ErrFederationGenerationMismatch.Error())
	}
	return release, nil
}

// Call after validating every frame sequence, but BEFORE the ordinary frame
// fencer. handled=true replaces (not supplements) that fencer. Pass returned
// ctx to applyLocalLRCInteraction and its MOM observer; release before any ACK.
// Multiple operations cannot retain an ordinary reader around a lifecycle
// writer. Reject that control frame before applying any member of the batch.
func newMOMLocalLRCFrameAdmission(deps momServiceAdmissionDependencies) func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.LocalLRCRequest) (context.Context, func(), bool, error) {
	return func(ctx context.Context, fed core.FederationName, sender core.FederateHandle, frame *rtiv1.LocalLRCRequest) (context.Context, func(), bool, error) {
		var interactions []*rtiv1.LocalLRCInteraction
		operations := 1
		if interaction := frame.GetInteraction(); interaction != nil {
			interactions = append(interactions, interaction)
		} else if batch := frame.GetBatch(); batch != nil {
			operations = len(batch.Operations)
			for _, operation := range batch.Operations {
				if interaction := operation.GetInteraction(); interaction != nil {
					interactions = append(interactions, interaction)
				}
			}
		}
		for _, interaction := range interactions {
			request := &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(sender), InteractionClassHandle: interaction.InteractionClassHandle, Parameters: interaction.Parameters}
			leaf, service := momRequestServiceLeaf(ctx, deps, request)
			if !service {
				continue
			}
			if operations != 1 {
				return ctx, nil, true, status.Error(codes.InvalidArgument, "MOM HLAservice requires a single-operation LocalLRC frame")
			}
			admitted, release, err := acquireMOMRequestAdmission(ctx, deps, request, leaf)
			return admitted, release, true, err
		}
		return ctx, nil, false, nil
	}
}
