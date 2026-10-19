package object_test

import (
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestPrivilegeInitialOwnershipFollowsPublicationLifetime(t *testing.T) {
	f := newKnownFixture(t)
	f.decl.SetObjectUnpublication(f.reg, f.owners)
	priv, _ := f.fom.LookupAttribute(f.child, "HLAprivilegeToDeleteObject")
	first := f.register(t)
	if !f.owners.IsOwnedBy(knownFed, 1, first, priv) {
		t.Fatal("implicit publication did not initialize privilege ownership")
	}
	knownOK(t, f.decl.UnpublishObjectClassAttributeSet(f.ctx, knownFed, 1, f.child, []core.AttributeHandle{priv}))
	if f.owners.IsOwnedBy(knownFed, 1, first, priv) {
		t.Fatal("explicit unpublication retained privilege ownership")
	}
	second := f.register(t)
	if f.owners.IsOwnedBy(knownFed, 1, second, priv) {
		t.Fatal("registration recreated explicitly unpublished privilege")
	}
	knownOK(t, f.decl.UnpublishObjectClass(f.ctx, knownFed, 1, f.child))
	knownOK(t, f.decl.PublishObjectClassAttributes(f.ctx, knownFed, 1, f.child, []core.AttributeHandle{f.inherited}))
	third := f.register(t)
	if !f.owners.IsOwnedBy(knownFed, 1, third, priv) {
		t.Fatal("new lifetime failed to initialize implicit privilege ownership")
	}
	knownOK(t, f.decl.UnpublishObjectClassAttributeSet(f.ctx, knownFed, 1, f.child, []core.AttributeHandle{f.inherited}))
	if f.owners.IsOwnedBy(knownFed, 1, third, priv) || len(f.decl.PublishedObjectAttributes(knownFed, 1, f.child)) != 0 {
		t.Fatal("last explicit unpublication retained implicit privilege or ownership")
	}
}
