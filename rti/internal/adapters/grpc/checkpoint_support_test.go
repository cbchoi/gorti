package grpc

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func supportCheckpointRequests() []struct {
	method  string
	request any
} {
	return []struct {
		method  string
		request any
	}{
		{rtiv1.SupportService_GetUpdateRateValue_FullMethodName, &rtiv1.GetUpdateRateValueRequest{FederationName: "fed", FederateHandle: 1}},
		{rtiv1.SupportService_GetUpdateRateValueForAttribute_FullMethodName, &rtiv1.GetUpdateRateValueForAttributeRequest{FederationName: "fed", FederateHandle: 1}},
		{rtiv1.SupportService_NormalizeFederateHandle_FullMethodName, &rtiv1.NormalizeFederateHandleRequest{FederationName: "fed", FederateHandle: 1}},
		{rtiv1.SupportService_NormalizeServiceGroup_FullMethodName, &rtiv1.NormalizeServiceGroupRequest{FederationName: "fed", FederateHandle: 1}},
		{rtiv1.FederationService_GetAutomaticResignDirective_FullMethodName, &rtiv1.GetAutomaticResignDirectiveRequest{FederationName: "fed", FederateHandle: 1}},
		{rtiv1.FederationService_SetAutomaticResignDirective_FullMethodName, &rtiv1.SetAutomaticResignDirectiveRequest{FederationName: "fed", FederateHandle: 1}},
	}
}

func TestCheckpointSupportQueriesUseSerializedAdmissionDuringSaveAndRestore(t *testing.T) {
	for _, blocked := range []error{core.ErrSaveAlreadyInProgress, core.ErrRestoreAlreadyInProgress} {
		for _, service := range supportCheckpointRequests() {
			t.Run(blocked.Error()+service.method, func(t *testing.T) {
				probe := &checkpointProbe{deny: blocked}
				srv := &Server{checkpointAdmission: probe, membership: checkpointMembership{probe: probe}}
				_, err := srv.UnaryMembershipInterceptor()(context.Background(), service.request,
					&stdgrpc.UnaryServerInfo{FullMethod: service.method},
					func(context.Context, any) (any, error) { probe.record("handler"); return nil, nil })
				if err != nil {
					t.Fatal(err)
				}
				probe.assertSteps(t, "checkpoint:fed", "member", "handler", "member-release", "checkpoint-release")
			})
		}
	}
}

func TestCheckpointSupportQueriesStillRejectInvalidMembership(t *testing.T) {
	for _, service := range supportCheckpointRequests() {
		t.Run(service.method, func(t *testing.T) {
			probe := &checkpointProbe{deny: core.ErrSaveAlreadyInProgress}
			srv := &Server{checkpointAdmission: probe, membership: checkpointMembership{probe: probe, err: core.ErrFederateNotJoined}}
			_, err := srv.UnaryMembershipInterceptor()(context.Background(), service.request,
				&stdgrpc.UnaryServerInfo{FullMethod: service.method},
				func(context.Context, any) (any, error) {
					t.Error("invalid member reached support handler")
					return nil, nil
				})
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("membership error=%v", err)
			}
			probe.assertSteps(t, "checkpoint:fed", "member", "checkpoint-release")
			if !probe.rw.TryLock() {
				t.Fatal("support admission leaked on membership failure")
			}
			probe.rw.Unlock()
		})
	}
}

func TestCheckpointSupportAdmissionIsAnExactStandardMethodList(t *testing.T) {
	for _, method := range rtiv1.SupportService_ServiceDesc.Methods {
		t.Run(method.MethodName, func(t *testing.T) {
			probe := &checkpointProbe{deny: core.ErrSaveAlreadyInProgress}
			release, err := acquireCheckpointAdmission(context.Background(), probe,
				&rtiv1.GetUpdateRateValueRequest{FederationName: "fed", FederateHandle: 1},
				"/rti.v1.SupportService/"+method.MethodName)
			if method.MethodName == "GetDimensionHandleSet" {
				if release != nil || status.Code(err) != codes.FailedPrecondition {
					t.Fatalf("region state must be fenced: %v", err)
				}
				return
			}
			if err != nil || release == nil {
				t.Fatalf("legal support method rejected: %v", err)
			}
			release()
			probe.assertSteps(t, "checkpoint:fed", "checkpoint-release")
		})
	}
	for _, method := range []string{
		"/rti.v1.SupportService/FutureMutation", "/rti.v1.SupportService/GetUpdateRateValueExtra",
		"/rti.v1.DeclarationService/SetAdvisorySwitch", "/rti.v1.DDMService/QueryBounds",
		"/rti.v1.TimeService/QueryLogicalTime", "/rti.v1.FederationService/ResignFederation",
	} {
		t.Run(method, func(t *testing.T) {
			probe := &checkpointProbe{deny: core.ErrRestoreAlreadyInProgress}
			release, err := acquireCheckpointAdmission(context.Background(), probe,
				&rtiv1.GetUpdateRateValueRequest{FederationName: "fed", FederateHandle: 1}, method)
			if release != nil || status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("non-exempt method admitted: %v", err)
			}
			probe.assertSteps(t, "service:fed")
		})
	}
}
