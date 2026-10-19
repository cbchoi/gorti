package composition

import (
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

func TestFOMAttributeDimensionContext(t *testing.T) {
	h := NewModelHandle(model.NewFOMWithDimensions([]model.ObjectClass{
		{Name: "Root", Attributes: []model.Attribute{{Name: "Inherited", Dimensions: []string{"Y", "X"}}}},
		{Name: "Child", ParentName: "Root", Attributes: []model.Attribute{{Name: "Own"}}},
	}, nil, nil, []model.Dimension{{Name: "X", UpperBound: 100}, {Name: "Y", UpperBound: 200}}))
	lookup, ok := h.(interface {
		AttributeDimensions(core.ObjectClassHandle, core.AttributeHandle) ([]core.DDMDimensionHandle, bool)
	})
	if !ok {
		t.Fatal("FOM has no attribute dimension context")
	}
	cls, _ := h.LookupObjectClass("Child")
	attr, _ := h.LookupAttribute(cls, "Inherited")
	got, ok := lookup.AttributeDimensions(cls, attr)
	if !ok || !reflect.DeepEqual(got, []core.DDMDimensionHandle{1, 2}) {
		t.Fatalf("inherited dimension context = %v %v", got, ok)
	}
	got[0] = 999
	again, _ := lookup.AttributeDimensions(cls, attr)
	if again[0] != 1 {
		t.Fatal("dimension result aliases live model")
	}
	own, _ := h.LookupAttribute(cls, "Own")
	if dims, ok := lookup.AttributeDimensions(cls, own); !ok || len(dims) != 0 {
		t.Fatalf("dimensionless attribute: %v %v", dims, ok)
	}
	for _, pair := range [][2]uint64{{0, 1}, {999, 1}, {uint64(cls), 0}, {uint64(cls), 999}} {
		if _, ok := lookup.AttributeDimensions(core.ObjectClassHandle(pair[0]), core.AttributeHandle(pair[1])); ok {
			t.Fatalf("invalid pair accepted: %v", pair)
		}
	}
}

func TestFOMAttributeHandlesComplete(t *testing.T) {
	root := model.ObjectClass{Name: "Root", Attributes: []model.Attribute{{Name: "Inherited"}}}
	child := model.ObjectClass{Name: "Child", ParentName: "Root"}
	for i := 0; i < 12; i++ {
		child.Attributes = append(child.Attributes, model.Attribute{Name: string(rune('a' + i))})
	}
	h := NewModelHandle(model.NewFOM([]model.ObjectClass{root, child}, nil, nil)).(*fomHandle)
	cls, _ := h.LookupObjectClass("Child")
	attrs := h.AttributeHandles(cls)
	if len(attrs) != 13 {
		t.Fatalf("attribute enumeration truncated: %v", attrs)
	}
	for i, attr := range attrs {
		if attr != core.AttributeHandle(i+1) {
			t.Fatalf("attribute enumeration = %v", attrs)
		}
	}
	attrs[0] = 99
	if h.AttributeHandles(cls)[0] != 1 {
		t.Fatal("enumeration aliases live state")
	}
	for _, invalid := range []core.ObjectClassHandle{0, 999, ^core.ObjectClassHandle(0)} {
		if got := h.AttributeHandles(invalid); len(got) != 0 {
			t.Fatalf("invalid class %d = %v", invalid, got)
		}
	}
}
