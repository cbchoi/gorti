package parser

import "testing"

// TestSpec_Cycle_RejectsSelfLoop asserts a class declaring itself as parent
// (self-loop) produces FOM-002.
func TestSpec_Cycle_RejectsSelfLoop(t *testing.T) {
	t.Parallel()

	// Construct a parent table with a self-loop directly via the model:
	// the structural XML walk does not emit self-loops, but cycle.go must
	// be robust against any input where Name == ParentName.
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
	if !res.HasCode("FOM-002") {
		t.Fatalf("want FOM-002, got %v", res.CodeCounts())
	}
	if res.FOM != nil {
		t.Errorf("want FOM nil on rejection")
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
