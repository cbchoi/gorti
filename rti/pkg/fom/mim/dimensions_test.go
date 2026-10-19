package mim_test

import (
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

func TestDimensionReferencesStandardMIMHandle(t *testing.T) {
	f, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Dimension{{Name: "HLAfederate", DataType: "HLAnormalizedFederateHandle", NormalizationKey: "Normalize Federate Handle service", DefaultValue: "Excluded"}, {Name: "HLAserviceGroup", DataType: "HLAnormalizedServiceGroup", UpperBound: 7, NormalizationKey: "Normalize Service Group service", DefaultValue: "Excluded"}}
	if got := f.Dimensions(); !reflect.DeepEqual(got, want) {
		t.Errorf("MIM dimension definitions = %+v, want %+v", got, want)
	}
	var attribute, interaction bool
	for _, c := range f.ObjectClasses() {
		for _, a := range c.Attributes {
			if c.Name == "HLAfederate" && a.Name == "HLAfederateHandle" {
				attribute = reflect.DeepEqual(a.Dimensions, []string{"HLAfederate"})
				if attribute {
					a.Dimensions[0] = testDimensionMutation
				}
			}
		}
	}
	for _, c := range f.InteractionClasses() {
		if c.Name == "HLAreportServiceInvocation" {
			interaction = reflect.DeepEqual(c.Dimensions, []string{"HLAfederate", "HLAserviceGroup"})
			if interaction {
				c.Dimensions[0] = testDimensionMutation
			}
		}
	}
	if !attribute || !interaction {
		t.Fatalf("MIM references dropped: attribute=%v interaction=%v", attribute, interaction)
	}
	for _, c := range f.ObjectClasses() {
		for _, a := range c.Attributes {
			for _, d := range a.Dimensions {
				if d == testDimensionMutation {
					t.Fatal("MIM singleton attribute mutation leaked")
				}
			}
		}
	}
	for _, c := range f.InteractionClasses() {
		for _, d := range c.Dimensions {
			if d == testDimensionMutation {
				t.Fatal("MIM singleton interaction mutation leaked")
			}
		}
	}
}

const (
	testDimensionMutation = "mutation"
)

func TestDimensionReferencesMIMMergeCopies(t *testing.T) {
	makeFOM := func(name string) *model.FOM {
		return model.NewFOMWithDimensions([]model.ObjectClass{{Name: name, Attributes: []model.Attribute{{Name: "Position", Dimensions: []string{name}}}}}, []model.InteractionClass{{Name: name, Dimensions: []string{name}}}, nil, []model.Dimension{{Name: name, UpperBound: 100}})
	}
	base, user := makeFOM("Base"), makeFOM("User")
	merged, diags := mim.Merge(base, user)
	if merged == nil || len(diags) != 0 {
		t.Fatalf("Merge: %+v", diags)
	}
	for _, f := range []*model.FOM{base, user, merged} {
		for _, c := range f.ObjectClasses() {
			if !reflect.DeepEqual(c.Attributes[0].Dimensions, []string{c.Name}) {
				t.Fatalf("attribute references changed: %+v", c)
			}
			c.Attributes[0].Dimensions[0] = testDimensionMutation
		}
		for _, c := range f.InteractionClasses() {
			if !reflect.DeepEqual(c.Dimensions, []string{c.Name}) {
				t.Fatalf("interaction references changed: %+v", c)
			}
			c.Dimensions[0] = testDimensionMutation
		}
	}
	for _, c := range merged.ObjectClasses() {
		if !reflect.DeepEqual(c.Attributes[0].Dimensions, []string{c.Name}) {
			t.Fatal("merged attribute references are mutable")
		}
	}
}

func TestDimensionReferencesMIMMergeCompatibility(t *testing.T) {
	base := model.NewFOM(nil, []model.InteractionClass{{Name: "Base", Dimensions: []string{"X", "Y"}}}, nil)
	for _, tc := range []struct {
		name string
		dims []string
		bad  bool
	}{
		{"container", nil, false},
		{"identical", []string{"X", "Y"}, false},
		{"reordered", []string{"Y", "X"}, false},
		{"conflict", []string{"X"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := model.NewFOM(nil, []model.InteractionClass{{Name: "Base", Dimensions: tc.dims}}, nil)
			merged, diags := mim.Merge(base, user)
			if tc.bad {
				if merged != nil || !hasCode(diags, "FOM-101") {
					t.Fatalf("dimension redefinition accepted: %+v", diags)
				}
			} else if merged == nil || len(diags) != 0 || !reflect.DeepEqual(merged.InteractionClasses()[0].Dimensions, []string{"X", "Y"}) {
				t.Fatalf("passthrough lost base dimension context: %+v", diags)
			}
		})
	}
}

func TestDimensionDeclarationsMIMMerge(t *testing.T) {
	definition := model.Dimension{Name: "X", UpperBound: 100, NormalizationKey: "meters"}
	base := model.NewFOMWithDimensions(nil, nil, nil, []model.Dimension{definition})
	for _, tc := range []struct {
		name string
		dim  model.Dimension
		bad  bool
	}{
		{"identical", definition, false},
		{"upper_bound_conflict", model.Dimension{Name: "X", UpperBound: 101, NormalizationKey: "meters"}, true},
		{"normalization_conflict", model.Dimension{Name: "X", UpperBound: 100, NormalizationKey: "feet"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := model.NewFOMWithDimensions(nil, nil, nil, []model.Dimension{tc.dim, tc.dim})
			merged, diags := mim.Merge(base, user)
			if tc.bad {
				if merged != nil || !hasCode(diags, "FOM-101") {
					t.Fatalf("dimension conflict accepted: %+v", diags)
				}
				return
			}
			if merged == nil || len(diags) != 0 || !reflect.DeepEqual(merged.Dimensions(), []model.Dimension{definition}) {
				t.Fatalf("compatible dimension declarations not deduplicated: model=%v diagnostics=%+v", merged, diags)
			}
		})
	}
	for _, inputs := range [][2]*model.FOM{
		{nil, model.NewFOMWithDimensions(nil, nil, nil, []model.Dimension{definition, definition})},
		{model.NewFOMWithDimensions(nil, nil, nil, []model.Dimension{definition, definition}), nil},
	} {
		merged, diags := mim.Merge(inputs[0], inputs[1])
		if merged == nil || len(diags) != 0 || !reflect.DeepEqual(merged.Dimensions(), []model.Dimension{definition}) {
			t.Fatalf("nil-side merge failed to deduplicate dimensions: model=%v diagnostics=%+v", merged, diags)
		}
	}
}
