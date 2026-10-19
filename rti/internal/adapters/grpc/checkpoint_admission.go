package grpc

import (
	"context"
	"strings"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// CheckpointAdmission isolates federation service calls from Save/Restore
// control calls. Its leases must be acquired before membership leases.
type CheckpointAdmission interface {
	AcquireService(core.FederationName) (func(), error)
	AcquireCheckpoint(core.FederationName) func()
}

type federationScopedRequest interface {
	GetFederationName() string
}

type checkpointAdmissionContextKey struct{}

// CheckpointAdmissionFederation identifies admission held by the enclosing
// unary interceptor. It is valid only during that interceptor's handler call.
// Observation adapters must not recursively acquire the same checkpoint lock.
func CheckpointAdmissionFederation(ctx context.Context) (core.FederationName, bool) {
	fed, ok := ctx.Value(checkpointAdmissionContextKey{}).(core.FederationName)
	return fed, ok
}

func acquireCheckpointAdmission(ctx context.Context, admission CheckpointAdmission, request any, method string) (func(), error) {
	if admission == nil {
		return nil, nil
	}
	scoped, ok := request.(federationScopedRequest)
	if !ok {
		return nil, nil
	}
	fed := core.FederationName(scoped.GetFederationName())
	if strings.HasPrefix(method, "/rti.v1.SavepointService/") || method == "/rti.v1.SupportService/ReportCallbackInvocation" || supportDuringCheckpoint(method) {
		return admission.AcquireCheckpoint(fed), nil
	}
	var release func()
	var err error
	if participant, ok := admission.(core.FederateServiceAdmission); ok {
		if scoped, ok := request.(interface{ GetFederateHandle() uint64 }); ok && scoped.GetFederateHandle() != 0 {
			release, err = participant.AcquireFederateService(fed, core.FederateHandle(scoped.GetFederateHandle()))
		} else {
			release, err = admission.AcquireService(fed)
		}
	} else {
		release, err = admission.AcquireService(fed)
	}
	return release, errToStatus(ctx, err)
}

// These support services remain legal during save/restore, unlike region-state
// queries and advisory mutations. The checkpoint lease still serializes them
// with snapshots and restore application; membership/authentication is unchanged.
func supportDuringCheckpoint(method string) bool {
	switch method {
	case rtiv1.FederationService_GetAutomaticResignDirective_FullMethodName,
		rtiv1.FederationService_SetAutomaticResignDirective_FullMethodName,
		rtiv1.FederationService_ListFederationMembers_FullMethodName,
		rtiv1.SupportService_NormalizeFederateHandle_FullMethodName,
		rtiv1.SupportService_NormalizeServiceGroup_FullMethodName,
		rtiv1.SupportService_GetUpdateRateValue_FullMethodName,
		rtiv1.SupportService_GetUpdateRateValueForAttribute_FullMethodName,
		rtiv1.SupportService_GetAvailableDimensionsForClassAttribute_FullMethodName,
		rtiv1.SupportService_GetAvailableDimensionsForInteractionClass_FullMethodName,
		rtiv1.SupportService_GetObjectClassHandle_FullMethodName,
		rtiv1.SupportService_GetObjectClassName_FullMethodName,
		rtiv1.SupportService_GetAttributeHandle_FullMethodName,
		rtiv1.SupportService_GetAttributeName_FullMethodName,
		rtiv1.SupportService_GetInteractionClassHandle_FullMethodName,
		rtiv1.SupportService_GetInteractionClassName_FullMethodName,
		rtiv1.SupportService_GetParameterHandle_FullMethodName,
		rtiv1.SupportService_GetParameterName_FullMethodName,
		rtiv1.SupportService_GetDimensionHandle_FullMethodName,
		rtiv1.SupportService_GetDimensionName_FullMethodName,
		rtiv1.SupportService_GetDimensionUpperBound_FullMethodName,
		rtiv1.SupportService_GetOrderType_FullMethodName,
		rtiv1.SupportService_GetOrderName_FullMethodName,
		rtiv1.SupportService_GetTransportationType_FullMethodName,
		rtiv1.SupportService_GetTransportationName_FullMethodName,
		rtiv1.SupportService_GetObjectInstanceHandle_FullMethodName,
		rtiv1.SupportService_GetObjectInstanceName_FullMethodName,
		rtiv1.SupportService_GetKnownObjectClassHandle_FullMethodName:
		return true
	default:
		return false
	}
}

func (s *membershipServerStream) acquireOperation(request any) error {
	releaseAdmission, err := acquireCheckpointAdmission(s.Context(), s.server.checkpointAdmission, request, "")
	if err != nil {
		return err
	}
	releaseMember, err := s.server.acquireMembership(s.Context(), request)
	if err != nil {
		if releaseAdmission != nil {
			releaseAdmission()
		}
		return err
	}
	s.currentAdmissionRelease = releaseAdmission
	s.currentRelease = releaseMember
	return nil
}
