package federate

import (
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestTransportationStreamConfirmationActivatesBeforeBodyAndDeduplicates(t *testing.T) {
	for _, interaction := range []bool{false, true} {
		t.Run(map[bool]string{false: "attributes", true: "interaction"}[interaction], func(t *testing.T) {
			trace := []string{}
			change := &rtiv1.TransportationChange{Ticket: make([]byte, 32), FederationGeneration: 3, CallbackEpoch: 1, TransportType: 1}
			if interaction {
				change.InteractionClassHandle = 9
			} else {
				change.ObjectHandle = 4
				change.AttributeHandles = []uint64{5, 6}
			}
			f := remainingServicesFederate(t, func(_ context.Context, req any, info *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
				switch r := req.(type) {
				case *rtiv1.ReportCallbackInvocationRequest:
					if r.InvocationEntry {
						trace = append(trace, "entry")
					} else {
						trace = append(trace, "completion")
					}
					return &rtiv1.Empty{}, nil
				case *rtiv1.ActivateTransportationTypeChangeRequest:
					if r.FederateHandle != 7 || r.ExpectedFederationGeneration != 3 || r.ExpectedCallbackEpoch != 1 || !reflect.DeepEqual(r.Ticket, change.Ticket) {
						t.Fatal("activation lost original scope", r)
					}
					trace = append(trace, "activate")
					result := proto.Clone(change).(*rtiv1.TransportationChange)
					if !interaction {
						result.AttributeHandles = nil
					}
					return result, nil
				default:
					t.Fatalf("unexpected RPC %s", info.FullMethod)
					return nil, nil
				}
			})
			f.federationGeneration = 3
			f.callbackEntrySupported = true
			event := f.translate(&rtiv1.FederateEvent{CallbackReceipt: make([]byte, 32), Event: &rtiv1.FederateEvent_TransportationChangeConfirmed{TransportationChangeConfirmed: change}})
			body := func(event Event) error {
				confirmed, ok := event.(TransportationChangeConfirmed)
				if !ok || len(confirmed.AttributeHandles) != 0 || confirmed.TransportationType != 1 {
					t.Fatalf("incorrect confirmation: %#v", event)
				}
				trace = append(trace, "body")
				return nil
			}
			for i := 0; i < 2; i++ {
				if a, r := f.DispatchEvent(context.Background(), event, body); a != nil || r != nil {
					t.Fatal(a, r)
				}
			}
			if !reflect.DeepEqual(trace, []string{"activate", "entry", "body", "completion"}) {
				t.Fatal(trace)
			}
		})
	}
}

func TestTransportationActivatedTicketRetriesEntryFailureWithoutDuplicateBody(t *testing.T) {
	failEntry := true
	activations, bodies, completions := 0, 0, 0
	change := &rtiv1.TransportationChange{Ticket: make([]byte, 32), FederationGeneration: 3, CallbackEpoch: 1, InteractionClassHandle: 9, TransportType: 1}
	f := remainingServicesFederate(t, func(_ context.Context, req any, info *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		switch r := req.(type) {
		case *rtiv1.ActivateTransportationTypeChangeRequest:
			activations++
			return proto.Clone(change), nil
		case *rtiv1.ReportCallbackInvocationRequest:
			if r.InvocationEntry && failEntry {
				if activations != 1 {
					t.Error("ENTRY preceded activation")
				}
				return nil, status.Error(codes.Unavailable, "entry retry")
			}
			if !r.InvocationEntry {
				completions++
			}
			return &rtiv1.Empty{}, nil
		default:
			t.Fatal(info.FullMethod)
			return nil, nil
		}
	})
	f.federationGeneration = 3
	f.callbackEntrySupported = true
	event := f.translate(&rtiv1.FederateEvent{CallbackReceipt: make([]byte, 32), Event: &rtiv1.FederateEvent_TransportationChangeConfirmed{TransportationChangeConfirmed: change}})
	body := func(Event) error { bodies++; return nil }
	if _, r := f.DispatchEvent(context.Background(), event, body); r == nil || bodies != 0 || completions != 0 {
		t.Fatal(r, bodies, completions)
	}
	failEntry = false
	for i := 0; i < 2; i++ {
		if a, r := f.DispatchEvent(context.Background(), event, body); a != nil || r != nil {
			t.Fatal(a, r)
		}
	}
	if activations != 2 || bodies != 1 || completions != 1 {
		t.Fatal(activations, bodies, completions)
	}
}

func TestTransportationActivationFailureDoesNotCompleteOrInvokeAndCanRetry(t *testing.T) {
	fail := true
	bodies, completions := 0, 0
	change := &rtiv1.TransportationChange{Ticket: make([]byte, 32), FederationGeneration: 3, CallbackEpoch: 1, InteractionClassHandle: 9, TransportType: 1}
	f := remainingServicesFederate(t, func(_ context.Context, req any, info *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		switch r := req.(type) {
		case *rtiv1.ReportCallbackInvocationRequest:
			if !r.InvocationEntry {
				completions++
			}
			return &rtiv1.Empty{}, nil
		case *rtiv1.ActivateTransportationTypeChangeRequest:
			if fail {
				return nil, status.Error(codes.Unavailable, "retry")
			}
			return proto.Clone(change), nil
		default:
			t.Fatal(info.FullMethod)
			return nil, nil
		}
	})
	f.federationGeneration = 3
	f.callbackEntrySupported = true
	event := f.translate(&rtiv1.FederateEvent{CallbackReceipt: make([]byte, 32), Event: &rtiv1.FederateEvent_TransportationChangeConfirmed{TransportationChangeConfirmed: change}})
	body := func(Event) error { bodies++; return nil }
	if a, r := f.DispatchEvent(context.Background(), event, body); a != nil || r == nil || bodies != 0 || completions != 0 {
		t.Fatal(a, r, bodies, completions)
	}
	fail = false
	if a, r := f.DispatchEvent(context.Background(), event, body); a != nil || r != nil || bodies != 1 || completions != 1 {
		t.Fatal(a, r, bodies, completions)
	}
}
