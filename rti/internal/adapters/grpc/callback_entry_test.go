package grpc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestTransportationConfirmationReceiptUsesStandardCallbackNames(t *testing.T) {
	for _, test := range []struct {
		name    string
		change  *rtiv1.TransportationChange
		service string
	}{
		{"attribute", &rtiv1.TransportationChange{ObjectHandle: 7, AttributeHandles: []uint64{8}}, "confirmAttributeTransportationTypeChange"},
		{"interaction", &rtiv1.TransportationChange{InteractionClassHandle: 9}, "confirmInteractionTransportationTypeChange"},
		{"ambiguous", &rtiv1.TransportationChange{ObjectHandle: 7, InteractionClassHandle: 9}, ""},
		{"empty", &rtiv1.TransportationChange{}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_TransportationChangeConfirmed{TransportationChangeConfirmed: test.change}}
			if got := callbackServiceName(event); got != test.service {
				t.Fatalf("callback service=%q want=%q", got, test.service)
			}
		})
	}
}

func TestRequiredCallbackReceiptCapacityRejectsWithoutCoverageFallback(t *testing.T) {
	r, err := NewCallbackReceipts(CallbackReceiptOptions{
		MaxPending:   1,
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	event := &rtiv1.FederateEvent{Seq: 7, Event: &rtiv1.FederateEvent_SyncAnnounced{SyncAnnounced: &rtiv1.SynchronizationPointAnnounced{Label: "ready"}}}
	ticket, release, err := r.PrepareRequired(context.Background(), "f", 1, event)
	if err != nil || len(ticket) != 32 {
		t.Fatalf("receipt=%x err=%v", ticket, err)
	}
	if _, _, err := r.PrepareRequired(context.Background(), "f", 1, event); err == nil {
		t.Fatal("required receipt silently omitted at capacity")
	}
	release(false)
	if ticket, release, err := r.PrepareRequired(context.Background(), "f", 1, event); err != nil || len(ticket) != 32 {
		t.Fatalf("rolled-back capacity not reusable: receipt=%x err=%v", ticket, err)
	} else {
		release(false)
	}
}

func TestCallbackEntryBeforeSendCommitAllowsReentrantServicesWithoutCompleting(t *testing.T) {
	var entries, completed atomic.Int32
	r, err := NewCallbackReceipts(CallbackReceiptOptions{
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		ObserveEntry: func(_ context.Context, _ core.FederationName, _ core.FederateHandle, event *rtiv1.FederateEvent) error {
			if event.GetSyncAnnounced().GetLabel() != "ready" {
				t.Fatal("mutated callback entered")
			}
			entries.Add(1)
			return nil
		},
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			completed.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	event := &rtiv1.FederateEvent{Seq: 7, Event: &rtiv1.FederateEvent_SyncAnnounced{SyncAnnounced: &rtiv1.SynchronizationPointAnnounced{Label: "ready"}}}
	ticket, commit, err := r.PrepareRequired(context.Background(), "f", 1, event)
	if err != nil {
		t.Fatal(err)
	}
	event.GetSyncAnnounced().Label = "mutated"
	if err := r.ReportEntry(context.Background(), "f", 1, ticket); err != nil {
		t.Fatal(err)
	}
	if entries.Load() != 1 || completed.Load() != 0 {
		t.Fatal("entry treated as completion")
	}
	if err := r.ReportEntry(context.Background(), "f", 1, ticket); err != nil {
		t.Fatal(err)
	}
	if entries.Load() != 1 {
		t.Fatal("duplicate entry reapplied")
	}
	commit(true)
	if completed.Load() != 0 {
		t.Fatal("send commit treated as callback return")
	}
	if err := r.Report(context.Background(), "f", 1, ticket, false, "handler threw"); err != nil {
		t.Fatal(err)
	}
	if completed.Load() != 1 {
		t.Fatal("throwing callback completion missing")
	}
	if err := r.ReportEntry(context.Background(), "f", 1, ticket); err != nil {
		t.Fatal("completed entry replay", err)
	}
}

func TestCallbackEntryAuthenticationAndObserverRetry(t *testing.T) {
	denied, transient := errors.New("denied"), errors.New("retry")
	var calls atomic.Int32
	r, _ := NewCallbackReceipts(CallbackReceiptOptions{
		Authenticate: func(_ context.Context, _ core.FederationName, h core.FederateHandle) error {
			if h != 1 {
				return denied
			}
			return nil
		},
		ObserveEntry: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error {
			if calls.Add(1) == 1 {
				return transient
			}
			return nil
		},
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			return nil
		},
	})
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SyncAnnounced{SyncAnnounced: &rtiv1.SynchronizationPointAnnounced{Label: "ready"}}}
	ticket, commit, err := r.PrepareRequired(context.Background(), "f", 1, event)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ReportEntry(context.Background(), "f", 2, nil); !errors.Is(err, denied) {
		t.Fatalf("auth must precede receipt lookup: %v", err)
	}
	if err := r.ReportEntry(context.Background(), "other", 1, ticket); err == nil {
		t.Fatal("cross-federation entry accepted")
	}
	if err := r.ReportEntry(context.Background(), "f", 1, ticket); !errors.Is(err, transient) {
		t.Fatalf("error=%v", err)
	}
	if err := r.ReportEntry(context.Background(), "f", 1, ticket); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("failed observer was not retried")
	}
	commit(false)
	if err := r.ReportEntry(context.Background(), "f", 1, ticket); err == nil {
		t.Fatal("rolled-back receipt accepted")
	}
}

func TestRequiredCallbackOutcomeRetriesAfterTransientApplicationFailure(t *testing.T) {
	var calls atomic.Int32
	transient := errors.New("checkpoint observation cancelled")
	r, _ := NewCallbackReceipts(CallbackReceiptOptions{
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			if calls.Add(1) == 1 {
				return transient
			}
			return nil
		},
	})
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SyncSynchronized{SyncSynchronized: &rtiv1.FederationSynchronized{Label: "ready"}}}
	ticket, commit, err := r.PrepareRequired(context.Background(), "f", 1, event)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ReportEntry(context.Background(), "f", 1, ticket); err != nil {
		t.Fatal(err)
	}
	if err := r.Report(context.Background(), "f", 1, ticket, true, ""); !errors.Is(err, transient) {
		t.Fatal("missing application failure", err)
	}
	if r.pending != 1 || r.entries[string(ticket)].event == nil {
		t.Fatal("failed application discharged mandatory outcome")
	}
	if err := r.Report(context.Background(), "f", 1, ticket, false, "different"); err == nil {
		t.Fatal("conflicting outcome accepted during retry")
	}
	if err := r.Report(context.Background(), "f", 1, ticket, true, ""); err != nil {
		t.Fatal(err)
	}
	if r.pending != 0 || calls.Load() != 2 {
		t.Fatal("idempotent application did not recover")
	}
	commit(true)
	if err := r.Report(context.Background(), "f", 1, ticket, true, ""); err != nil || calls.Load() != 2 {
		t.Fatal("duplicate outcome reapplied", err)
	}
}

func TestRequiredCallbackCompletionDoesNotDependOnOptionalMetrics(t *testing.T) {
	var lifecycle, metrics atomic.Int32
	r, _ := NewCallbackReceipts(CallbackReceiptOptions{
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		ObserveRequired: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			lifecycle.Add(1)
			return nil
		},
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			metrics.Add(1)
			return errors.New("optional MOM observation unavailable")
		},
	})
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SyncSynchronized{SyncSynchronized: &rtiv1.FederationSynchronized{Label: "ready"}}}
	ticket, commit, err := r.PrepareRequired(context.Background(), "f", 1, event)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ReportEntry(context.Background(), "f", 1, ticket); err != nil {
		t.Fatal(err)
	}
	if err := r.Report(context.Background(), "f", 1, ticket, true, ""); err != nil {
		t.Fatal("optional metrics blocked lifecycle", err)
	}
	commit(true)
	if err := r.Report(context.Background(), "f", 1, ticket, true, ""); err != nil {
		t.Fatal(err)
	}
	if lifecycle.Load() != 1 || metrics.Load() != 1 || r.pending != 0 {
		t.Fatal("completed lifecycle was recounted")
	}
}
