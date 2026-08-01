package parser

import (
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

const minimalFOM = `<?xml version="1.0" encoding="UTF-8"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification>
    <name>inline-test</name>
    <type>FOM</type>
  </modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>Vehicle</name>
        <sharing>PublishSubscribe</sharing>
        <attribute>
          <name>position</name>
          <dataType>HLAfloat64BE</dataType>
          <updateType>Periodic</updateType>
          <ownership>NoTransfer</ownership>
          <sharing>PublishSubscribe</sharing>
          <transportation>HLAreliable</transportation>
          <order>TimeStamp</order>
        </attribute>
      </objectClass>
    </objectClass>
  </objects>
  <interactions>
    <interactionClass>
      <name>HLAinteractionRoot</name>
      <interactionClass>
        <name>Honk</name>
        <sharing>PublishSubscribe</sharing>
        <transportation>HLAreliable</transportation>
        <order>TimeStamp</order>
      </interactionClass>
    </interactionClass>
  </interactions>
</objectModel>`

// TestParse_MinimalValidXML_ReturnsNonNilFOMNoDiagnostics is the parser-local
// happy-path test paralleling the spec test in tests/spec/M1.
func TestParse_MinimalValidXML_ReturnsNonNilFOMNoDiagnostics(t *testing.T) {
	t.Parallel()

	res, err := Parse([]Module{{Path: "inline.xml", XML: []byte(minimalFOM)}})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if res.FOM == nil {
		t.Fatal("Parse returned nil FOM for valid input")
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d: %+v", len(res.Diagnostics), res.Diagnostics)
	}
	if _, ok := res.FOM.(*model.FOM); !ok {
		t.Fatalf("Parse returned FOM of type %T; want *model.FOM", res.FOM)
	}
}

// TestParse_EmptyModulesSlice_ReturnsEmptyFOM asserts that calling Parse with
// no modules is well-defined: empty FOM, no error.
func TestParse_EmptyModulesSlice_ReturnsEmptyFOM(t *testing.T) {
	t.Parallel()

	res, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse(nil) returned error: %v", err)
	}
	if res.FOM == nil {
		t.Fatal("Parse(nil) returned nil FOM; want non-nil empty FOM")
	}
	fm, ok := res.FOM.(*model.FOM)
	if !ok {
		t.Fatalf("Parse(nil) FOM type %T; want *model.FOM", res.FOM)
	}
	if got := fm.ObjectClasses(); len(got) != 0 {
		t.Errorf("empty FOM: ObjectClasses len = %d; want 0", len(got))
	}
	if len(res.Diagnostics) != 0 {
		t.Errorf("empty FOM: expected 0 diagnostics, got %d", len(res.Diagnostics))
	}
}

// TestParse_MalformedXML_ReportsError asserts that an unclosed tag is
// surfaced — either as a Go error or as a diagnostic; either signals
// failure to the caller. The FOM must be nil on failure.
func TestParse_MalformedXML_ReportsError(t *testing.T) {
	t.Parallel()

	bad := []byte(`<?xml version="1.0"?><objectModel><objects><objectClass><name>X</name>`)

	res, err := Parse([]Module{{Path: "broken.xml", XML: bad}})
	signalled := err != nil || len(res.Diagnostics) > 0
	if !signalled {
		t.Fatal("Parse on malformed XML returned no error and no diagnostics")
	}
	if err == nil && res.FOM != nil {
		t.Errorf("Parse on malformed XML returned non-nil FOM with diagnostics; want FOM nil on rejection")
	}
}

// TestParse_ObjectClassHierarchy_FlattensWithParentName asserts that nested
// <objectClass> nodes in DIF XML are flattened into model.FOM as a
// name-sorted list, with each non-root carrying its parent's name in
// ParentName. Verifies the recursive walk, not just XML deserialization.
func TestParse_ObjectClassHierarchy_FlattensWithParentName(t *testing.T) {
	t.Parallel()

	res, err := Parse([]Module{{Path: "inline.xml", XML: []byte(minimalFOM)}})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	fm, ok := res.FOM.(*model.FOM)
	if !ok {
		t.Fatalf("FOM type %T; want *model.FOM", res.FOM)
	}

	classes := fm.ObjectClasses()
	if len(classes) != 2 {
		t.Fatalf("ObjectClasses len = %d; want 2 (HLAobjectRoot + Vehicle); got %+v", len(classes), classes)
	}
	// Sorted by name: HLAobjectRoot < Vehicle.
	if classes[0].Name != "HLAobjectRoot" || classes[1].Name != "Vehicle" {
		t.Fatalf("classes not sorted by name: %v", []string{classes[0].Name, classes[1].Name})
	}
	if classes[0].ParentName != "" {
		t.Errorf("HLAobjectRoot.ParentName = %q; want empty", classes[0].ParentName)
	}
	if classes[1].ParentName != "HLAobjectRoot" {
		t.Errorf("Vehicle.ParentName = %q; want HLAobjectRoot", classes[1].ParentName)
	}
	// Vehicle's attribute must be carried over.
	if len(classes[1].Attributes) != 1 || classes[1].Attributes[0].Name != "position" {
		t.Errorf("Vehicle attributes = %+v; want one attr 'position'", classes[1].Attributes)
	}
}

// TestParse_InteractionClassHierarchy_FlattensWithParentName mirrors the
// object class flattening test for interaction classes.
func TestParse_InteractionClassHierarchy_FlattensWithParentName(t *testing.T) {
	t.Parallel()

	res, err := Parse([]Module{{Path: "inline.xml", XML: []byte(minimalFOM)}})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	fm, ok := res.FOM.(*model.FOM)
	if !ok {
		t.Fatalf("FOM type %T; want *model.FOM", res.FOM)
	}

	ics := fm.InteractionClasses()
	if len(ics) != 2 {
		t.Fatalf("InteractionClasses len = %d; want 2; got %+v", len(ics), ics)
	}
	// Sorted by name: HLAinteractionRoot < Honk.
	if ics[0].Name != "HLAinteractionRoot" || ics[1].Name != "Honk" {
		t.Fatalf("interactions not sorted by name: %v", []string{ics[0].Name, ics[1].Name})
	}
	if ics[0].ParentName != "" {
		t.Errorf("HLAinteractionRoot.ParentName = %q; want empty", ics[0].ParentName)
	}
	if ics[1].ParentName != "HLAinteractionRoot" {
		t.Errorf("Honk.ParentName = %q; want HLAinteractionRoot", ics[1].ParentName)
	}
}

// TestParse_MultipleModules_AggregatesObjectClasses confirms that Parse
// processes every module and merges the resulting model. (Cut-1 merge: union
// of definitions; conflict detection is later tasks.)
func TestParse_MultipleModules_AggregatesObjectClasses(t *testing.T) {
	t.Parallel()

	moduleA := strings.Replace(minimalFOM, "Vehicle", "Alpha", 1)
	moduleB := strings.Replace(minimalFOM, "Vehicle", "Bravo", 1)

	res, err := Parse([]Module{
		{Path: "a.xml", XML: []byte(moduleA)},
		{Path: "b.xml", XML: []byte(moduleB)},
	})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	fm, ok := res.FOM.(*model.FOM)
	if !ok {
		t.Fatalf("FOM type %T; want *model.FOM", res.FOM)
	}
	names := make([]string, 0, len(fm.ObjectClasses()))
	for _, c := range fm.ObjectClasses() {
		names = append(names, c.Name)
	}
	// Both modules contribute; HLAobjectRoot is shared; both Alpha and Bravo
	// should appear (deduplication is later-task scope).
	wantAlpha, wantBravo := false, false
	for _, n := range names {
		if n == "Alpha" {
			wantAlpha = true
		}
		if n == "Bravo" {
			wantBravo = true
		}
	}
	if !wantAlpha || !wantBravo {
		t.Fatalf("expected Alpha and Bravo in object classes; got %v", names)
	}
}
