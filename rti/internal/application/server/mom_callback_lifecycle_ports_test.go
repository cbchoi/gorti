package server

import (
	"context"
	"errors"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func TestMOMCallbackLifecyclePortsWithoutMOM(t *testing.T) {
	entries, invoked, validated, authenticated := 0, 0, 0, 0
	_, receipts, err := newMOMRuntimeObservers(momObservationDependencies{
		Authenticate:   func(context.Context, core.FederationName, core.FederateHandle) error { authenticated++; return nil },
		AcquireService: func(context.Context, core.FederationName) (func(), error) { return func() {}, nil },
		ValidateRequiredRecipient: func(_ context.Context, fed core.FederationName, h core.FederateHandle) error {
			validated++
			if fed != "f" || h != 2 {
				return core.ErrFederateNotJoined
			}
			return nil
		},
		CallbackEntry: func(_ context.Context, _ core.FederationName, h core.FederateHandle, e *rtiv1.FederateEvent) error {
			if h != 2 || e.GetSyncAnnounced().Label != testReadyLabel {
				t.Fatal("incorrect recipient entry")
			}
			entries++
			return nil
		},
		CallbackInvoked: func(_ context.Context, _ core.FederationName, h core.FederateHandle, e *rtiv1.FederateEvent, success bool, exception string) error {
			if h != 2 || success || exception != "callback threw" || e.GetSyncAnnounced().Label != testReadyLabel {
				t.Fatal("incorrect callback outcome")
			}
			invoked++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SyncAnnounced{SyncAnnounced: &rtiv1.SynchronizationPointAnnounced{Label: testReadyLabel}}}
	ticket, commit, err := receipts.PrepareRequired(context.Background(), "f", 2, event)
	if err != nil || len(ticket) == 0 {
		t.Fatal(err)
	}
	if validated != 1 || authenticated != 0 {
		t.Fatal("server-side required callback impersonated recipient")
	}
	if err := receipts.ReportEntry(context.Background(), "f", 2, ticket); err != nil {
		t.Fatal(err)
	}
	commit(true)
	if err := receipts.Report(context.Background(), "f", 2, ticket, false, "callback threw"); err != nil {
		t.Fatal(err)
	}
	if entries != 1 || invoked != 1 {
		t.Fatalf("entry=%d invoked=%d", entries, invoked)
	}
}

func TestMOMCallbackLifecycleRequiredRetryDoesNotRepeatOptionalTelemetry(t *testing.T) {
	f := newMOMExecutionFixture(t, momServiceEnableAsynchronousDelivery)
	manager, err := mom.New(mom.Options{Outbox: f.out})
	if err != nil {
		t.Fatal(err)
	}
	f.deps.Manager = manager
	f.dispatcher = mom.NewDispatcher(manager)
	cause := errors.New("synchronization callback outcome failure")
	calls, telemetryErrors := 0, 0
	_, receipts, err := newMOMRuntimeObservers(momObservationDependencies{Manager: f.deps.Manager, Dispatcher: f.dispatcher, FOMs: f.deps.FOMs,
		Authenticate:              func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		ValidateRequiredRecipient: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		CallbackInvoked: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			calls++
			if calls == 1 {
				return cause
			}
			return nil
		},
		OnError: func(error) { telemetryErrors++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FederateJoined(context.Background(), "catalog-provider", f.target, "target", "test"); err != nil {
		t.Fatal(err)
	}
	manager.SetServiceReporting("catalog-provider", f.target, true)
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SyncAnnounced{SyncAnnounced: &rtiv1.SynchronizationPointAnnounced{Label: testReadyLabel}}}
	ticket, commit, err := receipts.PrepareRequired(context.Background(), "catalog-provider", f.target, event)
	if err != nil {
		t.Fatal(err)
	}
	if err := receipts.ReportEntry(context.Background(), "catalog-provider", f.target, ticket); err != nil {
		t.Fatal(err)
	}
	commit(true)
	err = receipts.Report(context.Background(), "catalog-provider", f.target, ticket, true, "")
	if !errors.Is(err, cause) || calls != 1 {
		t.Fatalf("synchronization outcome lost: calls=%d err=%v", calls, err)
	}
	if telemetryErrors != 0 {
		t.Fatal("telemetry ran before required lifecycle outcome succeeded")
	}
	if err := receipts.Report(context.Background(), "catalog-provider", f.target, ticket, true, ""); err != nil {
		t.Fatal("optional MOM error prevented required completion:", err)
	}
	if calls != 2 || telemetryErrors != 1 {
		t.Fatalf("lifecycle=%d telemetry errors=%d", calls, telemetryErrors)
	}
	if err := receipts.Report(context.Background(), "catalog-provider", f.target, ticket, true, ""); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || telemetryErrors != 1 {
		t.Fatal("completed receipt replayed lifecycle or optional MOM effects")
	}
}

func TestMOMObserverReusesAuthenticatedCheckpointLease(t *testing.T) {
	f := newMOMExecutionFixture(t, momServiceFederateSaveBegun)
	manager, err := mom.New(mom.Options{Outbox: f.out})
	if err != nil {
		t.Fatal(err)
	}
	f.deps.Manager = manager
	f.dispatcher = mom.NewDispatcher(manager)
	var observationErr error
	observer, _, err := newMOMRuntimeObservers(momObservationDependencies{Manager: f.deps.Manager, Dispatcher: f.dispatcher, FOMs: f.deps.FOMs,
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		ResolveCaller: func(context.Context) (core.FederationName, core.FederateHandle, func(), error) {
			t.Fatal("observer reacquired checkpoint lease")
			return "", 0, nil, nil
		},
		OnError: func(err error) { observationErr = err },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FederateJoined(context.Background(), "catalog-provider", f.sender, "sender", "test"); err != nil {
		t.Fatal(err)
	}
	release := f.save.AcquireCheckpoint("catalog-provider")
	defer release()
	ctx := context.WithValue(context.Background(), momServiceAdmissionKey{}, momServiceAdmissionIdentity{federation: "catalog-provider", sender: f.sender, service: "HLAfederateSaveBegun", checkpointHeld: true})
	observer.Observe(ctx, rtiv1.SupportService_GetObjectClassHandle_FullMethodName, &rtiv1.GetObjectClassHandleRequest{FederationName: "catalog-provider", ClassName: "Vehicle"}, nil, nil)
	if observationErr != nil {
		t.Fatal(observationErr)
	}
}
