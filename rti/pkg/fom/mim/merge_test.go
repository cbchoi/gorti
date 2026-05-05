package mim_test

import (
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// hasCode reports whether diags contains at least one diagnostic with the
// given code.
func hasCode(diags []mim.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

// TestSpec_Merge_UserRedefinesObjectClass_EmitsFOM101 asserts that when a
// user FOM declares an MIM-provided object class (e.g. HLAobjectRoot) with
// new attributes, Merge reports FOM-101 and returns a nil merged FOM.
//
// Per the brief: "User declares a NEW class with parent HLAobjectRoot →
// no FOM-101 (legitimate inheritance)" — pass-through use of the MIM root
// is excluded by the implementation's emptiness check, exercised in a
// separate test below.
//
// Implements: FR-FOM-3, FR-FOM-4.
func TestSpec_Merge_UserRedefinesObjectClass_EmitsFOM101(t *testing.T) {
	t.Parallel()

	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatalf("StandardMIMHandle: %v", err)
	}
	user := model.NewFOM(
		[]model.ObjectClass{
			{
				Name: "HLAobjectRoot",
				Attributes: []model.Attribute{
					{Name: "userInjected", DataType: "HLAinteger32BE"},
				},
			},
		},
		nil, nil,
	)

	merged, diags := mim.Merge(base, user)
	if !hasCode(diags, "FOM-101") {
		t.Fatalf("expected FOM-101 for HLAobjectRoot redefinition; got %+v", diags)
	}
	if merged != nil {
		t.Errorf("merged FOM should be nil on FOM-101; got %+v", merged)
	}
}

// TestSpec_Merge_UserRedefinesDataType_EmitsFOM101 asserts that any user
// dataType whose name matches an MIM dataType triggers FOM-101 — there is
// no pass-through pattern for dataTypes (a dataType declaration is always
// a definition).
//
// Implements: FR-FOM-3, FR-FOM-4.
func TestSpec_Merge_UserRedefinesDataType_EmitsFOM101(t *testing.T) {
	t.Parallel()

	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatalf("StandardMIMHandle: %v", err)
	}
	user := model.NewFOM(nil, nil, []model.DataType{
		&model.BasicData{NameField: "HLAinteger32BE", Size: 32, Endian: "Big"},
	})

	merged, diags := mim.Merge(base, user)
	if !hasCode(diags, "FOM-101") {
		t.Fatalf("expected FOM-101 for HLAinteger32BE redefinition; got %+v", diags)
	}
	if merged != nil {
		t.Errorf("merged FOM should be nil on FOM-101; got %+v", merged)
	}
}

// TestSpec_Merge_UserExtendsHLAobjectRoot_NoFOM101 asserts that declaring a
// brand-new user class that lists HLAobjectRoot as its parent is legitimate
// inheritance, not redefinition. No FOM-101.
//
// Implements: FR-FOM-3 (positive path).
func TestSpec_Merge_UserExtendsHLAobjectRoot_NoFOM101(t *testing.T) {
	t.Parallel()

	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatalf("StandardMIMHandle: %v", err)
	}
	user := model.NewFOM(
		[]model.ObjectClass{
			{Name: "HLAobjectRoot"}, // pass-through container
			{Name: "Vehicle", ParentName: "HLAobjectRoot",
				Attributes: []model.Attribute{
					{Name: "speed", DataType: "HLAfloat64BE"},
				}},
		},
		nil, nil,
	)

	merged, diags := mim.Merge(base, user)
	if hasCode(diags, "FOM-101") {
		t.Fatalf("unexpected FOM-101 for legitimate inheritance: %+v", diags)
	}
	if merged == nil {
		t.Fatal("merged FOM should not be nil when no FOM-101 emitted")
	}
	// Vehicle must appear in the merged FOM.
	found := false
	for _, oc := range merged.ObjectClasses() {
		if oc.Name == "Vehicle" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("merged FOM missing user-declared class Vehicle")
	}
}

// TestSpec_Merge_UserAddsNewDataType_NoFOM101 asserts that a user dataType
// with a name not in the MIM is added to the merged FOM without diagnostics.
//
// Implements: FR-FOM-3 (positive path).
func TestSpec_Merge_UserAddsNewDataType_NoFOM101(t *testing.T) {
	t.Parallel()

	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatalf("StandardMIMHandle: %v", err)
	}
	user := model.NewFOM(nil, nil, []model.DataType{
		&model.BasicData{NameField: "MyAppInteger24BE", Size: 24, Endian: "Big"},
	})

	merged, diags := mim.Merge(base, user)
	if hasCode(diags, "FOM-101") {
		t.Fatalf("unexpected FOM-101 for novel dataType: %+v", diags)
	}
	if merged == nil {
		t.Fatal("merged FOM should not be nil when no FOM-101 emitted")
	}
	foundUser := false
	foundMIM := false
	for _, dt := range merged.DataTypes() {
		switch dt.Name() {
		case "MyAppInteger24BE":
			foundUser = true
		case "HLAfloat64BE":
			foundMIM = true
		}
	}
	if !foundUser {
		t.Errorf("merged FOM missing user dataType MyAppInteger24BE")
	}
	if !foundMIM {
		t.Errorf("merged FOM missing MIM dataType HLAfloat64BE (base must be carried through)")
	}
}

// TestSpec_Merge_EmptyUser_ReturnsBaseUnchanged asserts that an empty user
// FOM produces a merged FOM whose declarations equal the MIM base.
//
// Implements: FR-FOM-3 (degenerate path).
func TestSpec_Merge_EmptyUser_ReturnsBaseUnchanged(t *testing.T) {
	t.Parallel()

	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatalf("StandardMIMHandle: %v", err)
	}
	user := model.NewFOM(nil, nil, nil)

	merged, diags := mim.Merge(base, user)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics on empty user FOM: %+v", diags)
	}
	if merged == nil {
		t.Fatal("merged FOM should not be nil")
	}
	if got, want := len(merged.ObjectClasses()), len(base.ObjectClasses()); got != want {
		t.Errorf("ObjectClasses len = %d; want %d", got, want)
	}
	if got, want := len(merged.InteractionClasses()), len(base.InteractionClasses()); got != want {
		t.Errorf("InteractionClasses len = %d; want %d", got, want)
	}
	if got, want := len(merged.DataTypes()), len(base.DataTypes()); got != want {
		t.Errorf("DataTypes len = %d; want %d", got, want)
	}
}

// TestSpec_Merge_UserRedefinesInteractionClass_EmitsFOM101 asserts that a
// user FOM that adds parameters to HLAinteractionRoot trips FOM-101 — the
// MIM-provided root must remain a pass-through inheritance node.
func TestSpec_Merge_UserRedefinesInteractionClass_EmitsFOM101(t *testing.T) {
	t.Parallel()

	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatalf("StandardMIMHandle: %v", err)
	}
	user := model.NewFOM(
		nil,
		[]model.InteractionClass{
			{
				Name: "HLAinteractionRoot",
				Parameters: []model.Parameter{
					{Name: "userInjected", DataType: "HLAinteger32BE"},
				},
			},
		},
		nil,
	)

	merged, diags := mim.Merge(base, user)
	if !hasCode(diags, "FOM-101") {
		t.Fatalf("expected FOM-101 for HLAinteractionRoot redefinition; got %+v", diags)
	}
	if merged != nil {
		t.Errorf("merged FOM should be nil on FOM-101; got %+v", merged)
	}
}

// TestSpec_Merge_DiagnosticCarriesName ensures the FOM-101 diagnostic
// message names the offending MIM type so a downstream consumer can localize.
func TestSpec_Merge_DiagnosticCarriesName(t *testing.T) {
	t.Parallel()

	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatalf("StandardMIMHandle: %v", err)
	}
	user := model.NewFOM(nil, nil, []model.DataType{
		&model.BasicData{NameField: "HLAfloat64BE", Size: 64, Endian: "Big"},
	})

	_, diags := mim.Merge(base, user)
	if len(diags) == 0 {
		t.Fatal("expected a FOM-101 diagnostic")
	}
	found := false
	for _, d := range diags {
		if d.Code == "FOM-101" {
			if msg := d.Message; len(msg) == 0 {
				t.Errorf("FOM-101 diagnostic has empty Message")
			} else if !contains(msg, "HLAfloat64BE") {
				t.Errorf("FOM-101 message %q does not name the offending type HLAfloat64BE", msg)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("no FOM-101 diagnostic in %+v", diags)
	}
}

// TestSpec_Merge_UserDimensionsPreserved asserts that user-declared
// <dimensions> survive the MIM merge. Regression for the M12 W2
// finding (Agent C report, deferral #2) where mergeNoCollision called
// model.NewFOM and silently dropped the dimensions slice.
//
// Implements: FR-DDM-1 (dimensions reachable post-MIM merge).
func TestSpec_Merge_UserDimensionsPreserved(t *testing.T) {
	t.Parallel()

	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatalf("StandardMIMHandle: %v", err)
	}
	user := &userFOMWithDimensions{
		dims: []model.Dimension{
			{Name: "BearingDeg", UpperBound: 360},
			{Name: "RangeKm", UpperBound: 1000},
		},
	}

	merged, diags := mim.Merge(base, user.fom())
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
	if merged == nil {
		t.Fatal("merged FOM is nil")
	}
	got := merged.Dimensions()
	if len(got) != 2 {
		t.Fatalf("merged FOM has %d dimensions; want 2", len(got))
	}
	if got[0].Name != "BearingDeg" || got[1].Name != "RangeKm" {
		t.Errorf("merged dimensions = %+v; want [BearingDeg RangeKm] (sorted)", got)
	}
}

type userFOMWithDimensions struct {
	dims []model.Dimension
}

func (u *userFOMWithDimensions) fom() *model.FOM {
	return model.NewFOMWithDimensions(nil, nil, nil, u.dims)
}

// contains is a tiny substring helper to avoid pulling in strings just for
// one assertion; keeps the test file self-contained.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
