package parser

import "testing"

// TestSpec_DuplicateParameter_RejectsRepeatInSameInteraction asserts two
// <parameter> entries with the same <name> in one interaction produce
// FOM-005.
func TestSpec_DuplicateParameter_RejectsRepeatInSameInteraction(t *testing.T) {
	t.Parallel()

	bad := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>x</name><type>FOM</type></modelIdentification>
  <objects><objectClass><name>HLAobjectRoot</name></objectClass></objects>
  <interactions>
    <interactionClass>
      <name>HLAinteractionRoot</name>
      <interactionClass>
        <name>I</name>
        <parameter><name>seq</name><dataType>HLAinteger32BE</dataType></parameter>
        <parameter><name>seq</name><dataType>HLAinteger64BE</dataType></parameter>
      </interactionClass>
    </interactionClass>
  </interactions>
</objectModel>`)

	res, err := Parse([]Module{{Path: "bad.xml", XML: bad}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.HasCode("FOM-005") {
		t.Fatalf("want FOM-005, got %v", res.CodeCounts())
	}
}

// TestSpec_DuplicateParameter_RejectsInheritedClash asserts a child
// re-declaring an ancestor's parameter produces FOM-005.
func TestSpec_DuplicateParameter_RejectsInheritedClash(t *testing.T) {
	t.Parallel()

	bad := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>x</name><type>FOM</type></modelIdentification>
  <objects><objectClass><name>HLAobjectRoot</name></objectClass></objects>
  <interactions>
    <interactionClass>
      <name>HLAinteractionRoot</name>
      <interactionClass>
        <name>P</name>
        <parameter><name>shared</name><dataType>HLAinteger32BE</dataType></parameter>
        <interactionClass>
          <name>C</name>
          <parameter><name>shared</name><dataType>HLAinteger64BE</dataType></parameter>
        </interactionClass>
      </interactionClass>
    </interactionClass>
  </interactions>
</objectModel>`)

	res, err := Parse([]Module{{Path: "bad.xml", XML: bad}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.HasCode("FOM-005") {
		t.Fatalf("want FOM-005, got %v", res.CodeCounts())
	}
}

// TestSpec_DuplicateParameter_AcceptsDistinctNames asserts unique parameter
// names do not produce FOM-005.
func TestSpec_DuplicateParameter_AcceptsDistinctNames(t *testing.T) {
	t.Parallel()

	res, err := Parse([]Module{{Path: "x.xml", XML: []byte(minimalFOM)}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, d := range res.Diagnostics {
		if d.Code == "FOM-005" {
			t.Fatalf("unexpected FOM-005 on minimal FOM: %+v", d)
		}
	}
}
