package object_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestApplicableSupportOrphanCannotRediscoverOrAcquire(t *testing.T) {
	f := newKnownFixture(t)
	f.subscribe(t, 2, f.child, f.inherited, f.own)
	obj := f.register(t)
	privilege, _ := f.fom.LookupAttribute(f.child, "HLAprivilegeToDeleteObject")
	attrs := []core.AttributeHandle{f.inherited, f.own, privilege}
	knownOK(t, f.owners.UnconditionalDivest(f.ctx, knownFed, 1, obj, attrs))
	for _, attr := range attrs {
		if owner, owned := f.owners.QueryOwnership(knownFed, obj, attr); owned || owner != 0 {
			t.Fatalf("orphan precondition: attribute %d still owned by %d", attr, owner)
		}
	}
	knownOK(t, f.reg.LocalDelete(f.ctx, knownFed, 2, obj))
	knownOK(t, f.reg.LocalDelete(f.ctx, knownFed, 1, obj))
	for _, h := range []core.FederateHandle{1, 2, 3, 4} {
		if _, _, known := f.reg.LookupKnownObjectInstance(knownFed, h, obj); known {
			t.Fatalf("orphan precondition: federate %d still knows instance", h)
		}
		f.drain(h)
	}
	before := f.owners.ExportState(knownFed)
	for _, h := range []core.FederateHandle{1, 2, 3, 4} {
		f.subscribe(t, h, f.child, attrs...)
		knownOK(t, f.decl.PublishObjectClassAttributes(f.ctx, knownFed, h, f.child, attrs))
		for _, event := range f.drain(h) {
			if event.Kind == object.CallbackDiscover {
				t.Fatalf("orphan rediscovered by %d", h)
			}
		}
		if _, _, known := f.reg.LookupKnownObjectInstance(knownFed, h, obj); known {
			t.Fatal("subscription restored orphan knowledge")
		}
		if err := f.owners.Acquire(f.ctx, knownFed, h, obj, []core.AttributeHandle{f.inherited}, nil); !errors.Is(err, core.ErrObjectHandleInvalid) {
			t.Fatalf("unknown orphan acquisition by %d: %v", h, err)
		}
	}
	if !reflect.DeepEqual(before, f.owners.ExportState(knownFed)) {
		t.Fatal("rejected orphan acquisition changed ownership")
	}
}
