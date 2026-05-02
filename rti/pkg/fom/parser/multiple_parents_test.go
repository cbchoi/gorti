package parser

import "testing"

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

// TestSpec_MultipleParents_RejectsTwoParents asserts a class name appearing
// under two distinct parents (sibling + nested) produces FOM-003.
func TestSpec_MultipleParents_RejectsTwoParents(t *testing.T) {
	t.Parallel()

	bad := []byte(`<?xml version="1.0"?>
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

	res, err := Parse([]Module{{Path: "bad.xml", XML: bad}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.HasCode("FOM-003") {
		t.Fatalf("want FOM-003, got %v", res.CodeCounts())
	}
}
