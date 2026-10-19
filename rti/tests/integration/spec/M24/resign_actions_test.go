// TASK-280 (M24 W2) — ResignAction enum acceptance.

package m24spec

import (
	"context"
	"errors"
	"fmt"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

// newFedMgr creates a minimal federation.Manager for resign-action tests.
// The OnFederateResigning hook is captured so tests can assert it
// fires with the supplied action.
func newFedMgr(t *testing.T) (*federation.Manager, *resigningCapture) {
	t.Helper()
	cap := &resigningCapture{}
	mgr, err := federation.New(federation.Options{
		Clock: core.NewManualClock(stdtime.Unix(0, 0)),
		FOMs:  &nopFOMRepo{},
		OnFederateResigning: func(_ context.Context, _ core.FederationName, h core.FederateHandle, action core.ResignAction) {
			cap.action = action
			cap.handle = h
		},
	})
	if err != nil {
		t.Fatalf("federation.New: %v", err)
	}
	return mgr, cap
}

type resigningCapture struct {
	action core.ResignAction
	handle core.FederateHandle
}

// nopFOMRepo satisfies core.FOMRepository for fed manager construction.
type nopFOMRepo struct{}

func (*nopFOMRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	return &nopFOMHandle{}, nil
}
func (*nopFOMRepo) Get(_ context.Context, _ core.FederationName) (core.FOMHandle, error) {
	return &nopFOMHandle{}, nil
}

type nopFOMHandle struct{}

func (*nopFOMHandle) IsValid() bool                                           { return true }
func (*nopFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) { return 1, true }
func (*nopFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (*nopFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (*nopFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

// TestSpec_M24_ResignUnspecifiedRejected — UNSPECIFIED is the only
// invalid action.
func TestSpec_M24_ResignUnspecifiedRejected(t *testing.T) {
	mgr, _ := newFedMgr(t)
	ctx := context.Background()
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "fed"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	h, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "fed", FederateName: "f1"})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	for _, action := range []core.ResignAction{core.ResignActionUnspecified, core.ResignAction(255)} {
		err = mgr.ResignFederation(ctx, "fed", h, action)
		if !errors.Is(err, core.ErrResignActionUnsupported) {
			t.Errorf("ResignFederation(%v) = %v, want ErrResignActionUnsupported", action, err)
		}
		if err := mgr.ValidateMember("fed", h); err != nil {
			t.Fatalf("invalid resign changed membership: %v", err)
		}
	}
}

// TestSpec_M24_AllResignActionsAccepted — every non-UNSPECIFIED
// action is accepted by the manager and the OnFederateResigning hook
// fires with the same value.
func TestSpec_M24_AllResignActionsAccepted(t *testing.T) {
	cases := []struct {
		name   string
		action core.ResignAction
	}{
		{"UnconditionallyDivestAttributes", core.ResignActionUnconditionallyDivestAttributes},
		{"DeleteThenDivest", core.ResignActionDeleteThenDivest},
		{"CancelThenDelete", core.ResignActionCancelThenDelete},
		{"CancelPendingOwnership", core.ResignActionCancelPendingOwnership},
		{"NoAction", core.ResignActionNoAction},
		{"DeleteObjects", core.ResignActionDeleteObjects},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, cap := newFedMgr(t)
			ctx := context.Background()
			if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "fed"}); err != nil {
				t.Fatalf("Create: %v", err)
			}
			h, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "fed", FederateName: "f1"})
			if err != nil {
				t.Fatalf("Join: %v", err)
			}
			if _, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "fed", FederateName: "remaining"}); err != nil {
				t.Fatalf("Join remaining member: %v", err)
			}
			if err := mgr.ResignFederation(ctx, "fed", h, tc.action); err != nil {
				t.Errorf("Resign(%s) = %v; want nil", tc.name, err)
			}
			if cap.action != tc.action {
				t.Errorf("OnFederateResigning got action %v, want %v", cap.action, tc.action)
			}
			if cap.handle != h {
				t.Errorf("OnFederateResigning got handle %d, want %d", cap.handle, h)
			}
		})
	}
}

func TestSpec_M24_LastMemberEffectiveDeleteAction(t *testing.T) {
	for _, action := range []core.ResignAction{
		core.ResignActionUnconditionallyDivestAttributes, core.ResignActionDeleteThenDivest,
		core.ResignActionCancelThenDelete, core.ResignActionCancelPendingOwnership,
		core.ResignActionNoAction, core.ResignActionDeleteObjects,
	} {
		t.Run(fmt.Sprint(action), func(t *testing.T) {
			mgr, capture := newFedMgr(t)
			ctx := context.Background()
			if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "fed"}); err != nil {
				t.Fatal(err)
			}
			h, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "fed", FederateName: "last"})
			if err != nil {
				t.Fatal(err)
			}
			if err := mgr.ResignFederation(ctx, "fed", h, action); err != nil {
				t.Fatal(err)
			}
			if capture.action != core.ResignActionDeleteObjects || capture.handle != h {
				t.Fatalf("last member effective action/handle = %v/%v", capture.action, capture.handle)
			}
			if _, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "fed", FederateName: "last"}); err != nil {
				t.Fatalf("federation/name not reusable: %v", err)
			}
		})
	}
}

func TestUnnamedJoinsAllocateDistinctNamesForSameType(t *testing.T) {
	mgr, _ := newFedMgr(t)
	ctx := context.Background()
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "fed"}); err != nil {
		t.Fatal(err)
	}
	// Exercise a collision with a pre-existing explicit name as well.
	if _, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "fed", FederateName: "HLAfederate-2"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "fed", FederateType: "shared-type"}); err != nil {
			t.Fatal(err)
		}
	}
	members := mgr.ListMembers("fed")
	if len(members) != 3 {
		t.Fatalf("members = %d, want 3", len(members))
	}
	names := map[string]bool{}
	for i, member := range members {
		if member.Name == "" || names[member.Name] {
			t.Fatalf("empty or duplicate generated name: %q", member.Name)
		}
		names[member.Name] = true
		if i > 0 && member.FederateType != "shared-type" {
			t.Fatalf("type = %q, want shared-type", member.FederateType)
		}
	}
}
