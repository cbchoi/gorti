package object

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

type reviewUnpublicationBarrier struct {
	core.ObjectClassUnpublicationPreparer
	entered chan struct{}
	proceed chan struct{}
	once    sync.Once
}

func (b *reviewUnpublicationBarrier) PrepareObjectClassUnpublication(ctx context.Context, fed core.FederationName, caller core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) (core.PreparedObjectClassUnpublication, error) {
	b.once.Do(func() { close(b.entered) })
	select {
	case <-b.proceed:
		return b.ObjectClassUnpublicationPreparer.PrepareObjectClassUnpublication(ctx, fed, caller, cls, attrs)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestUnpublicationReviewWholeClassIncludesPublicationAddedBeforeObjectPin(t *testing.T) {
	r, decl, owners, obj := knownOwnershipPreparationFixture(t)
	// Enumeration is optional on core.FOMHandle. Retain the same valid model
	// and attribute projection, but exercise the supported non-enumerating port.
	decl.SetFOMRepository(&roFOMRepository{})
	decl.SetObjectUnpublication(r, owners)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	transportationOK(t, decl.UnpublishObjectClassAttributeSet(ctx, "fed", 1, 1, []core.AttributeHandle{2}))
	if owners.IsOwnedBy("fed", 1, obj, 2) {
		t.Fatal("initial unpublication retained attribute 2 ownership")
	}
	barrier := &reviewUnpublicationBarrier{ObjectClassUnpublicationPreparer: r, entered: make(chan struct{}), proceed: make(chan struct{})}
	decl.SetObjectUnpublication(barrier, owners)
	done := make(chan error, 1)
	go func() { done <- decl.UnpublishObjectClass(ctx, "fed", 1, 1) }()
	select {
	case <-barrier.entered:
	case <-ctx.Done():
		t.Fatal("unpublication did not reach object preparation")
	}
	err := decl.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{2})
	// Seed instance ownership explicitly: publication alone does not grant it.
	owners.RegisterInitialOwnership("fed", 1, obj, []core.AttributeHandle{2})
	newAttributeOwned := owners.IsOwnedBy("fed", 1, obj, 2)
	// Observe both publications after Publish returns and before allowing the
	// whole-class call to proceed: no serial ordering can justify retaining 2.
	observed := decl.PublishedObjectAttributes("fed", 1, 1)
	close(barrier.proceed)
	transportationOK(t, err)
	if !newAttributeOwned {
		t.Fatal("interleaving did not establish attribute 2 ownership")
	}
	if !reflect.DeepEqual(observed, []core.AttributeHandle{1, 2}) {
		t.Fatalf("interleaving did not expose both publications: %v", observed)
	}
	select {
	case err := <-done:
		transportationOK(t, err)
	case <-ctx.Done():
		t.Fatal("unpublication did not finish")
	}
	if attrs := decl.PublishedObjectAttributes("fed", 1, 1); len(attrs) != 0 {
		t.Fatalf("successful whole-class unpublication retained publications added before its object pin: %v", attrs)
	}
	for _, attr := range []core.AttributeHandle{1, 2} {
		if owners.IsOwnedBy("fed", 1, obj, attr) {
			t.Fatalf("whole-class unpublication retained instance ownership of attribute %d", attr)
		}
	}
}

func TestUnpublicationReviewUnpublishedAttributeDoesNotBlockPublishedSubset(t *testing.T) {
	r, decl, owners, obj := knownOwnershipPreparationFixture(t)
	decl.SetObjectUnpublication(r, owners)
	ctx := context.Background()
	_, err := owners.ExecuteKnownOwnership(ctx, core.OwnershipCommand{Operation: core.OwnershipAcquire, Federation: "fed", Federate: 2, Object: obj, Attributes: []core.AttributeHandle{101}})
	transportationOK(t, err)
	// Attribute 2 is valid at the child class but is not part of the caller's
	// child publication. Its removal must not consult the pending parent acquire.
	transportationOK(t, decl.UnpublishObjectClassAttributeSet(ctx, "fed", 2, 1, []core.AttributeHandle{2}))
	if got := decl.PublishedObjectAttributes("fed", 2, 9); !reflect.DeepEqual(got, []core.AttributeHandle{101}) {
		t.Fatalf("unrelated pending publication changed: %v", got)
	}
	if !owners.IsOwnedBy("fed", 1, obj, 1) {
		t.Fatal("other owner's attribute changed")
	}
}

func TestUnpublicationReviewCapacityFailurePreservesDeclarationAndPin(t *testing.T) {
	r, decl, owners, first := knownOwnershipPreparationFixture(t, func(opts *ownership.Options) { opts.CallbackWindow = 1 })
	decl.SetObjectUnpublication(r, owners)
	ctx := context.Background()
	transportationOK(t, owners.EnableOwnershipCallbackReceipts(ctx, core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 17}))
	transportationOK(t, decl.UnsubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{2}))
	reserveFixtureName(t, r, "fed", 1, "review-capacity-second")
	second, _, err := r.Register(ctx, "fed", 1, 1, "review-capacity-second")
	transportationOK(t, err)
	for _, obj := range []core.ObjectHandle{first, second} {
		_, err := owners.ExecuteKnownOwnership(ctx, core.OwnershipCommand{Operation: core.OwnershipAcquire, Federation: "fed", Federate: 2, Object: obj, Attributes: []core.AttributeHandle{101}})
		transportationOK(t, err)
	}
	beforeDecl, beforeOwners := decl.ExportState("fed"), owners.ExportState("fed")
	if err := decl.UnpublishObjectClass(ctx, "fed", 1, 1); !errors.Is(err, core.ErrOwnershipCallbackCapacity) {
		t.Fatalf("capacity failure = %v", err)
	}
	if !reflect.DeepEqual(beforeDecl, decl.ExportState("fed")) || !reflect.DeepEqual(beforeOwners, owners.ExportState("fed")) {
		t.Fatal("rejected multi-object unpublication changed state")
	}
	st := r.stateFor("fed")
	if !st.mu.TryLock() {
		t.Fatal("capacity rejection leaked object pin")
	}
	st.mu.Unlock()
}

func TestUnpublicationReviewPostCommitHookCanReenterAllManagers(t *testing.T) {
	r, decl, owners, obj := knownOwnershipPreparationFixture(t)
	decl.SetObjectUnpublication(r, owners)
	observed := make(chan bool, 1)
	owners.SetOnOwnershipChanged(func(_ context.Context, fed core.FederationName, changed core.ObjectHandle) {
		_, _, known := r.LookupKnownObjectInstance(fed, 1, changed)
		attrs := decl.PublishedObjectAttributes(fed, 1, 1)
		owned := owners.IsOwnedBy(fed, 1, changed, 1)
		observed <- known && reflect.DeepEqual(attrs, []core.AttributeHandle{2}) && !owned
	})
	done := make(chan error, 1)
	go func() {
		done <- decl.UnpublishObjectClassAttributeSet(context.Background(), "fed", 1, 1, []core.AttributeHandle{1})
	}()
	select {
	case err := <-done:
		transportationOK(t, err)
	case <-time.After(3 * time.Second):
		t.Fatalf("post-commit hook deadlocked on object %d", obj)
	}
	select {
	case valid := <-observed:
		if !valid {
			t.Fatal("hook observed inconsistent committed state")
		}
	default:
		t.Fatal("ownership-change hook missing")
	}
}
