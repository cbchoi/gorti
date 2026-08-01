package parser

import "testing"

// TestSpec_ObjectParent_AcceptsRootedHierarchy asserts a tree rooted at
// HLAobjectRoot does not produce FOM-011.
func TestSpec_ObjectParent_AcceptsRootedHierarchy(t *testing.T) {
	t.Parallel()

	res, err := Parse([]Module{{Path: "x.xml", XML: []byte(minimalFOM)}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, d := range res.Diagnostics {
		if d.Code == "FOM-011" {
			t.Fatalf("unexpected FOM-011 on rooted FOM: %+v", d)
		}
	}
}

// TestSpec_ObjectParent_RejectsTopLevelOrphan asserts a class declared
// directly under <objects> (not nested under HLAobjectRoot) produces
// FOM-011.
func TestSpec_ObjectParent_RejectsTopLevelOrphan(t *testing.T) {
	t.Parallel()

	bad := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>x</name><type>FOM</type></modelIdentification>
  <objects>
    <objectClass>
      <name>OrphanClass</name>
    </objectClass>
  </objects>
  <interactions><interactionClass><name>HLAinteractionRoot</name></interactionClass></interactions>
</objectModel>`)

	res, err := Parse([]Module{{Path: "bad.xml", XML: bad}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.HasCode("FOM-011") {
		t.Fatalf("want FOM-011, got %v", res.CodeCounts())
	}
}
