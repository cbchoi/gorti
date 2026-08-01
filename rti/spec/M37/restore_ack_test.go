package m37spec

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/savepoint"
)

const restoreFed = core.FederationName("m37_restore_ack")

// newRestoreStack builds a savepoint.Manager with two joined federates
// (1="alpha", 2="beta") and a completed save under `label`, so restore
// requests have a bundle to hit.
func newRestoreStack(t *testing.T, label string) (*savepoint.Manager, *fakeOutbox) {
	t.Helper()
	outbox := newFakeOutbox()
	members := []core.FederationMember{
		{Handle: 1, Name: "alpha"},
		{Handle: 2, Name: "beta"},
	}
	mgr, err := savepoint.New(savepoint.Options{
		Outbox:      outbox,
		BundleStore: newMemStore(),
		Members: func(core.FederationName) []core.FederateHandle {
			return []core.FederateHandle{1, 2}
		},
		Roster: func(core.FederationName) []core.FederationMember {
			return members
		},
	})
	if err != nil {
		t.Fatalf("savepoint.New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.RequestFederationSave(ctx, restoreFed, label, nil); err != nil {
		t.Fatalf("RequestFederationSave: %v", err)
	}
	for _, h := range []core.FederateHandle{1, 2} {
		if err := mgr.FederateSaveComplete(ctx, restoreFed, h); err != nil {
			t.Fatalf("FederateSaveComplete(%d): %v", h, err)
		}
	}
	outbox.Reset() // drop the save-phase events; tests assert restore only
	return mgr, outbox
}

// TestSpec_M37_RequestFederationRestore_AcksRequesterAndBroadcastsBegun:
// IEEE 1516.1-2010 §4.25 requestFederationRestoreSucceeded fires on the
// REQUESTING federate only, then §4.26 federationRestoreBegun fires on
// every joined federate BEFORE the per-federate initiateFederateRestore
// (§4.26) events; initiateFederateRestore carries the federate NAME.
func TestSpec_M37_RequestFederationRestore_AcksRequesterAndBroadcastsBegun(t *testing.T) {
	mgr, outbox := newRestoreStack(t, "cp1")

	if err := mgr.RequestFederationRestoreBy(context.Background(), restoreFed, 1, "cp1"); err != nil {
		t.Fatalf("RequestFederationRestoreBy: %v", err)
	}

	// Requester (federate 1): success ack → begun → initiate.
	evts1 := outbox.SentTo(1)
	if len(evts1) != 3 {
		t.Fatalf("requester events = %d, want 3 (succeeded, begun, initiate); got %+v", len(evts1), evts1)
	}
	if ok := evts1[0].GetRestoreRequestSucceeded(); ok == nil || ok.GetLabel() != "cp1" {
		t.Errorf("event[0] = %+v, want RequestFederationRestoreSucceeded{cp1}", evts1[0])
	}
	if evts1[1].GetRestoreBegun() == nil {
		t.Errorf("event[1] = %+v, want FederationRestoreBegun", evts1[1])
	}
	init1 := evts1[2].GetRestoreInitiate()
	if init1 == nil {
		t.Fatalf("event[2] = %+v, want InitiateFederateRestore", evts1[2])
	}
	if init1.GetFederateName() != "alpha" {
		t.Errorf("initiate.federate_name = %q, want %q", init1.GetFederateName(), "alpha")
	}
	if init1.GetFederateHandle() != 1 {
		t.Errorf("initiate.federate_handle = %d, want 1", init1.GetFederateHandle())
	}

	// Non-requester (federate 2): begun → initiate (NO §4.25 ack).
	evts2 := outbox.SentTo(2)
	if len(evts2) != 2 {
		t.Fatalf("non-requester events = %d, want 2 (begun, initiate); got %+v", len(evts2), evts2)
	}
	if evts2[0].GetRestoreBegun() == nil {
		t.Errorf("event[0] = %+v, want FederationRestoreBegun", evts2[0])
	}
	init2 := evts2[1].GetRestoreInitiate()
	if init2 == nil {
		t.Fatalf("event[1] = %+v, want InitiateFederateRestore", evts2[1])
	}
	if init2.GetFederateName() != "beta" {
		t.Errorf("initiate.federate_name = %q, want %q", init2.GetFederateName(), "beta")
	}
}

// TestSpec_M37_RequestFederationRestore_UnknownLabel_FailsRequesterOnly:
// §4.25 requestFederationRestoreFailed fires on the requester when the
// label has no bundle; the RPC error is preserved (old-client compat)
// and nobody else hears anything.
func TestSpec_M37_RequestFederationRestore_UnknownLabel_FailsRequesterOnly(t *testing.T) {
	mgr, outbox := newRestoreStack(t, "cp1")

	err := mgr.RequestFederationRestoreBy(context.Background(), restoreFed, 2, "no_such_label")
	if err == nil {
		t.Fatalf("RequestFederationRestoreBy(no_such_label) succeeded; want ErrSaveBundleNotFound")
	}

	evts2 := outbox.SentTo(2)
	if len(evts2) != 1 {
		t.Fatalf("requester events = %d, want 1 (failed); got %+v", len(evts2), evts2)
	}
	failed := evts2[0].GetRestoreRequestFailed()
	if failed == nil {
		t.Fatalf("event[0] = %+v, want RequestFederationRestoreFailed", evts2[0])
	}
	if failed.GetLabel() != "no_such_label" {
		t.Errorf("failed.label = %q, want %q", failed.GetLabel(), "no_such_label")
	}
	if failed.GetReason() == "" {
		t.Errorf("failed.reason is empty; want the causing error text")
	}
	if n := len(outbox.SentTo(1)); n != 0 {
		t.Errorf("non-requester received %d events, want 0", n)
	}
}

// TestSpec_M37_RequestFederationRestore_LegacyPath_EmitsNoAck: the frozen
// core.SavepointCoordinator method (no requester handle) must keep its
// pre-M37 behavior — begun + initiate broadcast, but NO §4.25 ack.
func TestSpec_M37_RequestFederationRestore_LegacyPath_EmitsNoAck(t *testing.T) {
	mgr, outbox := newRestoreStack(t, "cp1")

	if err := mgr.RequestFederationRestore(context.Background(), restoreFed, "cp1"); err != nil {
		t.Fatalf("RequestFederationRestore: %v", err)
	}
	for _, h := range []core.FederateHandle{1, 2} {
		evts := outbox.SentTo(h)
		if len(evts) != 2 {
			t.Fatalf("federate %d events = %d, want 2 (begun, initiate); got %+v", h, len(evts), evts)
		}
		if evts[0].GetRestoreBegun() == nil || evts[1].GetRestoreInitiate() == nil {
			t.Errorf("federate %d events = %+v, want [begun, initiate]", h, evts)
		}
	}
}
