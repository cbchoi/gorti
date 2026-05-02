package parser

import (
	"strings"
	"testing"
)

// TestSpec_StrictMode_AcceptsAnnexAElements asserts the strict-mode walker
// accepts a minimal valid FOM (only Annex A names) without emitting FOM-009.
func TestSpec_StrictMode_AcceptsAnnexAElements(t *testing.T) {
	t.Parallel()

	xml := []byte(minimalFOM)
	res, err := Parse([]Module{{Path: "inline.xml", XML: xml}})
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	for _, d := range res.Diagnostics {
		if d.Code == "FOM-009" {
			t.Fatalf("unexpected FOM-009 on minimal good FOM: %+v", d)
		}
	}
}

// TestSpec_StrictMode_RejectsUnknownElement asserts an unknown element
// inside a known section produces FOM-009 with the offending element name
// in the message.
func TestSpec_StrictMode_RejectsUnknownElement(t *testing.T) {
	t.Parallel()

	bad := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification>
    <name>x</name>
    <type>FOM</type>
    <unknownExtensionElement>nope</unknownExtensionElement>
  </modelIdentification>
  <objects><objectClass><name>HLAobjectRoot</name></objectClass></objects>
  <interactions><interactionClass><name>HLAinteractionRoot</name></interactionClass></interactions>
</objectModel>`)

	res, err := Parse([]Module{{Path: "bad.xml", XML: bad}})
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if !res.HasCode("FOM-009") {
		t.Fatalf("want FOM-009, got %v", res.CodeCounts())
	}
	if res.FOM != nil {
		t.Errorf("want FOM nil on rejection")
	}
	var got Diagnostic
	for _, d := range res.Diagnostics {
		if d.Code == "FOM-009" {
			got = d
			break
		}
	}
	if !strings.Contains(got.Message, "unknownExtensionElement") {
		t.Errorf("FOM-009 message should name the offending element; got %q", got.Message)
	}
	if got.ModulePath != "bad.xml" {
		t.Errorf("FOM-009 ModulePath = %q; want bad.xml", got.ModulePath)
	}
	if got.Line == 0 {
		t.Errorf("FOM-009 Line should be non-zero; got %d", got.Line)
	}
}
