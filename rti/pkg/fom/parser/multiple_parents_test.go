package parser

import (
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// TestSpec_MultipleParents_AcceptsSingleParent asserts a class declared
// once does not produce FOM-003.
func TestSpec_MultipleParents_AcceptsSingleParent(t *testing.T) {
	t.Parallel()

	res, err := Parse([]Module{{Path: "x.xml", XML: []byte(minimalFOM)}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, d := range res.Diagnostics {
		if d.Code == "FOM-003" {
			t.Fatalf("unexpected FOM-003 on rooted FOM: %+v", d)
		}
	}
}

// The old RejectsTwoParents assertion treated equal leaf names as one
// identity. HLAobjectRoot.A and HLAobjectRoot.B.A each have one parent;
// the invalid single-identity graph is tested separately below.
func TestSpec_MultipleParents_AcceptsRepeatedBranchLeaf(t *testing.T) {
	t.Parallel()

	xml := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>x</name><type>FOM</type></modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass><name>A</name></objectClass>
      <objectClass>
        <name>B</name>
        <objectClass><name>A</name></objectClass>
      </objectClass>
    </objectClass>
  </objects>
  <interactions><interactionClass><name>HLAinteractionRoot</name></interactionClass></interactions>
</objectModel>`)

	res, err := Parse([]Module{{Path: "branches.xml", XML: xml}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Diagnostics) != 0 || res.FOM == nil {
		t.Fatalf("distinct qualified classes must be accepted: %+v", res)
	}
}

func TestSpec_MultipleParents_RejectsLegacyGraphTwoParents(t *testing.T) {
	t.Parallel()

	res := Result{Diagnostics: (multipleParentsValidator{}).Run(diagnosticInput{
		modulePath: "legacy-graph",
		fom: model.NewFOM([]model.ObjectClass{
			{Name: objectClassRootName},
			{Name: "B", ParentName: objectClassRootName},
			{Name: "A", ParentName: objectClassRootName},
			{Name: "A", ParentName: "B"},
		}, nil, nil),
	})}
	if res.CodeCounts()["FOM-003"] != 1 {
		t.Fatalf("want one FOM-003 for A with two parents, got %+v", res.Diagnostics)
	}
	if res.Diagnostics[0].ModulePath != "legacy-graph" {
		t.Errorf("lost module path: %+v", res.Diagnostics[0])
	}
}
