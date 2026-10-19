package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	hlatime "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func fmEarlyResignManager(t *testing.T, objects *object.Registry, owners *ownership.Manager, tm *hlatime.Manager, out *roResignOutbox) *federation.Manager {
	t.Helper()
	m, err := federation.New(federation.Options{
		Clock: core.NewRealClock(), FOMs: roResignFOMs{},
		PrepareResign: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, action core.ResignAction) (federation.ResignPreparation, error) {
			return PrepareResign(ctx, fed, h, action, objects, owners, out, tm)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CreateFederation(context.Background(), core.CreateFederationRequest{Name: "fed"}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		h, err := m.JoinFederation(context.Background(), core.JoinFederationRequest{Federation: "fed", FederateName: fmt.Sprint(i)})
		if err != nil || h != core.FederateHandle(i) {
			t.Fatalf("fixture join %d: %d %v", i, h, err)
		}
	}
	return m
}

func TestFMEarly20261004SixResignActionsChangeRealObjectAndOwnershipState(t *testing.T) {
	for _, action := range []core.ResignAction{
		core.ResignActionUnconditionallyDivestAttributes, core.ResignActionDeleteObjects,
		core.ResignActionCancelPendingOwnership, core.ResignActionDeleteThenDivest,
		core.ResignActionCancelThenDelete, core.ResignActionNoAction,
	} {
		t.Run(fmt.Sprint(action), func(t *testing.T) {
			ctx := context.Background()
			objects, owners, tm, out, owned := roResignFixture(t)
			manager := fmEarlyResignManager(t, objects, owners, tm, out)
			peer, _, err := objects.Register(ctx, "fed", 3, 1, "")
			if err != nil {
				t.Fatal(err)
			}
			divests := action == core.ResignActionUnconditionallyDivestAttributes || action == core.ResignActionDeleteThenDivest || action == core.ResignActionCancelThenDelete
			deletes := action == core.ResignActionDeleteObjects || action == core.ResignActionDeleteThenDivest || action == core.ResignActionCancelThenDelete
			cancels := action == core.ResignActionCancelPendingOwnership || action == core.ResignActionCancelThenDelete
			if action == core.ResignActionCancelPendingOwnership || action == core.ResignActionNoAction {
				if err := owners.UnconditionalDivest(ctx, "fed", 1, owned, []core.AttributeHandle{1, 2}); err != nil {
					t.Fatal(err)
				}
			}
			var foreign core.ObjectHandle
			if divests {
				foreign, _, err = objects.Register(ctx, "fed", 3, 1, "")
				if err != nil {
					t.Fatal(err)
				}
				owners.RegisterInitialOwnership("fed", 1, foreign, []core.AttributeHandle{2})
				if err := owners.Acquire(ctx, "fed", 2, foreign, []core.AttributeHandle{2}, []byte("waiting-peer")); err != nil {
					t.Fatal(err)
				}
			}
			if cancels {
				if err := owners.Acquire(ctx, "fed", 1, peer, []core.AttributeHandle{2}, []byte("departing-request")); err != nil {
					t.Fatal(err)
				}
				if !owners.HasAcquisitionObligation("fed", 1, peer, []core.AttributeHandle{2}) {
					t.Fatal("fixture has no pending acquisition")
				}
			}
			beforeOwners := owners.ExportState("fed")
			out.events = nil
			if err := manager.ResignFederation(ctx, "fed", 1, action); err != nil {
				t.Fatal(err)
			}
			if err := manager.ValidateMember("fed", 1); !errors.Is(err, core.ErrFederateNotJoined) {
				t.Fatalf("resigned member remains: %v", err)
			}
			for _, owner := range owners.ExportState("fed").Owners {
				if owner.Owner == 1 {
					t.Fatal("resigned member retained an attribute", owner)
				}
			}
			if owners.HasAcquisitionObligation("fed", 1, peer, []core.AttributeHandle{2}) {
				t.Fatal("resigned member retained pending acquisition")
			}
			if got := objects.ClassOf("fed", owned); (got == 0) != deletes {
				t.Fatalf("delete action=%v class=%d", deletes, got)
			}
			if objects.ClassOf("fed", peer) != 1 || !owners.IsOwnedBy("fed", 3, peer, 1) || !owners.IsOwnedBy("fed", 3, peer, 2) {
				t.Fatal("unrelated peer object or ownership changed")
			}
			if divests && (objects.ClassOf("fed", foreign) != 1 || !owners.IsOwnedBy("fed", 2, foreign, 2) || !owners.IsOwnedBy("fed", 3, foreign, 1)) {
				t.Fatal("surviving object was not divested to the waiting acquirer")
			}
			if action == core.ResignActionNoAction && (!reflect.DeepEqual(beforeOwners.Owners, owners.ExportState("fed").Owners) || len(out.events) != 0) {
				t.Fatal("NoAction changed other ownership or emitted convenience callbacks")
			}
			if deletes && divests {
				remove, acquire := -1, -1
				for i, delivery := range out.events {
					if e, ok := delivery.Event.(*object.CallbackEvent); ok && e.Kind == object.CallbackRemove && e.Object == owned {
						remove = i
					}
					if e, ok := delivery.Event.(*ownership.CallbackEvent); ok && e.Kind == ownership.CallbackAcquisitionNotification && e.Object == foreign {
						acquire = i
					}
				}
				if remove < 0 || acquire <= remove {
					t.Fatalf("delete/divest callback order: remove=%d acquire=%d", remove, acquire)
				}
			}
		})
	}
}

func TestFMEarly20261004ResignOwnershipGuardsPreserveAllState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		action  core.ResignAction
		foreign bool
	}{
		{"delete-with-foreign-attribute", core.ResignActionDeleteObjects, true},
		{"cancel-with-owned-attributes", core.ResignActionCancelPendingOwnership, false},
		{"no-action-with-owned-attributes", core.ResignActionNoAction, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			objects, owners, tm, out, _ := roResignFixture(t)
			manager := fmEarlyResignManager(t, objects, owners, tm, out)
			if tc.foreign {
				peer, _, err := objects.Register(ctx, "fed", 3, 1, "")
				if err != nil {
					t.Fatal(err)
				}
				owners.RegisterInitialOwnership("fed", 1, peer, []core.AttributeHandle{2})
			}
			beforeObjects, beforeOwners, beforeMembers := objects.ExportState("fed"), owners.ExportState("fed"), manager.Snapshot()
			out.events = nil
			if err := manager.ResignFederation(ctx, "fed", 1, tc.action); !errors.Is(err, core.ErrFederateOwnsAttributes) {
				t.Fatalf("owned-attribute rejection: %v", err)
			}
			if !reflect.DeepEqual(beforeObjects, objects.ExportState("fed")) || !reflect.DeepEqual(beforeOwners, owners.ExportState("fed")) || !reflect.DeepEqual(beforeMembers, manager.Snapshot()) || len(out.events) != 0 {
				t.Fatal("rejected resign changed object, ownership, membership or callbacks")
			}
		})
	}
}

func TestFMEarly20261004PendingAcquisitionGuardsAndInvalidDirectiveAreAtomic(t *testing.T) {
	for _, action := range []core.ResignAction{core.ResignActionUnconditionallyDivestAttributes, core.ResignActionDeleteObjects, core.ResignActionDeleteThenDivest, core.ResignActionNoAction, core.ResignActionUnspecified} {
		t.Run(fmt.Sprint(action), func(t *testing.T) {
			ctx := context.Background()
			objects, owners, tm, out, owned := roResignFixture(t)
			manager := fmEarlyResignManager(t, objects, owners, tm, out)
			if err := owners.UnconditionalDivest(ctx, "fed", 1, owned, []core.AttributeHandle{1, 2}); err != nil {
				t.Fatal(err)
			}
			peer, _, err := objects.Register(ctx, "fed", 3, 1, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := owners.Acquire(ctx, "fed", 1, peer, []core.AttributeHandle{2}, []byte("pending")); err != nil {
				t.Fatal(err)
			}
			beforeObjects, beforeOwners, beforeMembers := objects.ExportState("fed"), owners.ExportState("fed"), manager.Snapshot()
			out.events = nil
			want := core.ErrOwnershipAcquisitionPending
			if action == core.ResignActionUnspecified {
				want = core.ErrResignActionUnsupported
			}
			if err := manager.ResignFederation(ctx, "fed", 1, action); !errors.Is(err, want) {
				t.Fatalf("rejection=%v want=%v", err, want)
			}
			if !reflect.DeepEqual(beforeObjects, objects.ExportState("fed")) || !reflect.DeepEqual(beforeOwners, owners.ExportState("fed")) || !reflect.DeepEqual(beforeMembers, manager.Snapshot()) || len(out.events) != 0 {
				t.Fatal("rejected resign changed object, ownership, membership or callbacks")
			}
			if err := manager.ResignFederation(ctx, "fed", 1, core.ResignActionCancelPendingOwnership); err != nil {
				t.Fatalf("cancelling retry: %v", err)
			}
			if owners.HasAcquisitionObligation("fed", 1, peer, []core.AttributeHandle{2}) {
				t.Fatal("cancelling retry retained obligation")
			}
		})
	}
}
