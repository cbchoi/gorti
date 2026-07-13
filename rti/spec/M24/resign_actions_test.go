// TASK-280 (M24 W2) — ResignAction enum acceptance.

package m24spec

import (
	"context"
	"errors"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/federation"
)

// newFedMgr creates a minimal federation.Manager for resign-action tests.
// The OnFederateResigning hook is captured so tests can assert it
// fires with the supplied action.
func newFedMgr(t *testing.T) (*federation.Manager, *resigningCapture) {
	t.Helper()
	cap := &resigningCapture{}
	mgr, err := federation.New(federation.Options{
		Clock: core.NewFakeClock(stdtime.Unix(0, 0)),
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

	err = mgr.ResignFederation(ctx, "fed", h, core.ResignActionUnspecified)
	if !errors.Is(err, core.ErrResignActionUnsupported) {
		t.Errorf("ResignFederation(Unspecified) = %v, want ErrResignActionUnsupported", err)
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
