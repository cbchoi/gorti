package ownership

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func importedCallbackScopes(t *testing.T) (*Manager, []core.OwnershipCallbackScope, []*CallbackEvent) {
	t.Helper()
	m, out := newMgr(t, Options{})
	var scopes []core.OwnershipCallbackScope
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1})
	for _, h := range []core.FederateHandle{2, 3} {
		scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: h, FederationGeneration: 7, CallbackEpoch: 1}
		if err := m.EnableOwnershipCallbackReceipts(context.Background(), scope); err != nil {
			t.Fatal(err)
		}
		if err := m.AcquireIfAvailable(context.Background(), "fed", h, 100, []core.AttributeHandle{1}, nil); err != nil {
			t.Fatal(err)
		}
		scopes = append(scopes, scope)
	}
	var old []*CallbackEvent
	for _, record := range out.Sent() {
		old = append(old, record.Event.(*CallbackEvent))
	}
	state := m.ExportState("fed")
	commit, err := m.PrepareImportState("fed", state)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	return m, scopes, old
}

func TestOwnershipCallbackRestoreScopesValidateAllBeforeMutation(t *testing.T) {
	for _, mode := range []string{"missing", "duplicate", "same-scope", "wrong-federation", "zero-generation", "zero-recipient"} {
		t.Run(mode, func(t *testing.T) {
			m, scopes, _ := importedCallbackScopes(t)
			before := m.ExportState("fed")
			for i := range scopes {
				scopes[i].CallbackEpoch++
			}
			switch mode {
			case "missing":
				scopes = scopes[:1]
			case "duplicate":
				scopes[1] = scopes[0]
			case "same-scope":
				scopes[1].CallbackEpoch--
			case "wrong-federation":
				scopes[1].Federation = "other"
			case "zero-generation":
				scopes[1].FederationGeneration = 0
			case "zero-recipient":
				scopes[1].Recipient = 0
			}
			reservation, err := m.PrepareOwnershipCallbackRestoreScopes("fed", scopes)
			if !errors.Is(err, core.ErrOwnershipCallbackScope) || reservation != nil {
				t.Fatal("invalid batch accepted", err)
			}
			if !reflect.DeepEqual(before, m.ExportState("fed")) {
				t.Fatal("valid earlier session partially rebound")
			}
			if !m.mu.TryLock() {
				t.Fatal("failed preparation leaked ownership lease")
			}
			m.mu.Unlock()
		})
	}
}

func TestOwnershipCallbackRestoreScopesCommitAtomicAndInfallible(t *testing.T) {
	m, scopes, old := importedCallbackScopes(t)
	before := m.ExportState("fed")
	for i := range scopes {
		scopes[i].CallbackEpoch++
	}
	// Parent supplies every roster scope, including legacy recipients.
	scopes = append(scopes, core.OwnershipCallbackScope{Federation: "fed", Recipient: 4, FederationGeneration: 7, CallbackEpoch: 0})
	reservation, err := m.PrepareOwnershipCallbackRestoreScopes("fed", scopes)
	if err != nil {
		t.Fatal(err)
	}
	if m.mu.TryLock() {
		m.mu.Unlock()
		reservation.Release()
		t.Fatal("restore batch did not pin imported ownership state")
	}
	if err := reservation.Commit(); err != nil {
		t.Fatal("validated restore commit must be infallible", err)
	}
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	reservation.Release()
	after := m.ExportState("fed")
	if !reflect.DeepEqual(before.PendingCallbacks, after.PendingCallbacks) || len(after.CallbackSessions) != 2 {
		t.Fatal("rebind changed stable payload/IDs or enabled legacy roster recipient", after)
	}
	for i, session := range after.CallbackSessions {
		if session.CallbackEpoch != 2 || session.LastCallbackID != before.CallbackSessions[i].LastCallbackID || session.CompletedThrough != before.CallbackSessions[i].CompletedThrough {
			t.Fatal("restore lost ID counters", session)
		}
		receipt, finish, err := m.BeginOwnershipCallbackDelivery(context.Background(), scopes[i], 1)
		if err != nil || receipt.DeliveryAttempt != 1 {
			t.Fatal("new scope not ready/reset", receipt, err)
		}
		finish(true)
	}
	for _, event := range old {
		scope, id, strict := event.OwnershipCallbackIdentity()
		if scope.CallbackEpoch != 1 || id != 1 || !strict {
			t.Fatal("old queued payload identity mutated", scope, id, strict)
		}
	}
}

func TestOwnershipCallbackRestoreScopesAbortAndImportedRequirement(t *testing.T) {
	m, scopes, _ := importedCallbackScopes(t)
	before := m.ExportState("fed")
	for i := range scopes {
		scopes[i].CallbackEpoch++
	}
	reservation, err := m.PrepareOwnershipCallbackRestoreScopes("fed", scopes)
	if err != nil {
		t.Fatal(err)
	}
	reservation.Release()
	reservation.Release()
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("aborted scope plan changed imported state")
	}
	if _, _, err := m.BeginOwnershipCallbackDelivery(context.Background(), scopes[0], 1); !errors.Is(err, core.ErrOwnershipCallbackScope) {
		t.Fatal("aborted plan activated callbacks", err)
	}
	reservation, err = m.PrepareOwnershipCallbackRestoreScopes("fed", scopes)
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	for i := range scopes {
		scopes[i].CallbackEpoch++
	}
	if reservation, err := m.PrepareOwnershipCallbackRestoreScopes("fed", scopes); !errors.Is(err, core.ErrOwnershipCallbackScope) || reservation != nil {
		t.Fatal("live non-imported state accepted as restore batch", err)
	}
}

func TestOwnershipCallbackScopeRestoreMapKeyAndFreshZeroEpoch(t *testing.T) {
	m, scopes, _ := importedCallbackScopes(t)
	planned := map[core.FederateHandle]core.OwnershipCallbackScope{}
	for _, scope := range scopes {
		scope.FederationGeneration++
		scope.CallbackEpoch = 0
		planned[scope.Recipient] = scope
	}
	bad := planned[3]
	bad.Recipient = 4
	planned[3] = bad
	if reservation, err := m.PrepareOwnershipCallbackScopeRestore("fed", planned); !errors.Is(err, core.ErrOwnershipCallbackScope) || reservation != nil {
		t.Fatal("scope-map key mismatch accepted", err)
	}
	bad.Recipient = 3
	planned[3] = bad
	reservation, err := m.PrepareOwnershipCallbackScopeRestore("fed", planned)
	if err != nil {
		t.Fatal("fresh generation with initial callback epoch0 rejected", err)
	}
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, session := range m.ExportState("fed").CallbackSessions {
		if session.FederationGeneration != 8 || session.CallbackEpoch != 0 {
			t.Fatal("map rebind lost fresh generation/valid zero epoch", session)
		}
	}
}
