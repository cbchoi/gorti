package server

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type momNoRecursiveMembership struct {
	*federation.Manager
	calls atomic.Int32
}

func (m *momNoRecursiveMembership) ValidateMember(core.FederationName, core.FederateHandle) error {
	m.calls.Add(1)
	return fmt.Errorf("recursive member validation while outer lifecycle reader held")
}

func (m *momNoRecursiveMembership) AcquireMember(core.FederationName, core.FederateHandle) (func(), error) {
	m.calls.Add(1)
	return nil, fmt.Errorf("inner transport reacquired outer lifecycle reader")
}

func TestMOMServiceStreamsDoNotReenterLifecycleAdmission(t *testing.T) {
	for _, transport := range []string{"confirmed", "interaction"} {
		for _, saveDisabled := range []bool{false, true} {
			for _, service := range []string{momServiceEnableAsynchronousDelivery, momServiceResignFederationExecution} {
				t.Run(fmt.Sprintf("%s/saveDisabled=%t/%s", transport, saveDisabled, service), func(t *testing.T) {
					var guard *momNoRecursiveMembership
					r := newMOMExecutionRPC(t, service, func(options *grpcadapter.Options, deps *momServiceAdmissionDependencies) {
						guard = &momNoRecursiveMembership{Manager: deps.Federations}
						options.Membership = guard
						if saveDisabled {
							options.CheckpointAdmission = nil
							deps.Checkpoint = nil
						}
					})
					params := map[string][]byte{}
					if service == momServiceResignFederationExecution {
						params["HLAresignAction"] = catalogRPCInt(6)
					}
					req := r.request(t, service, r.f.target, params)
					r.publish(t, req)
					if err := sendMOMStreamRequest(t, r, transport, req, r.senderToken); err != nil {
						t.Fatal(err)
					}
					if n := guard.calls.Load(); n != 0 {
						t.Fatalf("inner admission invoked %d times", n)
					}
					if service == momServiceResignFederationExecution {
						if _, err := r.f.deps.Federations.MemberIdentity("catalog-provider", r.f.target); err == nil {
							t.Fatal("actual target resign did not complete")
						}
					} else if !r.f.time.Metrics("catalog-provider", r.f.target).AsynchronousDelivery {
						t.Fatal("actual time service did not execute")
					}
				})
			}
		}
	}
}

func TestMOMSaveDisabledStreamsStillAuthenticateSender(t *testing.T) {
	for _, transport := range []string{"confirmed", "interaction"} {
		t.Run(transport, func(t *testing.T) {
			r := newMOMExecutionRPC(t, momServiceEnableAsynchronousDelivery, func(options *grpcadapter.Options, deps *momServiceAdmissionDependencies) {
				options.CheckpointAdmission, deps.Checkpoint = nil, nil
			})
			req := r.request(t, momServiceEnableAsynchronousDelivery, r.f.target, nil)
			r.publish(t, req)
			if err := sendMOMStreamRequest(t, r, transport, req, r.targetToken); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("recipient token error=%v", err)
			}
			if r.f.time.Metrics("catalog-provider", r.f.target).AsynchronousDelivery {
				t.Fatal("unauthenticated operation mutated target")
			}
		})
	}
}

func TestMOMSaveDisabledStreamsPreserveOrdinaryInteractionAdmission(t *testing.T) {
	for _, transport := range []string{"confirmed", "interaction"} {
		t.Run(transport, func(t *testing.T) {
			r := newMOMExecutionRPC(t, momServiceEnableAsynchronousDelivery, func(options *grpcadapter.Options, deps *momServiceAdmissionDependencies) {
				options.CheckpointAdmission, deps.Checkpoint = nil, nil
			})
			model, err := r.f.deps.FOMs.Get(context.Background(), "catalog-provider")
			if err != nil {
				t.Fatal(err)
			}
			class, ok := model.LookupInteractionClass("Pulse")
			if !ok {
				t.Fatal("ordinary class missing")
			}
			req := &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: "catalog-provider", FederateHandle: uint64(r.f.sender), InteractionClassHandle: uint64(class)}
			if err := sendMOMStreamRequest(t, r, transport, req, r.senderToken); err == nil {
				t.Fatal("ordinary unpublished class accepted")
			}
			r.publish(t, req)
			if err := sendMOMStreamRequest(t, r, transport, req, r.senderToken); err != nil {
				t.Fatal("ordinary nil-checkpoint service failed:", err)
			}
		})
	}
}

func TestMOMObserverReusesSaveDisabledAdmissionIdentity(t *testing.T) {
	f := newMOMExecutionFixture(t, momServiceEnableAsynchronousDelivery)
	manager, err := mom.New(mom.Options{Outbox: f.out})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), momServiceAdmissionKey{}, momServiceAdmissionIdentity{federation: "catalog-provider", sender: f.sender})
	var calls int
	var observationErr error
	observer, _, err := newMOMRuntimeObservers(momObservationDependencies{Manager: manager, Dispatcher: mom.NewDispatcher(manager), FOMs: f.deps.FOMs, Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil }, ResolveCaller: func(context.Context) (core.FederationName, core.FederateHandle, func(), error) {
		calls++
		return "", 0, nil, fmt.Errorf("recursive lifecycle admission")
	}, OnError: func(err error) { observationErr = err }})
	if err != nil || observer == nil {
		t.Fatalf("observer=%v error=%v", observer, err)
	}
	if err := manager.FederateJoined(context.Background(), "catalog-provider", f.sender, "sender", "test"); err != nil {
		t.Fatal(err)
	}
	observer.Observe(ctx, rtiv1.SupportService_GetObjectClassHandle_FullMethodName, &rtiv1.GetObjectClassHandleRequest{FederationName: "catalog-provider", ClassName: "Vehicle"}, nil, nil)
	if calls != 0 || observationErr != nil {
		t.Fatalf("recursive calls=%d observation error=%v", calls, observationErr)
	}
}
