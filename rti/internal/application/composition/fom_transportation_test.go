package composition

import (
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

func TestFOMTransportationInheritanceAndUnknownHandles(t *testing.T) {
	h := NewModelHandle(model.NewFOM([]model.ObjectClass{
		{Name: "Root", Attributes: []model.Attribute{{Name: "Inherited", Transportation: "HLAbestEffort"}}},
		{Name: "Child", ParentName: "Root", Attributes: []model.Attribute{{Name: "Own", Transportation: "HLAreliable"}}},
	}, []model.InteractionClass{
		{Name: "Root", Transportation: "HLAbestEffort"},
		{Name: "Child", ParentName: "Root"},
		{Name: "Override", ParentName: "Root", Transportation: "HLAreliable"},
		{Name: "Unknown", Transportation: "custom"},
	}, nil)).(*fomHandle)
	cls, _ := h.LookupObjectClass("Child")
	for name, want := range map[string]core.TransportType{"Own": core.TransportTypeReliable, "Inherited": core.TransportTypeBestEffort} {
		attr, _ := h.LookupAttribute(cls, name)
		if got, ok := h.TransportationForAttribute(cls, attr); !ok || got != want {
			t.Fatalf("%s: %v %v", name, got, ok)
		}
	}
	for name, want := range map[string]core.TransportType{"Root": core.TransportTypeBestEffort, "Child": core.TransportTypeBestEffort, "Override": core.TransportTypeReliable} {
		ic, _ := h.LookupInteractionClass(name)
		if got, ok := h.TransportationForInteraction(ic); !ok || got != want {
			t.Fatalf("%s: %v %v", name, got, ok)
		}
	}
	for _, pair := range [][2]uint64{{0, 1}, {999, 1}, {uint64(cls), 0}, {uint64(cls), 999}} {
		if got, ok := h.TransportationForAttribute(core.ObjectClassHandle(pair[0]), core.AttributeHandle(pair[1])); ok || got != core.TransportTypeUnspecified {
			t.Fatalf("invalid pair %v accepted", pair)
		}
	}
	for _, ic := range []core.InteractionClassHandle{0, 999, 1 << 32, 1 << 63, math.MaxUint64} {
		if _, ok := h.TransportationForInteraction(ic); ok {
			t.Fatal("invalid interaction accepted")
		}
		if _, ok := h.InteractionDimensions(ic); ok {
			t.Fatal("invalid interaction dimensions accepted")
		}
	}
	unknown, _ := h.LookupInteractionClass("Unknown")
	if _, ok := h.TransportationForInteraction(unknown); ok {
		t.Fatal("unsupported declared transport became a fabricated default")
	}
}
