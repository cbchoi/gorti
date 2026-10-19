package server

import (
	"context"
	"encoding/binary"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/lifecycle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type r4CommitFaultOutbox struct {
	*delivery.MultiOutbox
	fault      error
	deliveries int
	reserved   []core.OutboxDelivery
}

type r4CommitFaultReservation struct {
	core.OutboxReservation
	fault error
}

func (o *r4CommitFaultOutbox) Reserve(ctx context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	r, err := o.MultiOutbox.Reserve(ctx, fed, deliveries)
	if err != nil {
		return nil, err
	}
	o.deliveries = len(deliveries)
	o.reserved = slices.Clone(deliveries)
	return r4CommitFaultReservation{OutboxReservation: r, fault: o.fault}, nil
}

func (r r4CommitFaultReservation) Commit() error {
	if err := r.OutboxReservation.Commit(); err != nil {
		return err
	}
	return r.fault
}

func TestR4MOMResignDeleteDivestCommitLocksAndErrors(t *testing.T) {
	for _, tc := range []struct {
		action    core.ResignAction
		callbacks int
	}{
		{core.ResignActionDeleteThenDivest, 1},
		{core.ResignActionDeleteThenDivest, 2},
		{core.ResignActionUnconditionallyDivestAttributes, 1},
		{core.ResignActionUnconditionallyDivestAttributes, 2},
	} {
		action := tc.action
		for _, failure := range []string{auditReplayDisabledName, testCommitLabel, "telemetry"} {
			name := map[core.ResignAction]string{core.ResignActionDeleteThenDivest: "delete", core.ResignActionUnconditionallyDivestAttributes: "divest"}[action]
			t.Run(name+"/"+map[int]string{1: "single", 2: "multiple"}[tc.callbacks]+"/"+failure, func(t *testing.T) {
				ctx := context.Background()
				const fed core.FederationName = "catalog-provider"
				d, fom, a, b, _ := newMOMCatalogDeps(t)
				out := &r4CommitFaultOutbox{MultiOutbox: delivery.NewWithBatch(16, 1, 0)}
				var err error
				d.Manager, err = mom.New(mom.Options{Outbox: out})
				if err != nil {
					t.Fatal(err)
				}
				out.Bind(fed, b)
				t.Cleanup(func() { out.UnbindFederation(fed) })
				ch, release, err := out.Subscribe(ctx, fed, b)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = release() })
				fault := errors.New("injected post-commit delivery failure")
				if failure == testCommitLabel {
					out.fault = fault
				}
				class, _ := fom.LookupObjectClass("Vehicle")
				attr, _ := fom.LookupAttribute(class, "value")
				privilege, _ := fom.LookupAttribute(class, "HLAprivilegeToDeleteObject")
				for _, h := range []core.FederateHandle{a, b} {
					if err := d.Declarations.PublishObjectClassAttributes(ctx, fed, h, class, []core.AttributeHandle{attr, privilege}); err != nil {
						t.Fatal(err)
					}
				}
				if err := d.Declarations.SubscribeObjectClassAttributes(ctx, fed, b, class, []core.AttributeHandle{attr}); err != nil {
					t.Fatal(err)
				}
				obj, _, err := d.Objects.Register(ctx, fed, a, class, "")
				if err != nil {
					t.Fatal(err)
				}
				var secondObject core.ObjectHandle
				acquired := []core.AttributeHandle{attr}
				if tc.callbacks == 2 {
					if action == core.ResignActionDeleteThenDivest {
						secondObject, _, err = d.Objects.Register(ctx, fed, a, class, "")
						if err != nil {
							t.Fatal(err)
						}
					} else {
						acquired = append(acquired, privilege)
					}
				}
				if err := d.Ownership.Acquire(ctx, fed, b, obj, acquired, nil); err != nil {
					t.Fatal(err)
				}
				var observer *grpcadapter.MOMObserver
				outcomes, cleanups, observationErrors := 0, 0, 0
				// Use the real composed preparation: ownership, objects, then the
				// production reservation must all release locks before MOM runs.
				manager, err := federation.New(federation.Options{
					Clock: core.NewManualClock(time.Unix(0, 0)), FOMs: d.FOMs,
					PrepareResign: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, action core.ResignAction) (federation.ResignPreparation, error) {
						return lifecycle.PrepareResign(ctx, fed, h, action, d.Objects, d.Ownership, out)
					},
					OnFederateResignOutcome: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, action core.ResignAction, outcome error) {
						outcomes++
						if slices.Contains(d.Federations.MembersOf(fed), h) || cleanups != 0 {
							t.Error("outcome was not between roster removal and derived cleanup")
						}
						if _, present := d.Manager.QueryFederateAttributes(fed, h); !present {
							t.Error("MOM subject already retired at outcome")
						}
						owner, owned := d.Ownership.QueryOwnership(fed, obj, attr)
						objectClass := d.Objects.ClassOf(fed, obj)
						if action == core.ResignActionDeleteThenDivest && (owned || objectClass != 0) {
							t.Error("delete state not committed before observation")
						}
						if secondObject != 0 && d.Objects.ClassOf(fed, secondObject) != 0 {
							t.Error("second deletion not committed before observation")
						}
						if action == core.ResignActionUnconditionallyDivestAttributes && (!owned || owner != b || objectClass != class) {
							t.Error("divest state not committed before observation")
						}
						if outcome != out.fault {
							t.Errorf("outcome=%v, want %v", outcome, out.fault)
						}
						observer.ObserveResignOutcome(ctx, fed, h, action, outcome)
					},
					OnFederateResigned: func(ctx context.Context, fed core.FederationName, h core.FederateHandle) {
						cleanups++
						if err := d.Manager.FederateResigned(ctx, fed, h); err != nil {
							t.Error(err)
						}
						d.Declarations.OnFederateResign(fed, h)
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := manager.CreateFederation(ctx, core.CreateFederationRequest{Name: fed}); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"a", "b"} {
					if _, err := manager.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: name}); err != nil {
						t.Fatal(err)
					}
				}
				d.Federations = manager
				dispatcher := mom.NewDispatcher(d.Manager)
				if err := configureMOMCatalog(dispatcher, d); err != nil {
					t.Fatal(err)
				}
				emitter := mom.NewProductionEmitter(out)
				dispatcher.SetEmitter(func(ctx context.Context, fed core.FederationName, h core.FederateHandle, report mom.ResponseInteraction, fom core.FOMHandle, names core.FOMHandleNameLookup) error {
					if err := emitter(ctx, fed, h, report, fom, names); err != nil {
						return err
					}
					if failure == "telemetry" {
						return errors.New("injected telemetry failure after delivery")
					}
					return nil
				})
				observer, _, err = newMOMRuntimeObservers(momObservationDependencies{
					Manager: d.Manager, Dispatcher: dispatcher, FOMs: d.FOMs,
					Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
					OnError:      func(error) { observationErrors++ },
				})
				if err != nil {
					t.Fatal(err)
				}
				for _, h := range []core.FederateHandle{a, b} {
					if err := d.Manager.FederateJoined(ctx, fed, h, "member", "test"); err != nil {
						t.Fatal(err)
					}
				}
				d.Manager.SetServiceReporting(fed, a, true)
				d.Manager.SetExceptionReporting(fed, a, true)
				for _, name := range []string{mom.ClassReportServiceInvocation, mom.ClassReportException} {
					class, _ := fom.LookupInteractionClass(name)
					if err := d.Declarations.SubscribeInteractionClass(ctx, fed, b, class); err != nil {
						t.Fatal(err)
					}
				}
				request := &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(a)}
				_, err = observer.UnaryInterceptor()(ctx, request, &stdgrpc.UnaryServerInfo{FullMethod: rtiv1.FederationService_ResignFederation_FullMethodName}, func(ctx context.Context, _ any) (any, error) {
					return &rtiv1.Empty{}, manager.ResignFederation(ctx, fed, a, action)
				})
				expected := tc.callbacks
				if action == core.ResignActionDeleteThenDivest {
					// One pending acquisition must terminate before the removals,
					// in the same reservation, even for a post-commit fault.
					expected++
				}
				if err != out.fault || outcomes != 1 || cleanups != 1 || out.deliveries != expected {
					t.Fatalf("result=%v outcomes=%d cleanup=%d reserved=%d", err, outcomes, cleanups, out.deliveries)
				}
				if action == core.ResignActionDeleteThenDivest {
					terminal, ok := out.reserved[0].Event.(*ownership.CallbackEvent)
					if !ok || terminal.Kind != ownership.CallbackUnavailable || terminal.Object != obj || out.reserved[0].Recipient != b {
						t.Fatal("deletion did not reserve acquisition termination first")
					}
					for _, delivery := range out.reserved[1:] {
						removal, ok := delivery.Event.(*object.CallbackEvent)
						if !ok || removal.Kind != object.CallbackRemove || delivery.Recipient != b {
							t.Fatal("terminal outcome was not followed by object removals in one fanout")
						}
					}
				}
				if _, present := d.Manager.QueryFederateAttributes(fed, a); present {
					t.Fatal("retired MOM subject retained")
				}
				if (observationErrors != 0) != (failure == "telemetry") {
					t.Fatalf("telemetry errors=%d", observationErrors)
				}
				serviceReports, exceptionReports := 0, 0
				serviceClass, _ := fom.LookupInteractionClass(mom.ClassReportServiceInvocation)
				exceptionClass, _ := fom.LookupInteractionClass(mom.ClassReportException)
				for _, event := range drainScheduledEvents(ch) {
					report, ok := event.(*mom.InteractionEvent)
					if !ok {
						continue
					}
					if report.Class == serviceClass {
						serviceReports++
						success, _ := fom.LookupParameter(report.Class, "HLAsuccessIndicator")
						if (binary.BigEndian.Uint32(report.Parameters[success]) == 1) != (failure != testCommitLabel) {
							t.Fatal("report success disagrees with actual commit result")
						}
						exception, _ := fom.LookupParameter(report.Class, "HLAexception")
						if failure == testCommitLabel && !strings.Contains(catalogRPCString(t, report.Parameters[exception]), fault.Error()) {
							t.Fatal("report lost commit error")
						}
					} else if report.Class == exceptionClass {
						exceptionReports++
					}
				}
				wantExceptions := 0
				if failure == testCommitLabel {
					wantExceptions = 1
				}
				if serviceReports != 1 || exceptionReports != wantExceptions {
					t.Fatalf("reports=%d exceptions=%d, want 1/%d", serviceReports, exceptionReports, wantExceptions)
				}
			})
		}
	}
}

func TestR4MOMRejectedSignedResignDoesNotReportForOtherMembership(t *testing.T) {
	const fed core.FederationName = "r4-rejected-subject"
	ctx, srv, conn, _ := momCompletionReviewServer(t, fed)
	generation := momCompletionReviewCreate(ctx, t, conn, string(fed), "Parent")
	a := momCompletionReviewJoin(ctx, t, conn, string(fed), "caller", "", generation)
	b := momCompletionReviewJoin(ctx, t, conn, string(fed), "other", "", generation)
	retired := momCompletionReviewJoin(ctx, t, conn, string(fed), "rejoined", "", generation)
	if err := srv.fedMgr.ResignFederation(ctx, fed, core.FederateHandle(retired.FederateHandle), core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	replacement := momCompletionReviewJoin(ctx, t, conn, string(fed), "rejoined", "", generation)
	fom, err := srv.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	class, _ := fom.LookupInteractionClass(mom.ClassReportServiceInvocation)
	exceptionClass, _ := fom.LookupInteractionClass(mom.ClassReportException)
	for _, reportClass := range []core.InteractionClassHandle{class, exceptionClass} {
		if err := srv.declMgr.SubscribeInteractionClass(ctx, fed, core.FederateHandle(a.FederateHandle), reportClass); err != nil {
			t.Fatal(err)
		}
	}
	ch, release, err := srv.outbox.Subscribe(ctx, fed, core.FederateHandle(a.FederateHandle))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	manager := srv.momMgr.(*mom.Manager)
	for _, h := range []uint64{b.FederateHandle, replacement.FederateHandle} {
		manager.SetServiceReporting(fed, core.FederateHandle(h), true)
		manager.SetExceptionReporting(fed, core.FederateHandle(h), true)
	}
	forged := append([]byte(nil), a.SessionToken...)
	forged[0] ^= 1
	for _, tc := range []struct {
		name          string
		token         []byte
		target        uint64
		callerEnabled bool
		wantSubject   uint64
		wantCode      codes.Code
	}{
		{"caller-disabled", a.SessionToken, b.FederateHandle, false, 0, codes.PermissionDenied},
		{"caller-enabled", a.SessionToken, b.FederateHandle, true, a.FederateHandle, codes.PermissionDenied},
		{"forged", forged, b.FederateHandle, true, 0, codes.PermissionDenied},
		{"malformed", []byte("bad"), b.FederateHandle, true, 0, codes.Unauthenticated},
		{"stale", retired.SessionToken, replacement.FederateHandle, true, 0, codes.PermissionDenied},
		{"legacy", nil, b.FederateHandle, false, b.FederateHandle, codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager.SetServiceReporting(fed, core.FederateHandle(a.FederateHandle), tc.callerEnabled)
			manager.SetExceptionReporting(fed, core.FederateHandle(a.FederateHandle), tc.callerEnabled)
			requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			action := rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION
			if tc.token != nil {
				requestCtx = metadata.AppendToOutgoingContext(requestCtx, session.MetadataKey, string(tc.token))
			} else {
				action = rtiv1.ResignAction_RESIGN_ACTION_UNSPECIFIED
			}
			_, err := rtiv1.NewFederationServiceClient(conn).ResignFederation(requestCtx, &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: tc.target, Action: action})
			if status.Code(err) != tc.wantCode {
				t.Fatalf("rejected resign=%v, want %s", err, tc.wantCode)
			}
			for _, h := range []uint64{a.FederateHandle, b.FederateHandle, replacement.FederateHandle} {
				if err := srv.fedMgr.ValidateMember(fed, core.FederateHandle(h)); err != nil {
					t.Fatal("rejected request changed membership", err)
				}
			}
			counts := map[core.InteractionClassHandle]int{}
			for _, event := range drainScheduledEvents(ch) {
				wire, err := grpcadapter.EncodeOutboundEvent(event)
				if err != nil {
					t.Fatal(err)
				}
				report := wire.GetReceive()
				if report == nil {
					continue
				}
				reportClass := core.InteractionClassHandle(report.InteractionClassHandle)
				if reportClass != class && reportClass != exceptionClass {
					continue
				}
				counts[reportClass]++
				subject, _ := fom.LookupParameter(reportClass, "HLAfederate")
				encoded := report.Parameters[uint64(subject)]
				if len(encoded) != 12 || tc.wantSubject == 0 || binary.BigEndian.Uint64(encoded[4:]) != tc.wantSubject {
					t.Fatalf("rejected signed resign reported unverified/wrong subject: %x, want %d", encoded, tc.wantSubject)
				}
				service, _ := fom.LookupParameter(reportClass, "HLAservice")
				if got := catalogRPCString(t, report.Parameters[uint64(service)]); got != momServiceResignFederationExecution {
					t.Fatalf("wrong service: %s", got)
				}
				if reportClass == class {
					success, _ := fom.LookupParameter(class, "HLAsuccessIndicator")
					if binary.BigEndian.Uint32(report.Parameters[uint64(success)]) != 0 {
						t.Fatal("rejected resign reported success")
					}
				}
			}
			want := 0
			if tc.wantSubject != 0 {
				want = 1
			}
			if counts[class] != want || counts[exceptionClass] != want {
				t.Fatalf("service/exception reports=%d/%d, want %d each", counts[class], counts[exceptionClass], want)
			}
		})
	}
}

func TestR4MOMResignFallbackReusesProductionCallerAdmission(t *testing.T) {
	const fed core.FederationName = "r4-fallback-admission"
	ctx, srv, conn, _ := momCompletionReviewServer(t, fed)
	generation := momCompletionReviewCreate(ctx, t, conn, string(fed), "Parent")
	a := momCompletionReviewJoin(ctx, t, conn, string(fed), "caller", "", generation)
	b := momCompletionReviewJoin(ctx, t, conn, string(fed), "target", "", generation)
	adapter, err := grpcadapter.NewServer(grpcadapter.Options{
		Federations: srv.fedMgr, Declarations: srv.declMgr, Objects: srv.objReg, Outbox: srv.outbox,
		CheckpointAdmission: srv.saveMgr.(grpcadapter.CheckpointAdmission),
	})
	if err != nil {
		t.Fatal(err)
	}
	resolve := newMOMCallerResolver(srv.sessions, srv.fedMgr, func(core.FederationName) func() {
		// Fail without taking a recursive read lock: a queued restore writer
		// would otherwise keep this test and the original RPC blocked forever.
		t.Error("resolver attempted to reacquire held checkpoint admission")
		return func() {}
	})
	reports := 0
	observer := grpcadapter.NewMOMObserver(grpcadapter.MOMObserverOptions{
		ResolveCaller: resolve,
		Observe: func(_ context.Context, actual grpcadapter.ServiceObservation) error {
			reports++
			if actual.Federation != fed || actual.Federate != core.FederateHandle(a.FederateHandle) || actual.Success {
				t.Errorf("wrong authenticated caller: %+v", actual)
			}
			return nil
		},
	})
	signed := metadata.NewIncomingContext(ctx, metadata.Pairs(session.MetadataKey, string(a.SessionToken)))
	info := &stdgrpc.UnaryServerInfo{FullMethod: rtiv1.FederationService_ResignFederation_FullMethodName}
	want := status.Error(codes.PermissionDenied, "wrong target")
	_, err = adapter.UnaryMembershipInterceptor()(signed, &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: b.FederateHandle}, info, func(ctx context.Context, req any) (any, error) {
		return observer.UnaryInterceptor()(ctx, req, info, func(context.Context, any) (any, error) { return nil, want })
	})
	if err != want || reports != 1 {
		t.Fatalf("post-handler resolver result=%v reports=%d", err, reports)
	}
	if err := srv.fedMgr.ResignFederation(ctx, fed, core.FederateHandle(a.FederateHandle), core.ResignActionNoAction); err != nil {
		t.Fatal("resolved caller lease leaked", err)
	}
}
