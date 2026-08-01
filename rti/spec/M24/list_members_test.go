// TASK-287 (M24 W3) — ListFederationMembers + AbortSave/AbortRestore.

package m24spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestSpec_M24_ListMembers_ReturnsRoster verifies §4.8 surface.
func TestSpec_M24_ListMembers_ReturnsRoster(t *testing.T) {
	mgr, _ := newFedMgr(t)
	ctx := context.Background()
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "fed"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, n := range []string{"alpha", "beta", "gamma"} {
		_, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{
			Federation:   "fed",
			FederateName: n,
			FederateType: "kind-" + n,
		})
		if err != nil {
			t.Fatalf("Join %s: %v", n, err)
		}
	}

	members := mgr.ListMembers("fed")
	if len(members) != 3 {
		t.Fatalf("ListMembers len = %d, want 3", len(members))
	}
	expected := []string{"alpha", "beta", "gamma"}
	for i, want := range expected {
		if members[i].Name != want {
			t.Errorf("members[%d].Name = %q, want %q", i, members[i].Name, want)
		}
		if members[i].FederateType != "kind-"+want {
			t.Errorf("members[%d].FederateType = %q, want %q", i, members[i].FederateType, "kind-"+want)
		}
	}
}

// TestSpec_M24_ListMembers_UnknownFederationEmpty.
func TestSpec_M24_ListMembers_UnknownFederationEmpty(t *testing.T) {
	mgr, _ := newFedMgr(t)
	if got := mgr.ListMembers("nonexistent"); len(got) != 0 {
		t.Errorf("ListMembers(unknown) = %d entries, want 0", len(got))
	}
}

// TestSpec_M24_AbortSave_NotInProgressError.
func TestSpec_M24_AbortSave_NotInProgressError(t *testing.T) {
	// Build a savepoint manager with the smallest possible setup.
	mgr := newSavepointMgr(t)
	err := mgr.AbortSave(context.Background(), "fed")
	if !errors.Is(err, core.ErrSaveNotInProgress) {
		t.Errorf("AbortSave(no-save) = %v, want ErrSaveNotInProgress", err)
	}
}

// TestSpec_M24_AbortRestore_NotInProgressError.
func TestSpec_M24_AbortRestore_NotInProgressError(t *testing.T) {
	mgr := newSavepointMgr(t)
	err := mgr.AbortRestore(context.Background(), "fed")
	if !errors.Is(err, core.ErrRestoreNotInProgress) {
		t.Errorf("AbortRestore(no-restore) = %v, want ErrRestoreNotInProgress", err)
	}
}
