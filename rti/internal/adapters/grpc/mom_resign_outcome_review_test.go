package grpc

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestR4MOMResignOutcomePreservesResultAndSuppressesDuplicate(t *testing.T) {
	for _, failed := range []bool{false, true} {
		for _, telemetryFailed := range []bool{false, true} {
			t.Run(map[bool]string{false: "success", true: "failure"}[failed]+"/telemetry-"+map[bool]string{false: "ok", true: "failed"}[telemetryFailed], func(t *testing.T) {
				var observations []ServiceObservation
				var order []string
				telemetryErr := errors.New("injected telemetry failure")
				observer := NewMOMObserver(MOMObserverOptions{
					ResolveCaller: func(context.Context) (core.FederationName, core.FederateHandle, func(), error) {
						t.Error("completed lifecycle outcome tried to re-resolve its caller")
						return "", 0, nil, errors.New("unexpected reentry")
					},
					Observe: func(_ context.Context, actual ServiceObservation) error {
						observations = append(observations, actual)
						order = append(order, "report")
						if telemetryFailed {
							return telemetryErr
						}
						return nil
					},
					OnError: func(err error) {
						if err != telemetryErr {
							t.Errorf("unexpected observation error: %v", err)
						}
						order = append(order, "telemetry-error")
					},
				})
				request := &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: "f", FederateHandle: 11, Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION}
				var outcome error
				if failed {
					outcome = core.ErrFederateOwnsAttributes
				}
				serviceErr := errToStatus(context.Background(), outcome)
				response := &rtiv1.Empty{}
				signed := metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, "completed-session"))
				got, err := observer.UnaryInterceptor()(signed, request, &stdgrpc.UnaryServerInfo{FullMethod: rtiv1.FederationService_ResignFederation_FullMethodName}, func(ctx context.Context, _ any) (any, error) {
					// The actual membership/action can differ from the wire request after restore.
					observer.ObserveResignOutcome(ctx, "f", 22, core.ResignActionDeleteThenDivest, outcome)
					observer.ObserveResignOutcome(ctx, "f", 22, core.ResignActionDeleteThenDivest, outcome)
					order = append(order, "cleanup")
					return response, serviceErr
				})
				if err != serviceErr || got != response {
					t.Fatalf("telemetry changed service result: %v %v", got, err)
				}
				if len(observations) != 1 {
					t.Fatalf("reports=%d, want exactly one", len(observations))
				}
				actual := observations[0]
				wantArgs := momArguments(&rtiv1.ResignFederationRequest{Action: coreResignActionToProto(core.ResignActionDeleteThenDivest)})
				if actual.Federate != 22 || actual.Federation != "f" || actual.Service != "resignFederationExecution" || actual.Success == failed || !reflect.DeepEqual(actual.SuppliedArguments, wantArgs) {
					t.Fatalf("wrong actual outcome: %+v", actual)
				}
				if failed && (actual.Exception != serviceErr.Error() || len(actual.ReturnedArguments) != 0) {
					t.Fatalf("wrong error payload: %+v", actual)
				}
				wantOrder := []string{"report", "cleanup"}
				if telemetryFailed {
					wantOrder = []string{"report", "telemetry-error", "cleanup"}
				}
				if !reflect.DeepEqual(order, wantOrder) {
					t.Fatalf("order=%v, want %v", order, wantOrder)
				}
				if request.FederateHandle != 11 || request.Action != rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION {
					t.Fatal("outcome observer mutated wire request")
				}
			})
		}
	}
}

func TestR4MOMResignFallbackResolvesOnlySignedCaller(t *testing.T) {
	for _, mode := range []string{resignModeAuthenticated, "no-resolver", "resolver-error", "unresolved", resignModeLegacy} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			if mode != resignModeLegacy {
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(session.MetadataKey, "signed-session"))
			}
			var observations []ServiceObservation
			resolved, released, observationErrors := 0, 0, 0
			handlerReturned, held := false, false
			resolveErr := errors.New("untrusted session")
			opts := MOMObserverOptions{
				ResolveCaller: func(context.Context) (core.FederationName, core.FederateHandle, func(), error) {
					resolved++
					if !handlerReturned {
						t.Error("resolver entered before resign handler returned")
					}
					held = true
					release := func() { held = false; released++ }
					if mode == "resolver-error" {
						return resignCallerFederation, 22, release, resolveErr
					}
					if mode == "unresolved" {
						return "", 0, release, nil
					}
					return resignCallerFederation, 22, release, nil
				},
				CaptureArguments: func(f core.FederationName, h core.FederateHandle) bool {
					if mode == resignModeAuthenticated && (f != resignCallerFederation || h != 22 || !held) {
						t.Errorf("argument capture used unverified identity: %s/%d held=%v", f, h, held)
					}
					return true
				},
				Observe: func(_ context.Context, value ServiceObservation) error {
					if mode == resignModeAuthenticated && !held {
						t.Error("caller lease released before report")
					}
					observations = append(observations, value)
					return nil
				},
				OnError: func(err error) {
					observationErrors++
					if err != resolveErr {
						t.Errorf("unexpected resolver failure: %v", err)
					}
				},
			}
			if mode == "no-resolver" {
				opts.ResolveCaller = nil
			}
			observer := NewMOMObserver(opts)
			request := &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: "target-federation", FederateHandle: 11, Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION}
			want := status.Error(codes.PermissionDenied, "request target is not caller")
			result, err := observer.UnaryInterceptor()(ctx, request, &stdgrpc.UnaryServerInfo{FullMethod: rtiv1.FederationService_ResignFederation_FullMethodName}, func(context.Context, any) (any, error) {
				handlerReturned = true
				return nil, want
			})
			if err != want || result != nil || held || resolved != released {
				t.Fatalf("result changed or lease leaked: result=%v err=%v held=%v resolved=%d released=%d", result, err, held, resolved, released)
			}
			wantResolved := 1
			if mode == resignModeLegacy || mode == "no-resolver" {
				wantResolved = 0
			}
			if resolved != wantResolved || (observationErrors == 1) != (mode == "resolver-error") {
				t.Fatalf("resolver calls=%d errors=%d", resolved, observationErrors)
			}
			if mode == resignModeAuthenticated || mode == resignModeLegacy {
				fed, handle := core.FederationName(resignCallerFederation), core.FederateHandle(22)
				if mode == resignModeLegacy {
					fed, handle = "target-federation", 11
				}
				if len(observations) != 1 || observations[0].Federation != fed || observations[0].Federate != handle || observations[0].Success || observations[0].Exception != want.Error() {
					t.Fatalf("wrong failure attribution: %+v", observations)
				}
			} else if len(observations) != 0 {
				t.Fatalf("unverified target was reported: %+v", observations)
			}
		})
	}
}

func TestR4MOMResignFallbackCarriesCheckpointAdmission(t *testing.T) {
	probe := new(checkpointProbe)
	server := &Server{checkpointAdmission: probe}
	observer := NewMOMObserver(MOMObserverOptions{
		ResolveCaller: func(ctx context.Context) (core.FederationName, core.FederateHandle, func(), error) {
			if fed, held := CheckpointAdmissionFederation(ctx); !held || fed != "f" {
				t.Errorf("post-handler resolver would reenter checkpoint admission: federation=%q held=%v", fed, held)
			}
			probe.record("resolve")
			return "f", 22, func() { probe.record("caller-release") }, nil
		},
		Observe: func(context.Context, ServiceObservation) error { probe.record("report"); return nil },
	})
	signed := metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, "signed-session"))
	info := &stdgrpc.UnaryServerInfo{FullMethod: rtiv1.FederationService_ResignFederation_FullMethodName}
	want := status.Error(codes.PermissionDenied, "wrong target")
	_, err := server.UnaryMembershipInterceptor()(signed, &rtiv1.ResignFederationRequest{FederationName: "f", FederateHandle: 11}, info, func(ctx context.Context, req any) (any, error) {
		return observer.UnaryInterceptor()(ctx, req, info, func(context.Context, any) (any, error) {
			probe.record("handler")
			return nil, want
		})
	})
	if err != want {
		t.Fatal("resign result changed", err)
	}
	probe.assertSteps(t, "service:f", "handler", "resolve", "report", "caller-release", "service-release")
}

type r4ObserverPreparation struct{ outcome error }

func (p r4ObserverPreparation) Commit() error { return p.outcome }
func (p r4ObserverPreparation) Release()      {}

func TestR4MOMResignRealManagerReportsRestoredHandle(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "commit-error"}[failed], func(t *testing.T) {
			ctx := context.Background()
			const fed core.FederationName = "rebound"
			var actual []ServiceObservation
			observer := NewMOMObserver(MOMObserverOptions{Observe: func(_ context.Context, value ServiceObservation) error {
				actual = append(actual, value)
				return nil
			}})
			var outcome error
			if failed {
				outcome = core.ErrFederateOverflow
			}
			manager, err := federation.New(federation.Options{
				Clock: core.NewManualClock(time.Unix(0, 0)), FOMs: adminFOMs{}, GenerationEpoch: 9,
				PrepareResign: func(context.Context, core.FederationName, core.FederateHandle, core.ResignAction) (federation.ResignPreparation, error) {
					return r4ObserverPreparation{outcome: outcome}, nil
				},
				OnFederateResignOutcome: observer.ObserveResignOutcome,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.CreateFederation(ctx, core.CreateFederationRequest{Name: fed}); err != nil {
				t.Fatal(err)
			}
			join := func(name string) core.FederateHandle {
				t.Helper()
				h, err := manager.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: name})
				if err != nil {
					t.Fatal(err)
				}
				return h
			}
			a, b := join("a"), join("b")
			identity, err := manager.MemberIdentity(fed, a)
			if err != nil {
				t.Fatal(err)
			}
			request := &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(a), Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION}
			_, err = observer.UnaryInterceptor()(ctx, request, &stdgrpc.UnaryServerInfo{FullMethod: rtiv1.FederationService_ResignFederation_FullMethodName}, func(ctx context.Context, _ any) (any, error) {
				// Exercise the identity API's documented rebind boundary directly.
				r, err := manager.PrepareRestoreIdentity(fed, identity.Generation, []federation.RestoreIdentity{
					{Name: "a", CurrentHandle: a, SavedHandle: b},
					{Name: "b", CurrentHandle: b, SavedHandle: a},
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := r.Commit(); err != nil {
					r.Release()
					t.Fatal(err)
				}
				r.Release()
				return &rtiv1.Empty{}, manager.ResignFederationIdentity(ctx, identity, core.ResignActionNoAction)
			})
			if err != outcome || len(actual) != 1 || actual[0].Federate != b || actual[0].Success == failed {
				t.Fatalf("rebound actual result=%v, observations=%+v", err, actual)
			}
			if current, err := manager.MemberIdentity(fed, a); err != nil || current.Name != "b" {
				t.Fatalf("wire handle's new owner was affected: %+v %v", current, err)
			}
			if err := manager.ValidateMember(fed, b); !errors.Is(err, core.ErrFederateNotJoined) {
				t.Fatalf("actual retired handle remains active: %v", err)
			}
		})
	}
}

func TestR4MOMResignContextOwnershipAndFallback(t *testing.T) {
	var actual []ServiceObservation
	observe := func(_ context.Context, value ServiceObservation) error { actual = append(actual, value); return nil }
	owner := NewMOMObserver(MOMObserverOptions{Observe: observe})
	other := NewMOMObserver(MOMObserverOptions{Observe: observe})
	request := &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: "f", FederateHandle: 11, Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION}
	// Automatic/admin lifecycle hooks do not imply a federate service invocation.
	owner.ObserveResignOutcome(context.Background(), "f", 11, core.ResignActionNoAction, nil)
	if len(actual) != 0 {
		t.Fatal("unexecuted federate service was reported")
	}
	want := status.Error(codes.InvalidArgument, "rejected before mutation")
	_, err := owner.UnaryInterceptor()(context.Background(), request, &stdgrpc.UnaryServerInfo{FullMethod: rtiv1.FederationService_ResignFederation_FullMethodName}, func(ctx context.Context, _ any) (any, error) {
		other.ObserveResignOutcome(ctx, "f", 11, core.ResignActionNoAction, nil)
		owner.ObserveResignOutcome(ctx, "other-federation", 11, core.ResignActionNoAction, nil)
		owner.ObserveResignOutcome(ctx, "f", 0, core.ResignActionNoAction, nil)
		if len(actual) != 0 {
			t.Error("invalid context owner/federation/handle emitted an outcome")
		}
		return nil, want
	})
	if err != want || len(actual) != 1 || actual[0].Success || actual[0].Exception != want.Error() || actual[0].Federate != 11 {
		t.Fatalf("early failure not observed once: result=%v observations=%+v", err, actual)
	}
}

const (
	resignCallerFederation = "caller-federation"
)
