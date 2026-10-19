package grpc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestCallbackReceiptReportBeforeSendCommit(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int32
	r, err := NewCallbackReceipts(CallbackReceiptOptions{
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		Observe: func(_ context.Context, _ core.FederationName, _ core.FederateHandle, e *rtiv1.FederateEvent, success bool, exception string) error {
			if e.GetDiscover().GetObjectClassHandle() != 17 || success || exception != "user failure" {
				t.Errorf("wrong actual callback: %v %v %q", e, success, exception)
			}
			calls.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Discover{Discover: &rtiv1.DiscoverObjectInstance{ObjectHandle: 11, ObjectClassHandle: 17}}}
	ticket, commit, err := r.Prepare(ctx, "f", 1, event)
	if err != nil || len(ticket) != 32 {
		t.Fatalf("receipt=%x err=%v", ticket, err)
	}
	event.GetDiscover().ObjectClassHandle = 99
	if err := r.Report(ctx, "f", 1, ticket, false, "user failure"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("uncommitted send counted")
	}
	commit(true)
	if calls.Load() != 1 {
		t.Fatal("committed callback not counted")
	}
	if err := r.Report(ctx, "f", 1, ticket, false, "user failure"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate counted")
	}
	if err := r.Report(ctx, "f", 1, ticket, true, ""); err == nil {
		t.Fatal("conflicting duplicate accepted")
	}
}

func TestCallbackReceiptRollbackOverflowAndLifecycle(t *testing.T) {
	ctx := context.Background()
	var calls, unavailable atomic.Int32
	r, _ := NewCallbackReceipts(CallbackReceiptOptions{MaxPending: 1, MaxCompleted: 2,
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			calls.Add(1)
			return nil
		},
		OnUnavailable: func(core.FederationName, core.FederateHandle, error) { unavailable.Add(1) },
	})
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: 4}}}
	ticket, commit, err := r.Prepare(ctx, "f", 1, event)
	if err != nil || len(ticket) != 32 {
		t.Fatal("receipt missing", err)
	}
	if next, _, err := r.Prepare(ctx, "f", 1, event); err != nil || len(next) != 0 || unavailable.Load() != 1 {
		t.Fatal("overflow failed delivery or fabricated coverage", err)
	}
	if err := r.Report(ctx, "f", 1, ticket, true, ""); err != nil {
		t.Fatal(err)
	}
	commit(false)
	if calls.Load() != 0 {
		t.Fatal("failed send counted")
	}
	if err := r.Report(ctx, "f", 1, ticket, true, ""); err == nil {
		t.Fatal("rolled back ticket valid")
	}
	ticket, commit, _ = r.Prepare(ctx, "f", 1, event)
	commit(true)
	if err := r.Report(ctx, "f", 2, ticket, true, ""); err == nil {
		t.Fatal("other member ticket accepted")
	}
	r.ClearFederate("f", 1)
	if err := r.Report(ctx, "f", 1, ticket, true, ""); err == nil {
		t.Fatal("resigned ticket accepted")
	}
	ticket, commit, _ = r.Prepare(ctx, "f", 1, event)
	r.ClearFederation("f")
	commit(true)
	if err := r.Report(ctx, "f", 1, ticket, true, ""); err == nil {
		t.Fatal("restored ticket accepted")
	}
}

func TestCallbackReceiptAuthenticationPrecedesLookup(t *testing.T) {
	want := errors.New("denied")
	r, _ := NewCallbackReceipts(CallbackReceiptOptions{Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return want }, Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
		t.Fatal("unauthenticated report")
		return nil
	}})
	if err := r.Report(context.Background(), "f", 1, nil, true, ""); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
}

func TestCallbackReceiptRollbackIsAdmittedAndReportedFailureInvalidatesCoverage(t *testing.T) {
	var admitted, unavailable atomic.Int32
	r, _ := NewCallbackReceipts(CallbackReceiptOptions{
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			t.Fatal("rolledbacksend counted")
			return nil
		},
		AcquireService: func(context.Context, core.FederationName) (func(), error) { admitted.Add(1); return func() {}, nil },
		OnUnavailable:  func(core.FederationName, core.FederateHandle, error) { unavailable.Add(1) },
	})
	ctx := context.Background()
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{}}}
	ticket, commit, _ := r.Prepare(ctx, "f", 1, event)
	if err := r.Report(ctx, "f", 1, ticket, true, ""); err != nil {
		t.Fatal(err)
	}
	commit(false)
	if admitted.Load() != 2 || unavailable.Load() != 1 {
		t.Fatalf("rollback admissions=%d unavailable=%d", admitted.Load(), unavailable.Load())
	}
}

func TestCallbackReceiptPendingPortSerializedWithRegistration(t *testing.T) {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	r, _ := NewCallbackReceipts(CallbackReceiptOptions{Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil }, Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
		return nil
	}, Pending: func(_ core.FederationName, _ core.FederateHandle, delta int) {
		if delta == 1 {
			close(entered)
			<-release
		}
	}})
	go func() {
		_, _, _ = r.Prepare(context.Background(), "f", 1, &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{}}})
		close(done)
	}()
	<-entered
	unprotected := r.mu.TryLock()
	if unprotected {
		r.mu.Unlock()
	}
	close(release)
	<-done
	if unprotected {
		t.Fatal("lifecycle clear can overtake pending registration")
	}
}
