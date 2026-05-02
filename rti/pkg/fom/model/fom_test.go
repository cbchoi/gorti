package model

import (
	"testing"
)

const (
	mutatedSentinel  = "MUTATED"
	tamperedSentinel = "TAMPERED"
)

// TestNewFOM_NilSlices_ReturnsEmptyFOM asserts that the zero-input case
// produces a non-nil FOM whose accessors return empty slices. Callers can
// rely on this to avoid nil-checking everywhere.
func TestNewFOM_NilSlices_ReturnsEmptyFOM(t *testing.T) {
	t.Parallel()

	f := NewFOM(nil, nil, nil)
	if f == nil {
		t.Fatal("NewFOM(nil, nil, nil) returned nil; want non-nil empty FOM")
	}
	if got := f.ObjectClasses(); len(got) != 0 {
		t.Errorf("ObjectClasses() len = %d; want 0", len(got))
	}
	if got := f.InteractionClasses(); len(got) != 0 {
		t.Errorf("InteractionClasses() len = %d; want 0", len(got))
	}
	if got := f.DataTypes(); len(got) != 0 {
		t.Errorf("DataTypes() len = %d; want 0", len(got))
	}
}

// TestNewFOM_SortsObjectClassesByName confirms iteration order is
// deterministic regardless of construction order.
func TestNewFOM_SortsObjectClassesByName(t *testing.T) {
	t.Parallel()

	in := []ObjectClass{
		{Name: "Vehicle"},
		{Name: "Aircraft"},
		{Name: "HLAobjectRoot"},
	}
	f := NewFOM(in, nil, nil)

	got := f.ObjectClasses()
	want := []string{"Aircraft", "HLAobjectRoot", "Vehicle"}
	if len(got) != len(want) {
		t.Fatalf("ObjectClasses() len = %d; want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("ObjectClasses()[%d].Name = %q; want %q", i, got[i].Name, name)
		}
	}
}

// TestNewFOM_SortsInteractionClassesByName confirms iteration order is
// deterministic regardless of construction order.
func TestNewFOM_SortsInteractionClassesByName(t *testing.T) {
	t.Parallel()

	in := []InteractionClass{
		{Name: "Honk"},
		{Name: "Brake"},
		{Name: "HLAinteractionRoot"},
	}
	f := NewFOM(nil, in, nil)

	got := f.InteractionClasses()
	want := []string{"Brake", "HLAinteractionRoot", "Honk"}
	if len(got) != len(want) {
		t.Fatalf("InteractionClasses() len = %d; want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("InteractionClasses()[%d].Name = %q; want %q", i, got[i].Name, name)
		}
	}
}

// TestNewFOM_SortsDataTypesByName confirms data type iteration is
// deterministic regardless of registration order.
func TestNewFOM_SortsDataTypesByName(t *testing.T) {
	t.Parallel()

	in := []DataType{
		&BasicData{NameField: "HLAinteger32BE"},
		&BasicData{NameField: "HLAfloat64BE"},
		&BasicData{NameField: "HLAoctet"},
	}
	f := NewFOM(nil, nil, in)

	got := f.DataTypes()
	want := []string{"HLAfloat64BE", "HLAinteger32BE", "HLAoctet"}
	if len(got) != len(want) {
		t.Fatalf("DataTypes() len = %d; want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name() != name {
			t.Errorf("DataTypes()[%d].Name() = %q; want %q", i, got[i].Name(), name)
		}
	}
}

// TestNewFOM_DefensiveCopyOfInputSlices asserts the constructor copies
// caller-supplied slices, so post-construction mutation by the caller
// cannot reach into the FOM.
func TestNewFOM_DefensiveCopyOfInputSlices(t *testing.T) {
	t.Parallel()

	objects := []ObjectClass{{Name: "Bravo"}, {Name: "Alpha"}}
	interactions := []InteractionClass{{Name: "Honk"}}
	data := []DataType{&BasicData{NameField: "HLAoctet"}}

	f := NewFOM(objects, interactions, data)

	// Mutate caller's slices; the FOM must not observe these mutations.
	objects[0].Name = mutatedSentinel
	interactions[0].Name = mutatedSentinel
	data[0] = &BasicData{NameField: mutatedSentinel}

	for _, oc := range f.ObjectClasses() {
		if oc.Name == mutatedSentinel {
			t.Errorf("FOM.ObjectClasses observed caller mutation of input slice")
		}
	}
	for _, ic := range f.InteractionClasses() {
		if ic.Name == mutatedSentinel {
			t.Errorf("FOM.InteractionClasses observed caller mutation of input slice")
		}
	}
	for _, dt := range f.DataTypes() {
		if dt.Name() == mutatedSentinel {
			t.Errorf("FOM.DataTypes observed caller mutation of input slice")
		}
	}
}

// TestObjectClasses_ReturnsCopy asserts the accessor's return value cannot
// be used as a write handle into FOM internal state.
func TestObjectClasses_ReturnsCopy(t *testing.T) {
	t.Parallel()

	in := []ObjectClass{{Name: "Alpha"}, {Name: "Bravo"}}
	f := NewFOM(in, nil, nil)

	got := f.ObjectClasses()
	if len(got) == 0 {
		t.Fatal("expected non-empty ObjectClasses")
	}
	got[0].Name = tamperedSentinel

	again := f.ObjectClasses()
	for _, oc := range again {
		if oc.Name == tamperedSentinel {
			t.Fatalf("ObjectClasses() returned writable handle to internal state")
		}
	}
}

// TestInteractionClasses_ReturnsCopy mirrors TestObjectClasses_ReturnsCopy
// for the interaction class accessor.
func TestInteractionClasses_ReturnsCopy(t *testing.T) {
	t.Parallel()

	in := []InteractionClass{{Name: "Alpha"}, {Name: "Bravo"}}
	f := NewFOM(nil, in, nil)

	got := f.InteractionClasses()
	if len(got) == 0 {
		t.Fatal("expected non-empty InteractionClasses")
	}
	got[0].Name = tamperedSentinel

	again := f.InteractionClasses()
	for _, ic := range again {
		if ic.Name == tamperedSentinel {
			t.Fatalf("InteractionClasses() returned writable handle to internal state")
		}
	}
}
