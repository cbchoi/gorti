package object

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

func TestUnpublicationRelinquishesOwnedInstanceAttributes(t *testing.T) {
	r, decl, owners, obj := knownOwnershipPreparationFixture(t)
	decl.SetObjectUnpublication(r, owners)
	if err := decl.UnpublishObjectClassAttributes(context.Background(), "fed", 1, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if owners.IsOwnedBy("fed", 1, obj, 1) {
		t.Fatal("unpublication removed the declaration but retained instance ownership")
	}
	if !owners.IsOwnedBy("fed", 1, obj, 2) {
		t.Fatal("unpublication relinquished an unrelated instance attribute")
	}
}

func TestUnpublicationExplicitEmptyAndWholeClassDiffer(t *testing.T) {
	r, decl, owners, obj := knownOwnershipPreparationFixture(t)
	decl.SetObjectUnpublication(r, owners)
	before := owners.ExportState("fed")
	if err := decl.UnpublishObjectClassAttributeSet(context.Background(), "fed", 1, 1, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, owners.ExportState("fed")) || len(decl.PublishedObjectAttributes("fed", 1, 1)) != 2 {
		t.Fatal("supplied empty set changed publication or ownership")
	}
	if err := decl.UnpublishObjectClass(context.Background(), "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	if len(decl.PublishedObjectAttributes("fed", 1, 1)) != 0 || owners.IsOwnedBy("fed", 1, obj, 1) || owners.IsOwnedBy("fed", 1, obj, 2) {
		t.Fatal("omitted set did not remove every publication and owned instance attribute")
	}
}

func TestUnpublicationMapsRememberedParentAttributes(t *testing.T) {
	r, decl, owners, obj := knownOwnershipPreparationFixture(t)
	decl.SetObjectUnpublication(r, owners)
	owners.RegisterInitialOwnership("fed", 2, obj, []core.AttributeHandle{1})
	if err := decl.UnpublishObjectClassAttributeSet(context.Background(), "fed", 2, 9, []core.AttributeHandle{101}); err != nil {
		t.Fatal(err)
	}
	if owners.IsOwnedBy("fed", 2, obj, 1) || !owners.IsOwnedBy("fed", 1, obj, 2) {
		t.Fatal("unpublication used parent numeric handles as registered-class coordinates")
	}
	if len(decl.PublishedObjectAttributes("fed", 1, 1)) != 2 {
		t.Fatal("another federate's publication changed")
	}
}

func TestUnpublicationPendingAcquisitionRequiresTerminalInvocation(t *testing.T) {
	r, decl, owners, obj := knownOwnershipPreparationFixture(t)
	decl.SetObjectUnpublication(r, owners)
	ctx := context.Background()
	scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 17}
	transportationOK(t, owners.EnableOwnershipCallbackReceipts(ctx, scope))
	_, err := owners.ExecuteKnownOwnership(ctx, core.OwnershipCommand{Operation: core.OwnershipAcquire, Federation: "fed", Federate: 2, Object: obj, Attributes: []core.AttributeHandle{101}})
	transportationOK(t, err)
	before := owners.ExportState("fed")
	if err := decl.UnpublishObjectClass(ctx, "fed", 2, 9); !errors.Is(err, core.ErrOwnershipAcquisitionPending) {
		t.Fatal("pending acquisition allowed unpublication", err)
	}
	if !reflect.DeepEqual(before, owners.ExportState("fed")) || len(decl.PublishedObjectAttributes("fed", 2, 9)) != 1 {
		t.Fatal("rejected call had partial effects")
	}
	// Attributes not being unpublished cannot trigger section 5.3.3f.
	transportationOK(t, decl.UnpublishObjectClassAttributeSet(ctx, "fed", 2, 9, nil))
	_, err = owners.ExecuteKnownOwnership(ctx, core.OwnershipCommand{Operation: core.OwnershipCancelAcquire, Federation: "fed", Federate: 2, Object: obj, Attributes: []core.AttributeHandle{101}})
	transportationOK(t, err)
	if err := decl.UnpublishObjectClass(ctx, "fed", 2, 9); !errors.Is(err, core.ErrOwnershipAcquisitionPending) {
		t.Fatal("queue admission was mistaken for callback invocation", err)
	}
	id := owners.ExportState("fed").PendingCallbacks[0].CallbackID
	receipt, finish, err := owners.BeginOwnershipCallbackDelivery(ctx, scope, id)
	transportationOK(t, err)
	finish(true)
	transportationOK(t, owners.ReportOwnershipCallbackInvocation(ctx, scope, receipt))
	transportationOK(t, decl.UnpublishObjectClass(ctx, "fed", 2, 9))
	if !owners.IsOwnedBy("fed", 1, obj, 1) {
		t.Fatal("acquisition cancellation removed the other owner's attribute")
	}
}

func TestUnpublicationEntireObjectSelectionIsAtomic(t *testing.T) {
	r, decl, owners, first := knownOwnershipPreparationFixture(t, func(opts *ownership.Options) { opts.CallbackWindow = 1 })
	decl.SetObjectUnpublication(r, owners)
	ctx := context.Background()
	scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 17}
	transportationOK(t, owners.EnableOwnershipCallbackReceipts(ctx, scope))
	transportationOK(t, decl.UnsubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{2}))
	reserveFixtureName(t, r, "fed", 1, "unpublication-second")
	second, _, err := r.Register(ctx, "fed", 1, 1, "unpublication-second")
	transportationOK(t, err)
	for _, obj := range []core.ObjectHandle{first, second} {
		_, err := owners.ExecuteKnownOwnership(ctx, core.OwnershipCommand{Operation: core.OwnershipAcquire, Federation: "fed", Federate: 2, Object: obj, Attributes: []core.AttributeHandle{101}})
		transportationOK(t, err)
	}
	before := owners.ExportState("fed")
	err = decl.UnpublishObjectClassAttributeSet(ctx, "fed", 1, 1, []core.AttributeHandle{1})
	if !errors.Is(err, core.ErrOwnershipCallbackCapacity) {
		t.Fatal("entire fanout was not prevalidated", err)
	}
	if !reflect.DeepEqual(before, owners.ExportState("fed")) || len(decl.PublishedObjectAttributes("fed", 1, 1)) != 2 {
		t.Fatal("failed prepare changed an earlier object/publication")
	}
	st := r.stateFor("fed")
	if !st.mu.TryLock() {
		t.Fatal("failed prepare leaked object pin")
	}
	st.mu.Unlock()
}

func TestUnpublicationPinSelectionDetachedAndIdempotent(t *testing.T) {
	r, _, _, obj := knownOwnershipPreparationFixture(t)
	plan, err := r.PrepareObjectClassUnpublication(context.Background(), "fed", 2, 9, []core.AttributeHandle{101})
	transportationOK(t, err)
	st := r.stateFor("fed")
	if st.mu.TryLock() {
		st.mu.Unlock()
		plan.Release()
		t.Fatal("object selection was not pinned")
	}
	selected := plan.SelectAttributes([]core.AttributeHandle{101})
	if !reflect.DeepEqual(selected[obj], []core.AttributeHandle{1}) {
		plan.Release()
		t.Fatal(selected)
	}
	selected[obj][0] = 999
	if !reflect.DeepEqual(plan.SelectAttributes([]core.AttributeHandle{101})[obj], []core.AttributeHandle{1}) {
		plan.Release()
		t.Fatal("selection exposed mutable mappings")
	}
	plan.Release()
	plan.Release()
	if !st.mu.TryLock() {
		t.Fatal("idempotent release leaked object pin")
	}
	st.mu.Unlock()
	if _, _, _, err := plan.ProjectKnownObjectAttributes(context.Background(), "fed", 2, obj, []core.AttributeHandle{1}); !errors.Is(err, core.ErrObjectHandleInvalid) {
		t.Fatal("released pin allowed projection", err)
	}
}

func TestUnpublicationRetainsTerminalRecipientProjection(t *testing.T) {
	r, decl, owners, obj := knownOwnershipPreparationFixture(t)
	decl.SetObjectUnpublication(r, owners)
	ctx := context.Background()
	scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 17}
	transportationOK(t, owners.EnableOwnershipCallbackReceipts(ctx, scope))
	_, err := owners.ExecuteKnownOwnership(ctx, core.OwnershipCommand{Operation: core.OwnershipAcquire, Federation: "fed", Federate: 2, Object: obj, Attributes: []core.AttributeHandle{101}})
	transportationOK(t, err)
	transportationOK(t, decl.UnpublishObjectClassAttributeSet(ctx, "fed", 1, 1, []core.AttributeHandle{1}))
	state := owners.ExportState("fed")
	if len(state.PendingCallbacks) != 1 {
		t.Fatal("mandatory result missing", state.PendingCallbacks)
	}
	terminal := state.PendingCallbacks[0]
	projection := terminal.RetainedProjection
	if projection == nil || projection.RegisteredClass != 1 || projection.KnownClass != 9 || !reflect.DeepEqual(projection.Attributes, []core.AttributeHandle{101}) || !reflect.DeepEqual(terminal.Attributes, []core.AttributeHandle{1}) {
		t.Fatal("terminal result did not preserve canonical ledger and recipient class coordinates", terminal)
	}
	projection.Attributes[0] = 999
	if owners.ExportState("fed").PendingCallbacks[0].RetainedProjection.Attributes[0] != 101 {
		t.Fatal("snapshot exposed retained projection")
	}
	owners.OnObjectDeleted("fed", obj)
	after := owners.ExportState("fed")
	if len(after.PendingCallbacks) != 1 || after.PendingCallbacks[0].RetainedProjection.Attributes[0] != 101 {
		t.Fatal("object retirement erased the outstanding callback's class view")
	}
}
