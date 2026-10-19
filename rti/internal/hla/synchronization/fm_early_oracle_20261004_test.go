package sync

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const earlyOraclePoint = "point"

func TestFMEarly20261004InvalidSetFailureDoesNotRegisterAndValidRetryPreservesTag(t *testing.T) {
	ctx := context.Background()
	out := &completionOutbox{}
	m := strictCompletionManager(t, out, 1, 2)
	if err := m.RegisterBy(ctx, "f", 1, earlyOraclePoint, []byte("rejected"), []core.FederateHandle{2, 99}); err != nil {
		t.Fatalf("invalid never-joined member must use failed confirmation: %v", err)
	}
	if m.QueryState("f", earlyOraclePoint) != StateUnknown || len(m.ExportState("f").Points) != 0 {
		t.Fatal("invalid set installed a point")
	}
	sent := out.sent()
	if len(sent) != 1 || sent[0].Recipient != 1 {
		t.Fatalf("failed confirmation recipients: %+v", sent)
	}
	failed, ok := sent[0].Event.(*RegistrationFailedEvent)
	if !ok || failed.Label() != earlyOraclePoint || failed.Reason() != RegistrationFailureSetMemberNotJoined {
		t.Fatalf("failed confirmation: %#v", sent[0].Event)
	}
	tag := []byte{0, 255, 7}
	if err := m.RegisterBy(ctx, "f", 1, earlyOraclePoint, tag, []core.FederateHandle{2}); err != nil {
		t.Fatal(err)
	}
	tag[0] = 42
	if m.QueryState("f", earlyOraclePoint) != StateAnnounced {
		t.Fatal("successful retry did not install point")
	}
	sent = out.sent()
	if len(sent) != 3 || sent[1].Recipient != 1 || sent[2].Recipient != 2 {
		t.Fatalf("retry confirmation/announcement: %+v", sent)
	}
	confirmed, ok := sent[1].Event.(*RegistrationSucceededEvent)
	if !ok || confirmed.Label() != earlyOraclePoint {
		t.Fatalf("successful confirmation: %#v", sent[1].Event)
	}
	announcement, ok := sent[2].Event.(*AnnounceEvent)
	if !ok || announcement.Label() != earlyOraclePoint || !reflect.DeepEqual(announcement.Tag(), []byte{0, 255, 7}) || !reflect.DeepEqual(announcement.Required(), []core.FederateHandle{2}) {
		t.Fatalf("retry announcement: %#v", sent[2].Event)
	}
}

func TestFMEarly20261004ConfirmationsBelongToTheirRequestersAndLabels(t *testing.T) {
	out := &completionOutbox{}
	m := strictCompletionManager(t, out, 1, 2)
	if len(out.sent()) != 0 {
		t.Fatal("unsolicited confirmation")
	}
	for _, request := range []struct {
		h     core.FederateHandle
		label string
	}{{1, "first"}, {2, "second"}} {
		if err := m.RegisterBy(context.Background(), "f", request.h, request.label, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string][]core.FederateHandle{}
	for _, delivery := range out.sent() {
		if event, ok := delivery.Event.(*RegistrationSucceededEvent); ok {
			got[event.Label()] = append(got[event.Label()], delivery.Recipient)
		}
	}
	if !reflect.DeepEqual(got, map[string][]core.FederateHandle{"first": {1}, "second": {2}}) {
		t.Fatalf("confirmation routing: %+v", got)
	}
	if len(m.ExportState("f").Points) != 2 {
		t.Fatal("one label replaced another")
	}
}

func TestFMEarly20261004ResignedRegistrantRetiresQueuedConfirmation(t *testing.T) {
	ctx := context.Background()
	out := &completionOutbox{}
	m := strictCompletionManager(t, out, 1, 2)
	if err := m.RegisterBy(ctx, "f", 1, earlyOraclePoint, nil, nil); err != nil {
		t.Fatal(err)
	}
	confirmation := out.sent()[0].Event.(*RegistrationSucceededEvent)
	if err := m.FederateResigned(ctx, "f", 1); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState("f")
	for _, callback := range before.Obligations {
		if callback.Recipient == 1 {
			t.Fatal("departed registrant retained a callback obligation")
		}
	}
	if err := m.ObserveSyncCallbackEntry(ctx, "f", 1, core.SyncCallbackRegistrationSucceeded, earlyOraclePoint, confirmation.Seq()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("stale entry revived retired synchronization state")
	}
}
