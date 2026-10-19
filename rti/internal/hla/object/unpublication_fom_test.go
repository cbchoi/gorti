package object_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestUnpublicationValidatesFOMEvenForEmptySet(t *testing.T) {
	f := newKnownFixture(t)
	f.decl.SetObjectUnpublication(f.reg, f.owners)
	before := f.decl.ExportState(knownFed)
	if err := f.decl.UnpublishObjectClassAttributeSet(f.ctx, knownFed, 1, ^core.ObjectClassHandle(0), nil); !errors.Is(err, core.ErrObjectClassNotFound) {
		t.Fatal(err)
	}
	if err := f.decl.UnpublishObjectClassAttributeSet(f.ctx, knownFed, 1, f.child, []core.AttributeHandle{^core.AttributeHandle(0)}); !errors.Is(err, core.ErrAttributeNotFound) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, f.decl.ExportState(knownFed)) {
		t.Fatal("FOM rejection changed publication")
	}
}
