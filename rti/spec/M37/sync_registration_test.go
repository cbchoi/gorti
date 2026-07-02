package m37spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/sync"
)

const syncFed = core.FederationName("m37_sync_registration")

func newSyncStack(t *testing.T) (*sync.Manager, *fakeOutbox) {
	t.Helper()
	outbox := newFakeOutbox()
	mgr, err := sync.New(sync.Options{
		Outbox: outbox,
		Members: func(core.FederationName) []core.FederateHandle {
			return []core.FederateHandle{1, 2}
		},
	})
	if err != nil {
		t.Fatalf("sync.New: %v", err)
	}
	return mgr, outbox
}

// TestSpec_M37_SyncRegistration_Succeeded: IEEE 1516.1-2010 §4.12 —
// synchronizationPointRegistrationSucceeded fires on the REGISTERING
// federate only, BEFORE the §4.13 announce fanout.
func TestSpec_M37_SyncRegistration_Succeeded(t *testing.T) {
	mgr, outbox := newSyncStack(t)

	if err := mgr.RegisterBy(context.Background(), syncFed, 1, "ready", nil, nil); err != nil {
		t.Fatalf("RegisterBy: %v", err)
	}

	evts1 := outbox.SentTo(1)
	if len(evts1) != 2 {
		t.Fatalf("registrant events = %d, want 2 (registration ack, announce); got %+v", len(evts1), evts1)
	}
	ack := evts1[0].GetSyncRegistrationSucceeded()
	if ack == nil || ack.GetLabel() != "ready" {
		t.Errorf("event[0] = %+v, want SyncRegistrationSucceeded{ready}", evts1[0])
	}
	if evts1[1].GetSyncAnnounced() == nil {
		t.Errorf("event[1] = %+v, want SynchronizationPointAnnounced", evts1[1])
	}

	evts2 := outbox.SentTo(2)
	if len(evts2) != 1 || evts2[0].GetSyncAnnounced() == nil {
		t.Errorf("non-registrant events = %+v, want [announce] only", evts2)
	}
}

// TestSpec_M37_SyncRegistration_Failed_LabelNotUnique: registering a
// duplicate label acks the registrant with
// SYNCHRONIZATION_POINT_LABEL_NOT_UNIQUE; the RPC error is preserved
// for old clients.
func TestSpec_M37_SyncRegistration_Failed_LabelNotUnique(t *testing.T) {
	mgr, outbox := newSyncStack(t)
	ctx := context.Background()

	if err := mgr.RegisterBy(ctx, syncFed, 1, "ready", nil, nil); err != nil {
		t.Fatalf("first RegisterBy: %v", err)
	}
	outbox.Reset()

	err := mgr.RegisterBy(ctx, syncFed, 2, "ready", nil, nil)
	if !errors.Is(err, core.ErrSyncPointAlreadyRegistered) {
		t.Fatalf("duplicate RegisterBy err = %v, want ErrSyncPointAlreadyRegistered", err)
	}

	evts2 := outbox.SentTo(2)
	if len(evts2) != 1 {
		t.Fatalf("registrant events = %d, want 1 (failed); got %+v", len(evts2), evts2)
	}
	failed := evts2[0].GetSyncRegistrationFailed()
	if failed == nil {
		t.Fatalf("event[0] = %+v, want SyncRegistrationFailed", evts2[0])
	}
	if failed.GetLabel() != "ready" {
		t.Errorf("failed.label = %q, want %q", failed.GetLabel(), "ready")
	}
	if failed.GetReason() != rtiv1.SyncPointFailureReason_SYNC_POINT_FAILURE_REASON_LABEL_NOT_UNIQUE {
		t.Errorf("failed.reason = %v, want LABEL_NOT_UNIQUE", failed.GetReason())
	}
	if n := len(outbox.SentTo(1)); n != 0 {
		t.Errorf("other federate received %d events, want 0", n)
	}
}

// TestSpec_M37_SyncRegistration_Failed_SetMemberNotJoined: an explicit
// required set containing an unjoined federate acks the registrant with
// SYNCHRONIZATION_SET_MEMBER_NOT_JOINED; per IEEE 1516.1-2010 the
// register CALL itself succeeds (rejection arrives via callback) and no
// sync point is created.
func TestSpec_M37_SyncRegistration_Failed_SetMemberNotJoined(t *testing.T) {
	mgr, outbox := newSyncStack(t)
	ctx := context.Background()

	// Federate 99 is not in the joined set {1, 2}.
	if err := mgr.RegisterBy(ctx, syncFed, 1, "ready",
		nil, []core.FederateHandle{1, 99}); err != nil {
		t.Fatalf("RegisterBy with unjoined member returned RPC error %v; want nil (callback-only rejection)", err)
	}

	evts1 := outbox.SentTo(1)
	if len(evts1) != 1 {
		t.Fatalf("registrant events = %d, want 1 (failed); got %+v", len(evts1), evts1)
	}
	failed := evts1[0].GetSyncRegistrationFailed()
	if failed == nil {
		t.Fatalf("event[0] = %+v, want SyncRegistrationFailed", evts1[0])
	}
	if failed.GetReason() != rtiv1.SyncPointFailureReason_SYNC_POINT_FAILURE_REASON_SET_MEMBER_NOT_JOINED {
		t.Errorf("failed.reason = %v, want SET_MEMBER_NOT_JOINED", failed.GetReason())
	}

	// No sync point was created — the label is free for re-register.
	if err := mgr.RegisterBy(ctx, syncFed, 1, "ready", nil, nil); err != nil {
		t.Errorf("re-RegisterBy after member-not-joined rejection: %v; want success (no point created)", err)
	}
}

// TestSpec_M37_SyncRegistration_LegacyPath_EmitsNoAck: the frozen
// core.SyncCoordinator Register (no registrant) keeps its pre-M37
// behavior — announce only, no §4.12 ack, no member-joined validation.
func TestSpec_M37_SyncRegistration_LegacyPath_EmitsNoAck(t *testing.T) {
	mgr, outbox := newSyncStack(t)

	if err := mgr.Register(context.Background(), syncFed, "ready", nil, nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	for _, h := range []core.FederateHandle{1, 2} {
		evts := outbox.SentTo(h)
		if len(evts) != 1 || evts[0].GetSyncAnnounced() == nil {
			t.Errorf("federate %d events = %+v, want [announce] only", h, evts)
		}
	}
}
