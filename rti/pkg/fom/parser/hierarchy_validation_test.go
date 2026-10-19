package parser

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// Both trees have Left.Shared.Leaf and Right.Shared.Leaf. Each Shared
// declares a different member so following the other branch either invents
// a duplicate or hides a real one.
func TestSpec_Hierarchy_BranchLocalInheritedDuplicates(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		leftMember  string
		rightMember string
		want        []string
	}{
		{"valid_repeated_leafs", "rightValue", "leftValue", nil},
		{"left_only", "leftValue", "leftValue", []string{"Left"}},
		{"right_only", "rightValue", "rightValue", []string{"Right"}},
		{"both_branches", "leftValue", "rightValue", []string{"Left", "Right"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			xml := fmt.Sprintf(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>branch-local-members</name><type>FOM</type></modelIdentification>
  <objects><objectClass><name>HLAobjectRoot</name>
    <objectClass><name>Left</name>
      <objectClass><name>Shared</name>
        <attribute><name>leftValue</name><dataType>HLAinteger32BE</dataType></attribute>
        <objectClass><name>Leaf</name>
          <attribute><name>%s</name><dataType>HLAinteger32BE</dataType></attribute>
        </objectClass>
      </objectClass>
    </objectClass>
    <objectClass><name>Right</name>
      <objectClass><name>Shared</name>
        <attribute><name>rightValue</name><dataType>HLAinteger32BE</dataType></attribute>
        <objectClass><name>Leaf</name>
          <attribute><name>%s</name><dataType>HLAinteger32BE</dataType></attribute>
        </objectClass>
      </objectClass>
    </objectClass>
  </objectClass></objects>
  <interactions><interactionClass><name>HLAinteractionRoot</name>
    <interactionClass><name>Left</name>
      <interactionClass><name>Shared</name>
        <parameter><name>leftValue</name><dataType>HLAinteger32BE</dataType></parameter>
        <interactionClass><name>Leaf</name>
          <parameter><name>%s</name><dataType>HLAinteger32BE</dataType></parameter>
        </interactionClass>
      </interactionClass>
    </interactionClass>
    <interactionClass><name>Right</name>
      <interactionClass><name>Shared</name>
        <parameter><name>rightValue</name><dataType>HLAinteger32BE</dataType></parameter>
        <interactionClass><name>Leaf</name>
          <parameter><name>%s</name><dataType>HLAinteger32BE</dataType></parameter>
        </interactionClass>
      </interactionClass>
    </interactionClass>
  </interactionClass></interactions>
</objectModel>`, tc.leftMember, tc.rightMember, tc.leftMember, tc.rightMember)
			res, err := Parse([]Module{{Path: "branches.xml", XML: []byte(xml)}})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			for code, root := range map[string]string{"FOM-004": objectClassRootName, "FOM-005": interactionClassRootName} {
				assertHierarchyDiagnosticClasses(t, res.Diagnostics, code, root, tc.want)
			}
			if len(res.Diagnostics) != 2*len(tc.want) {
				t.Errorf("unexpected diagnostics: %+v", res.Diagnostics)
			}
			if (res.FOM != nil) != (len(tc.want) == 0) {
				t.Errorf("FOM availability does not match validation result: %+v", res)
			}
		})
	}
}

func TestSpec_Hierarchy_QualifiedParentResolution(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		classes []model.ObjectClass
		want    []string
	}{
		{
			name: "existing_qualified_parent",
			classes: []model.ObjectClass{
				{Name: "Shared", QualifiedName: "HLAobjectRoot.Shared", ParentName: objectClassRootName},
				{Name: "Leaf", QualifiedName: "HLAobjectRoot.Shared.Leaf", ParentName: "Shared"},
			},
		},
		{
			name: "other_branch_cannot_supply_parent",
			classes: []model.ObjectClass{
				{Name: "Left", QualifiedName: "HLAobjectRoot.Left", ParentName: objectClassRootName},
				{Name: "Right", QualifiedName: "HLAobjectRoot.Right", ParentName: objectClassRootName},
				{Name: "Shared", QualifiedName: "HLAobjectRoot.Left.Shared", ParentName: "Left"},
				{Name: "Leaf", QualifiedName: "HLAobjectRoot.Right.Shared.Leaf", ParentName: "Shared"},
			},
			want: []string{"HLAobjectRoot.Right.Shared.Leaf"},
		},
		{
			name: "missing_parents_reported_per_identity",
			classes: []model.ObjectClass{
				{Name: "Leaf", QualifiedName: "HLAobjectRoot.Left.Shared.Leaf", ParentName: "Shared"},
				{Name: "Leaf", QualifiedName: "HLAobjectRoot.Right.Shared.Leaf", ParentName: "Shared"},
			},
			want: []string{"HLAobjectRoot.Left.Shared.Leaf", "HLAobjectRoot.Right.Shared.Leaf"},
		},
		{
			name: "root_leaf_is_not_root_identity",
			classes: []model.ObjectClass{
				{Name: objectClassRootName, QualifiedName: "HLAobjectRoot.Missing.HLAobjectRoot", ParentName: "Missing"},
			},
			want: []string{"HLAobjectRoot.Missing.HLAobjectRoot"},
		},
		{
			name: "legacy_missing_parent",
			classes: []model.ObjectClass{
				{Name: "Leaf", ParentName: "Missing"},
			},
			want: []string{"Leaf"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			interactions := make([]model.InteractionClass, len(tc.classes))
			for i, c := range tc.classes {
				interactions[i] = model.InteractionClass{
					Name:          strings.ReplaceAll(c.Name, objectClassRootName, interactionClassRootName),
					QualifiedName: strings.ReplaceAll(c.QualifiedName, objectClassRootName, interactionClassRootName),
					ParentName:    strings.ReplaceAll(c.ParentName, objectClassRootName, interactionClassRootName),
				}
			}
			in := diagnosticInput{modulePath: "parent-graph", fom: model.NewFOM(tc.classes, interactions, nil)}
			for _, v := range []struct {
				validator diagnoser
				code      string
				root      string
			}{
				{parentValidator{}, "FOM-011", objectClassRootName},
				{interactionParentValidator{}, "FOM-012", interactionClassRootName},
			} {
				diags := v.validator.Run(in)
				if len(diags) != len(tc.want) {
					t.Errorf("want %d %s diagnostics, got %+v", len(tc.want), v.code, diags)
				}
				for _, key := range tc.want {
					key = strings.ReplaceAll(key, objectClassRootName, v.root)
					found := false
					for _, d := range diags {
						if strings.Contains(d.Message, fmt.Sprintf("class %q", key)) && d.Code == v.code && d.ModulePath == in.modulePath {
							found = true
						}
					}
					if !found {
						t.Errorf("missing %s diagnostic for %s: %+v", v.code, key, diags)
					}
				}
			}
		})
	}
}

func TestSpec_Hierarchy_RepeatedAncestorLeafMembers(t *testing.T) {
	t.Parallel()

	for _, member := range []string{"distinct", "shared"} {
		t.Run(member, func(t *testing.T) {
			xml := fmt.Sprintf(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>repeated-ancestor-leaf</name><type>FOM</type></modelIdentification>
  <objects><objectClass><name>HLAobjectRoot</name>
    <objectClass><name>A</name>
      <attribute><name>shared</name><dataType>HLAinteger32BE</dataType></attribute>
      <objectClass><name>A</name>
        <attribute><name>%s</name><dataType>HLAinteger32BE</dataType></attribute>
      </objectClass>
    </objectClass>
  </objectClass></objects>
  <interactions><interactionClass><name>HLAinteractionRoot</name>
    <interactionClass><name>A</name>
      <parameter><name>shared</name><dataType>HLAinteger32BE</dataType></parameter>
      <interactionClass><name>A</name>
        <parameter><name>%s</name><dataType>HLAinteger32BE</dataType></parameter>
      </interactionClass>
    </interactionClass>
  </interactionClass></interactions>
</objectModel>`, member, member)
			res, err := Parse([]Module{{Path: "ancestor.xml", XML: []byte(xml)}})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			wantCount := 0
			if member == "shared" {
				wantCount = 1
			}
			for code, root := range map[string]string{"FOM-004": objectClassRootName, "FOM-005": interactionClassRootName} {
				if res.CodeCounts()[code] != wantCount {
					t.Errorf("want %d %s diagnostics, got %+v", wantCount, code, res.Diagnostics)
				}
				for _, d := range res.Diagnostics {
					if d.Code == code && (!strings.Contains(d.Message, fmt.Sprintf("class %q", root+".A.A")) ||
						!strings.Contains(d.Message, fmt.Sprintf("also declared on %q", root+".A"))) {
						t.Errorf("want distinct ancestor and child identities: %+v", d)
					}
				}
			}
			if len(res.Diagnostics) != 2*wantCount || (res.FOM != nil) != (wantCount == 0) {
				t.Errorf("unexpected validation result: %+v", res)
			}
		})
	}
}

func TestSpec_Hierarchy_DuplicateReportsUseQualifiedIdentity(t *testing.T) {
	t.Parallel()

	for _, inherited := range []bool{false, true} {
		t.Run(fmt.Sprintf("inherited_%t", inherited), func(t *testing.T) {
			objects := []model.ObjectClass{{Name: objectClassRootName, QualifiedName: objectClassRootName}}
			interactions := []model.InteractionClass{{Name: interactionClassRootName, QualifiedName: interactionClassRootName}}
			if inherited {
				objects[0].Attributes = []model.Attribute{{Name: "shared"}}
				interactions[0].Parameters = []model.Parameter{{Name: "shared"}}
			}
			for _, branch := range []string{"Left", "Right"} {
				objects = append(objects,
					model.ObjectClass{Name: branch, QualifiedName: "HLAobjectRoot." + branch, ParentName: objectClassRootName},
					model.ObjectClass{Name: "Leaf", QualifiedName: "HLAobjectRoot." + branch + ".Leaf", ParentName: branch, Attributes: []model.Attribute{{Name: "shared"}}},
				)
				interactions = append(interactions,
					model.InteractionClass{Name: branch, QualifiedName: "HLAinteractionRoot." + branch, ParentName: interactionClassRootName},
					model.InteractionClass{Name: "Leaf", QualifiedName: "HLAinteractionRoot." + branch + ".Leaf", ParentName: branch, Parameters: []model.Parameter{{Name: "shared"}}},
				)
				if !inherited {
					leaf := len(objects) - 1
					objects[leaf].Attributes = append(objects[leaf].Attributes, model.Attribute{Name: "shared"})
					interactions[leaf].Parameters = append(interactions[leaf].Parameters, model.Parameter{Name: "shared"})
				}
			}
			in := diagnosticInput{fom: model.NewFOM(objects, interactions, nil)}
			for _, v := range []diagnoser{duplicateAttributeValidator{}, duplicateParameterValidator{}} {
				if diags := v.Run(in); len(diags) != 2 {
					t.Errorf("want two branch-specific duplicate reports, got %+v", diags)
				}
			}
		})
	}
}

func assertHierarchyDiagnosticClasses(t *testing.T, diags []Diagnostic, code, root string, branches []string) {
	t.Helper()
	want := map[string]bool{}
	for _, branch := range branches {
		want[root+"."+branch+".Shared.Leaf"] = false
	}
	count := 0
	for _, d := range diags {
		if d.Code != code {
			continue
		}
		count++
		if d.ModulePath != "branches.xml" {
			t.Errorf("lost module path: %+v", d)
		}
		matched := false
		for key, seen := range want {
			if strings.Contains(d.Message, fmt.Sprintf("class %q", key)) {
				if seen {
					t.Errorf("duplicate diagnostic for %s: %+v", key, d)
				}
				want[key] = true
				matched = true
			}
		}
		if !matched {
			t.Errorf("unexpected %s class identity: %+v", code, d)
		}
	}
	if count != len(want) {
		t.Errorf("want %d %s diagnostics, got %d: %+v", len(want), code, count, diags)
	}
	for key, seen := range want {
		if !seen {
			t.Errorf("missing %s diagnostic for %s", code, key)
		}
	}
}
