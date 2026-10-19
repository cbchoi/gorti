package ddm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
)

func forgetObject(t *testing.T, m *ddm.Manager, obj core.ObjectHandle) {
	t.Helper()
	forgetter, ok := any(m).(interface {
		ForgetObject(core.FederationName, core.ObjectHandle)
	})
	if !ok {
		t.Fatal("DDM manager does not implement ForgetObject")
	}
	forgetter.ForgetObject(testFed, obj)
}

func TestForgetObjectReleasesRegistrationRegions(t *testing.T) {
	m := newManager(t, registrationFOM())
	rh := newRegion(t, m, testOwner, 1)
	tx := prepare(t, m, map[core.AttributeHandle][]core.DDMRegionHandleCore{1: {rh}})
	tx.Commit(42)
	tx.Release()
	forgetObject(t, m, 42)
	forgetObject(t, m, 42)
	if m.HasObjectAssociations(testFed, 42) {
		t.Fatal("deleted object still has region associations")
	}
	if err := m.DeleteRegion(context.Background(), testFed, testOwner, rh); err != nil {
		t.Fatalf("delete region after object cleanup: %v", err)
	}
}

func TestForgetObjectPreservesOtherUses(t *testing.T) {
	ctx := context.Background()
	m := newManager(t, registrationFOM())
	rh := newRegion(t, m, testOwner, 1)
	for _, obj := range []core.ObjectHandle{42, 43} {
		tx := prepare(t, m, map[core.AttributeHandle][]core.DDMRegionHandleCore{1: {rh}})
		tx.Commit(obj)
		tx.Release()
	}
	if err := m.SubscribeObjectClassAttributesWithRegions(ctx, testFed, testOwner, testClass, []core.AttributeHandle{1}, []core.DDMRegionHandleCore{rh}); err != nil {
		t.Fatal(err)
	}
	forgetObject(t, m, 42)
	if !m.HasObjectAssociations(testFed, 43) {
		t.Fatal("cleanup removed another object's associations")
	}
	if err := m.DeleteRegion(ctx, testFed, testOwner, rh); !errors.Is(err, core.ErrRegionInUse) {
		t.Fatalf("region still used by other object: %v", err)
	}
	forgetObject(t, m, 43)
	if err := m.DeleteRegion(ctx, testFed, testOwner, rh); !errors.Is(err, core.ErrRegionInUse) {
		t.Fatalf("region still used by subscription: %v", err)
	}
	if err := m.UnsubscribeObjectClassAttributesWithRegions(ctx, testFed, testOwner, testClass, []core.AttributeHandle{1}, []core.DDMRegionHandleCore{rh}); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteRegion(ctx, testFed, testOwner, rh); err != nil {
		t.Fatalf("region should be usable after final association removal: %v", err)
	}
}

func TestForgetObjectMissingFederationIsNoop(t *testing.T) {
	m := newManager(t, registrationFOM())
	forgetObject(t, m, 42)
	if m.ExportState(testFed) != nil {
		t.Fatal("cleanup created federation state")
	}
}
