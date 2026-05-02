package parser

import "testing"

// TestSpec_InteractionParent_AcceptsRootedHierarchy asserts a tree rooted
// at HLAinteractionRoot does not produce FOM-012.
func TestSpec_InteractionParent_AcceptsRootedHierarchy(t *testing.T) {
	t.Parallel()

	res, err := Parse([]Module{{Path: "x.xml", XML: []byte(minimalFOM)}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, d := range res.Diagnostics {
		if d.Code == "FOM-012" {
			t.Fatalf("unexpected FOM-012 on rooted FOM: %+v", d)
		}
	}
}

// TestSpec_InteractionParent_RejectsTopLevelOrphan asserts an interaction
// declared directly under <interactions> (not under HLAinteractionRoot)
// produces FOM-012.
func TestSpec_InteractionParent_RejectsTopLevelOrphan(t *testing.T) {
	t.Parallel()

	bad := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>x</name><type>FOM</type></modelIdentification>
  <objects><objectClass><name>HLAobjectRoot</name></objectClass></objects>
  <interactions>
    <interactionClass>
      <name>OrphanInteraction</name>
    </interactionClass>
  </interactions>
</objectModel>`)

	res, err := Parse([]Module{{Path: "bad.xml", XML: bad}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.HasCode("FOM-012") {
		t.Fatalf("want FOM-012, got %v", res.CodeCounts())
	}
}
