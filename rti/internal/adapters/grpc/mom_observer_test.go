package grpc

import (
	"context"
	"errors"
	"strings"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestMOMObserverActualRPCOutcome(t *testing.T) {
	var got []ServiceObservation
	var observedErrors int
	o := NewMOMObserver(MOMObserverOptions{Observe: func(_ context.Context, s ServiceObservation) error {
		got = append(got, s)
		return errors.New("telemetry failed")
	}, OnError: func(error) { observedErrors++ }})
	req := &rtiv1.RegisterObjectRequest{FederationName: "f", FederateHandle: 4, ObjectClassHandle: 17, ObjectName: "actual"}
	response := &rtiv1.RegisterObjectResponse{ObjectHandle: 9}
	result, err := o.UnaryInterceptor()(context.Background(), req, &grpc.UnaryServerInfo{FullMethod: rtiv1.ObjectService_RegisterObjectInstance_FullMethodName}, func(context.Context, any) (any, error) { return response, nil })
	if err != nil || result != response || len(got) != 1 {
		t.Fatalf("result=%v error=%v observations=%v", result, err, got)
	}
	if got[0].Service != "registerObjectInstance" || !got[0].Success || got[0].Federation != "f" || got[0].Federate != 4 || !strings.Contains(strings.Join(got[0].ReturnedArguments, " "), "9") {
		t.Fatalf("outcome=%+v", got[0])
	}
	want := status.Error(codes.FailedPrecondition, "not published")
	_, err = o.UnaryInterceptor()(context.Background(), req, &grpc.UnaryServerInfo{FullMethod: rtiv1.ObjectService_RegisterObjectInstance_FullMethodName}, func(context.Context, any) (any, error) { return nil, want })
	if err != want || len(got) != 2 || got[1].Success || got[1].Exception != want.Error() || len(got[1].ReturnedArguments) != 0 || observedErrors != 2 {
		t.Fatalf("error=%v observations=%+v observerErrors=%d", err, got, observedErrors)
	}
}

func TestMOMObserverCatalogCoversAllHLARPCsAndCallbacks(t *testing.T) {
	internal := map[string]bool{"ListFederationMembers": true, "GetFOMView": true, "QueryLBTS": true, "LookupRoutingSpace": true, "ReportCallbackInvocation": true, "SetAdvisorySwitch": true, "ActivateTransportationTypeChange": true}
	files := []protoreflect.FileDescriptor{rtiv1.File_rti_v1_federation_proto, rtiv1.File_rti_v1_declaration_proto, rtiv1.File_rti_v1_object_proto, rtiv1.File_rti_v1_ownership_proto, rtiv1.File_rti_v1_time_proto, rtiv1.File_rti_v1_ddm_proto, rtiv1.File_rti_v1_support_proto, rtiv1.File_rti_v1_savepoint_proto, rtiv1.File_rti_v1_sync_proto}
	for _, file := range files {
		for i := 0; i < file.Services().Len(); i++ {
			service := file.Services().Get(i)
			for j := 0; j < service.Methods().Len(); j++ {
				method := service.Methods().Get(j)
				if internal[string(method.Name())] {
					continue
				}
				if name := momRPCService("/"+string(service.FullName())+"/"+string(method.Name()), nil); name == "" {
					t.Errorf("unobserved HLA RPC %s", method.FullName())
				}
			}
		}
	}
	message := (&rtiv1.FederateEvent{}).ProtoReflect().Descriptor()
	variants := message.Oneofs().ByName("event").Fields()
	for i := 0; i < variants.Len(); i++ {
		field := variants.Get(i)
		if field.Name() == callbackHalted || field.Name() == callbackLocalValidationBarrier {
			continue
		}
		event := &rtiv1.FederateEvent{}
		event.ProtoReflect().Set(field, event.ProtoReflect().NewField(field))
		if change := event.GetTransportationChangeConfirmed(); change != nil {
			change.ObjectHandle = 1
			change.AttributeHandles = []uint64{2}
		}
		name := callbackServiceName(event)
		if _, ok := mom.ServiceGroup(name); !ok {
			t.Errorf("unobserved callback %s -> %q", field.Name(), name)
		}
	}
}

func TestMOMObserverTransportationPreparationIsOneStandardService(t *testing.T) {
	for _, test := range []struct {
		name, method, service string
		request               any
	}{
		{"attribute", rtiv1.ObjectService_PrepareAttributeTransportationTypeChange_FullMethodName, "requestAttributeTransportationTypeChange",
			&rtiv1.ChangeAttributeTransportRequest{FederationName: "f", FederateHandle: 1, ObjectHandle: 7, AttributeHandles: []uint64{8}}},
		{"interaction", rtiv1.ObjectService_PrepareInteractionTransportationTypeChange_FullMethodName, "requestInteractionTransportationTypeChange",
			&rtiv1.ChangeInteractionTransportRequest{FederationName: "f", FederateHandle: 1, InteractionClassHandle: 9}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, success := range []bool{true, false} {
				var observations []ServiceObservation
				observer := NewMOMObserver(MOMObserverOptions{Observe: func(_ context.Context, observation ServiceObservation) error {
					observations = append(observations, observation)
					return nil
				}})
				var outcome error
				if !success {
					outcome = status.Error(codes.FailedPrecondition, "transportation preparation rejected")
				}
				ticket := []byte("private-transportation-capability")
				response := &rtiv1.TransportationChange{Ticket: ticket, ObjectHandle: 7}
				observer.Observe(context.Background(), test.method, test.request, response, outcome)
				activation := &rtiv1.ActivateTransportationTypeChangeRequest{FederationName: "f", FederateHandle: 1, Ticket: ticket}
				observer.Observe(context.Background(), rtiv1.ObjectService_ActivateTransportationTypeChange_FullMethodName, activation, response, nil)
				observer.Observe(context.Background(), rtiv1.ObjectService_ActivateTransportationTypeChange_FullMethodName, activation, nil, status.Error(codes.FailedPrecondition, "invalid ticket"))
				if len(observations) != 1 {
					t.Fatalf("success=%v: expected preparation only, got %+v", success, observations)
				}
				got := observations[0]
				if got.Service != test.service || got.Success != success || got.Federation != "f" || got.Federate != 1 {
					t.Fatalf("success=%v: incorrect standard service observation: %+v", success, got)
				}
				if !success && (got.Exception != outcome.Error() || len(got.ReturnedArguments) != 0) {
					t.Fatalf("incorrect failed service outcome: %+v", got)
				}
				if strings.Contains(strings.Join(got.ReturnedArguments, ";"), "ticket") {
					t.Fatalf("private capability in standard service report: %+v", got)
				}
			}
		})
	}
}

func TestMOMObserverLazyArgumentsAndNestedSecretRedaction(t *testing.T) {
	var got ServiceObservation
	o := NewMOMObserver(MOMObserverOptions{CaptureArguments: func(core.FederationName, core.FederateHandle) bool { return false }, Observe: func(_ context.Context, s ServiceObservation) error { got = s; return nil }})
	o.Observe(context.Background(), rtiv1.ObjectService_RegisterObjectInstance_FullMethodName, &rtiv1.RegisterObjectRequest{FederationName: "f", FederateHandle: 1, ObjectName: "not serialized"}, &rtiv1.RegisterObjectResponse{ObjectHandle: 5}, nil)
	if len(got.SuppliedArguments) != 0 || len(got.ReturnedArguments) != 0 || got.Service == "" {
		t.Fatal("disabled service reporting serialized arguments", got)
	}
	encoded := momRedactJSON([]byte(`{"nested":{"session_token":"secret","data":"actual"},"list":[{"callback_receipt":"secret2"}]}`))
	if strings.Contains(string(encoded), "secret") || !strings.Contains(string(encoded), "actual") {
		t.Fatal("nested secret leaked or actual argument lost", string(encoded))
	}
}

func TestMOMObserverNoSecretOrTransportServiceReport(t *testing.T) {
	var got []ServiceObservation
	o := NewMOMObserver(MOMObserverOptions{Observe: func(_ context.Context, s ServiceObservation) error { got = append(got, s); return nil }})
	o.Observe(context.Background(), rtiv1.SupportService_ReportCallbackInvocation_FullMethodName, &rtiv1.ReportCallbackInvocationRequest{FederationName: "f", FederateHandle: 1, CallbackReceipt: []byte("secret")}, &rtiv1.Empty{}, nil)
	if len(got) != 0 {
		t.Fatal("receipt reporting recursively reported")
	}
	o.Observe(context.Background(), rtiv1.FederationService_JoinFederation_FullMethodName, &rtiv1.JoinFederationRequest{FederationName: "f"}, &rtiv1.JoinFederationResponse{FederateHandle: 3}, nil)
	if len(got) != 1 || got[0].Federate != 3 || got[0].Service != "joinFederationExecution" {
		t.Fatalf("join observation=%+v", got)
	}
}

func TestMOMObserverRetainsCallerLeaseThroughReportFailure(t *testing.T) {
	held, released := false, false
	o := NewMOMObserver(MOMObserverOptions{
		ResolveCaller: func(context.Context) (core.FederationName, core.FederateHandle, func(), error) {
			held = true
			return "f", 8, func() { held = false; released = true }, nil
		},
		Observe: func(_ context.Context, actual ServiceObservation) error {
			if !held || actual.Federation != "f" || actual.Federate != 8 {
				t.Fatal("caller lease or subject lost")
			}
			return errors.New("report failed")
		},
	})
	want := &rtiv1.GetOrderTypeResponse{OrderType: 1}
	actual, err := o.UnaryInterceptor()(context.Background(), &rtiv1.GetOrderTypeRequest{OrderName: "Receive"}, &grpc.UnaryServerInfo{FullMethod: rtiv1.SupportService_GetOrderType_FullMethodName}, func(context.Context, any) (any, error) { return want, nil })
	if err != nil || actual != want || held || !released {
		t.Fatalf("result changed or lease leaked: %v %v %v %v", actual, err, held, released)
	}
}
