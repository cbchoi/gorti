package parser

import (
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// The old RejectsSelfLoop assertion conflated HLAobjectRoot.A with
// HLAobjectRoot.A.B.A. These are distinct identities, not a graph cycle;
// explicit legacy graph rejection is covered below instead.
func TestSpec_Cycle_AcceptsRepeatedAncestorLeaf(t *testing.T) {
	t.Parallel()

	xml := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>x</name><type>FOM</type></modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>A</name>
        <objectClass>
          <name>B</name>
          <objectClass>
            <name>A</name>
          </objectClass>
        </objectClass>
      </objectClass>
    </objectClass>
  </objects>
  <interactions><interactionClass><name>HLAinteractionRoot</name></interactionClass></interactions>
</objectModel>`)

	res, err := Parse([]Module{{Path: "x.xml", XML: xml}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Diagnostics) != 0 || res.FOM == nil {
		t.Fatalf("distinct qualified classes must be accepted: %+v", res)
	}
}

func TestSpec_Cycle_RejectsLegacyGraphCycles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		classes []model.ObjectClass
	}{
		{"self_loop", []model.ObjectClass{{Name: "A", ParentName: "A"}}},
		{"two_nodes", []model.ObjectClass{{Name: "A", ParentName: "B"}, {Name: "B", ParentName: "A"}}},
		{"long_cycle", []model.ObjectClass{{Name: "A", ParentName: "B"}, {Name: "B", ParentName: "C"}, {Name: "C", ParentName: "A"}}},
		{"redeclaration_back_edge", []model.ObjectClass{
			{Name: objectClassRootName},
			{Name: "A", ParentName: objectClassRootName},
			{Name: "B", ParentName: "A"},
			{Name: "A", ParentName: "B"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := Result{Diagnostics: (cycleValidator{}).Run(diagnosticInput{
				modulePath: "legacy-graph",
				fom:        model.NewFOM(tc.classes, nil, nil),
			})}
			if !res.HasCode("FOM-002") {
				t.Fatalf("want FOM-002 for explicit graph cycle, got %+v", res.Diagnostics)
			}
			for _, d := range res.Diagnostics {
				if d.ModulePath != "legacy-graph" {
					t.Errorf("lost module path: %+v", d)
				}
			}
		})
	}
}

// TestSpec_Cycle_AcceptsLinearChain asserts a deep linear ancestor chain
// (no cycle) does not produce FOM-002.
func TestSpec_Cycle_AcceptsLinearChain(t *testing.T) {
	t.Parallel()

	xml := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>x</name><type>FOM</type></modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>A</name>
        <objectClass>
          <name>B</name>
          <objectClass>
            <name>C</name>
          </objectClass>
        </objectClass>
      </objectClass>
    </objectClass>
  </objects>
  <interactions><interactionClass><name>HLAinteractionRoot</name></interactionClass></interactions>
</objectModel>`)

	res, err := Parse([]Module{{Path: "x.xml", XML: xml}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, d := range res.Diagnostics {
		if d.Code == "FOM-002" {
			t.Fatalf("unexpected FOM-002 on linear chain: %+v", d)
		}
	}
}
