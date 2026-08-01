package parser

import "testing"

// TestSpec_DuplicateAttribute_RejectsRepeatInSameClass asserts that two
// <attribute> entries with the same <name> in one class produce FOM-004.
func TestSpec_DuplicateAttribute_RejectsRepeatInSameClass(t *testing.T) {
	t.Parallel()

	bad := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>x</name><type>FOM</type></modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>X</name>
        <attribute><name>a</name><dataType>HLAfloat64BE</dataType></attribute>
        <attribute><name>a</name><dataType>HLAfloat32BE</dataType></attribute>
      </objectClass>
    </objectClass>
  </objects>
  <interactions><interactionClass><name>HLAinteractionRoot</name></interactionClass></interactions>
</objectModel>`)

	res, err := Parse([]Module{{Path: "bad.xml", XML: bad}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.HasCode("FOM-004") {
		t.Fatalf("want FOM-004, got %v", res.CodeCounts())
	}
}

// TestSpec_DuplicateAttribute_RejectsInheritedClash asserts a child
// re-declaring an ancestor's attribute produces FOM-004.
func TestSpec_DuplicateAttribute_RejectsInheritedClash(t *testing.T) {
	t.Parallel()

	bad := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>x</name><type>FOM</type></modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>Parent</name>
        <attribute><name>shared</name><dataType>HLAfloat64BE</dataType></attribute>
        <objectClass>
          <name>Child</name>
          <attribute><name>shared</name><dataType>HLAfloat32BE</dataType></attribute>
        </objectClass>
      </objectClass>
    </objectClass>
  </objects>
  <interactions><interactionClass><name>HLAinteractionRoot</name></interactionClass></interactions>
</objectModel>`)

	res, err := Parse([]Module{{Path: "bad.xml", XML: bad}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.HasCode("FOM-004") {
		t.Fatalf("want FOM-004, got %v", res.CodeCounts())
	}
}

// TestSpec_DuplicateAttribute_AcceptsDistinctNames asserts unique attribute
// names across the inheritance chain do not produce FOM-004.
func TestSpec_DuplicateAttribute_AcceptsDistinctNames(t *testing.T) {
	t.Parallel()

	res, err := Parse([]Module{{Path: "x.xml", XML: []byte(minimalFOM)}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, d := range res.Diagnostics {
		if d.Code == "FOM-004" {
			t.Fatalf("unexpected FOM-004 on minimal FOM: %+v", d)
		}
	}
}
