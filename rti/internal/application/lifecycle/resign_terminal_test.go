package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

func TestResignTerminalOutcomePrecedesRemovalAndFinishesLegacy(t *testing.T) {
	ctx := context.Background()
	objects, owners, tm, out, obj := roResignFixture(t)
	if err := owners.Acquire(ctx, "fed", 3, obj, []core.AttributeHandle{2}, nil); err != nil {
		t.Fatal(err)
	}
	out.events = nil
	p, err := PrepareResign(ctx, "fed", 1, core.ResignActionDeleteObjects, objects, owners, out, tm)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Release()
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	if len(out.events) < 2 {
		t.Fatalf("missing terminal/removal: %+v", out.events)
	}
	terminal, ok := out.events[0].Event.(*ownership.CallbackEvent)
	if !ok || terminal.Kind != ownership.CallbackUnavailable || out.events[0].Recipient != 3 {
		t.Fatalf("first callback is not terminal: %T", out.events[0].Event)
	}
	removal, ok := out.events[1].Event.(*object.CallbackEvent)
	if !ok || removal.Kind != object.CallbackRemove {
		t.Fatalf("second callback is not removal: %T", out.events[1].Event)
	}
	if owners.HasAcquisitionObligation("fed", 3, obj, []core.AttributeHandle{2}) {
		t.Fatal("successful legacy admission did not finish obligation")
	}
}

func TestDeferredDeletionOwnershipAdmissionFailureWithholdsGrant(t *testing.T) {
	ctx := context.Background()
	objects, owners, tm, out, obj := roResignFixture(t)
	if err := owners.Acquire(ctx, "fed", 3, obj, []core.AttributeHandle{2}, nil); err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableRegulation(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	ts := core.LogicalTime(10)
	if err := objects.Delete(ctx, "fed", 1, obj, &ts, nil); err != nil {
		t.Fatal(err)
	}
	out.events = nil
	failure := errors.New("ownership terminal admission unavailable")
	out.fail = failure
	if err := tm.TimeAdvanceRequest(ctx, "fed", 1, 10); !errors.Is(err, failure) {
		t.Fatalf("grant admission error=%v", err)
	}
	if objects.ClassOf("fed", obj) == 0 || !owners.IsOwnedBy("fed", 1, obj, 2) || len(owners.ExportState("fed").PendingAcquires) != 1 {
		t.Fatal("failed atomic reservation changed object/ownership")
	}
	if len(out.events) != 0 {
		t.Fatal("failed terminal reservation published removal/grant")
	}
	out.fail = nil
	if err := tm.RetryPendingDeliveries(ctx, "fed"); err != nil {
		t.Fatal(err)
	}
	if objects.ClassOf("fed", obj) != 0 || owners.IsOwnedBy("fed", 1, obj, 2) {
		t.Fatal("retry did not commit object/ownership deletion")
	}
	if len(out.events) < 2 {
		t.Fatal("retry lost terminal/grant")
	}
	if event, ok := out.events[0].Event.(*ownership.CallbackEvent); !ok || event.Kind != ownership.CallbackUnavailable {
		t.Fatal("grant preceded mandatory termination")
	}
}

func TestFutureDeletionRetainsExistingTerminalBeforeROForget(t *testing.T) {
	ctx := context.Background()
	objects, owners, tm, out, obj := roResignFixture(t)
	scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 3, FederationGeneration: 1}
	if err := owners.EnableOwnershipCallbackReceipts(ctx, scope); err != nil {
		t.Fatal(err)
	}
	if err := owners.AcquireIfAvailable(ctx, "fed", 3, obj, []core.AttributeHandle{2}, nil); err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableRegulation(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	ts := core.LogicalTime(10)
	if err := objects.Delete(ctx, "fed", 1, obj, &ts, nil); err != nil {
		t.Fatal(err)
	}
	state := owners.ExportState("fed")
	if len(state.PendingCallbacks) != 1 || state.PendingCallbacks[0].RetainedProjection == nil {
		t.Fatal("existing terminal projection lost before RO removal")
	}
	if !owners.IsOwnedBy("fed", 1, obj, 2) || objects.ClassOf("fed", obj) == 0 {
		t.Fatal("projection retention prematurely applied timestamped deletion")
	}
	out.events = nil
	if err := owners.RetryOwnershipCallbacks(ctx, scope); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 1 || out.events[0].Event.(*ownership.CallbackEvent).RetainedProjection == nil {
		t.Fatal("mandatory retry depends on forgotten object knowledge")
	}
}

func TestDeferredDeletionResolvesOwnershipAtSenderTimestamp(t *testing.T) {
	ctx := context.Background()
	objects, owners, tm, out, obj := roResignFixture(t)
	scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 3, FederationGeneration: 1}
	if err := owners.EnableOwnershipCallbackReceipts(ctx, scope); err != nil {
		t.Fatal(err)
	}
	if err := owners.Acquire(ctx, "fed", 3, obj, []core.AttributeHandle{2}, nil); err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableRegulation(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	out.events = nil
	ts := core.LogicalTime(10)
	if err := objects.Delete(ctx, "fed", 1, obj, &ts, nil); err != nil {
		t.Fatal(err)
	}
	for _, delivery := range out.events {
		if _, ok := delivery.Event.(*ownership.CallbackEvent); ok {
			t.Fatal("future deletion prematurely terminated acquisition")
		}
	}
	if !owners.IsOwnedBy("fed", 1, obj, 2) {
		t.Fatal("future deletion prematurely removed ownership")
	}
	out.events = nil
	if err := tm.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	var terminal *ownership.CallbackEvent
	for _, delivery := range out.events {
		if event, ok := delivery.Event.(*ownership.CallbackEvent); ok && event.Kind == ownership.CallbackUnavailable {
			terminal = event
		}
	}
	if terminal == nil {
		t.Fatal("sender deletion timestamp silently erased/retained pending acquisition without terminal outcome")
	}
	if terminal.RetainedProjection == nil || terminal.RetainedProjection.KnownClass != 1 {
		t.Fatal("forgotten RO recipient lost its original class projection")
	}
	if objects.ClassOf("fed", obj) != 0 || owners.IsOwnedBy("fed", 1, obj, 2) {
		t.Fatal("sender timestamp did not retire object and ownership together")
	}
	if !owners.HasAcquisitionObligation("fed", 3, obj, []core.AttributeHandle{2}) {
		t.Fatal("deleted strict terminal lost lifetime before invocation")
	}
	saved, err := (savebundle.Ownership{Manager: owners}).Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ownership.New(ownership.Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	if err := (savebundle.Ownership{Manager: restored}).Unmarshal("fed", saved); err != nil {
		t.Fatal(err)
	}
	scope.CallbackEpoch = 1
	if err := restored.RebindOwnershipCallbackScope(scope); err != nil {
		t.Fatal(err)
	}
	out.events = nil
	if err := restored.RetryOwnershipCallbacks(ctx, scope); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 1 {
		t.Fatalf("restored deferred terminal count=%d", len(out.events))
	}
	replayed := out.events[0].Event.(*ownership.CallbackEvent)
	if replayed.RetainedProjection == nil || replayed.RetainedProjection.KnownClass != 1 || replayed.RetainedProjection.Attributes[0] != 2 {
		t.Fatal("deferred deletion save/restore lost retained class/attributes")
	}
	_, id, strict := replayed.OwnershipCallbackIdentity()
	if !strict || !restored.HasAcquisitionObligation("fed", 3, obj, nil) {
		t.Fatal("restored terminal lifetime ended before invocation")
	}
	receipt, finish, err := restored.BeginOwnershipCallbackDelivery(ctx, scope, id)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if err := restored.ReportOwnershipCallbackInvocation(ctx, scope, receipt); err != nil {
		t.Fatal(err)
	}
	if restored.HasAcquisitionObligation("fed", 3, obj, nil) {
		t.Fatal("invocation did not complete deferred terminal")
	}
}

func TestResignRetainsDeletedTerminalProjectionThroughRestoreAndRetry(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existing], func(t *testing.T) {
			ctx := context.Background()
			objects, owners, tm, out, obj := roResignFixture(t)
			scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 3, FederationGeneration: 1}
			if err := owners.EnableOwnershipCallbackReceipts(ctx, scope); err != nil {
				t.Fatal(err)
			}
			var err error
			if existing {
				err = owners.AcquireIfAvailable(ctx, "fed", 3, obj, []core.AttributeHandle{2}, nil)
			} else {
				err = owners.Acquire(ctx, "fed", 3, obj, []core.AttributeHandle{2}, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			p, err := PrepareResign(ctx, "fed", 1, core.ResignActionDeleteObjects, objects, owners, out, tm)
			if err != nil {
				t.Fatal(err)
			}
			if err := p.Commit(); err != nil {
				t.Fatal(err)
			}
			p.Release()
			state := owners.ExportState("fed")
			if len(state.PendingCallbacks) != 1 || state.PendingCallbacks[0].RetainedProjection == nil {
				t.Fatalf("deleted projection lost: %+v", state.PendingCallbacks)
			}
			if !owners.HasAcquisitionObligation("fed", 3, obj, nil) {
				t.Fatal("strict obligation completed before invocation")
			}
			restored, err := ownership.New(ownership.Options{Outbox: out})
			if err != nil {
				t.Fatal(err)
			}
			commit, err := restored.PrepareImportState("fed", state)
			if err != nil {
				t.Fatal(err)
			}
			commit()
			state.PendingCallbacks[0].RetainedProjection.Attributes[0] = 99
			scope.CallbackEpoch = 1
			if err := restored.RebindOwnershipCallbackScope(scope); err != nil {
				t.Fatal(err)
			}
			out.events = nil
			if err := restored.RetryOwnershipCallbacks(ctx, scope); err != nil {
				t.Fatal(err)
			}
			if len(out.events) != 1 {
				t.Fatalf("retry callbacks=%d", len(out.events))
			}
			event := out.events[0].Event.(*ownership.CallbackEvent)
			if event.RetainedProjection.Attributes[0] != 2 {
				t.Fatal("snapshot aliases retained projection")
			}
			_, id, strict := event.OwnershipCallbackIdentity()
			if !strict {
				t.Fatal("restore lost invocation requirement")
			}
			receipt, finish, err := restored.BeginOwnershipCallbackDelivery(ctx, scope, id)
			if err != nil {
				t.Fatal(err)
			}
			finish(true)
			if err := restored.ReportOwnershipCallbackInvocation(ctx, scope, receipt); err != nil {
				t.Fatal(err)
			}
			if restored.HasAcquisitionObligation("fed", 3, obj, nil) {
				t.Fatal("invocation did not finish deleted-object obligation")
			}
		})
	}
}
