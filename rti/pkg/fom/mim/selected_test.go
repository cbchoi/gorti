package mim

import (
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

func TestSelectedMIMSupportedProfileAndRejections(t *testing.T) {
	base := string(StandardMIMBytes())
	selected := strings.Replace(base, "This object class is the root class of all MOM object classes", "  selected descriptive text  ", 1)
	fom, err := ParseSelected([]byte(selected))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cls := range fom.ObjectClasses() {
		if cls.Name == "HLAmanager" {
			found = cls.Semantics.Present && cls.Semantics.Value == "  selected descriptive text  "
		}
	}
	if !found {
		t.Fatal("selected descriptive text silently replaced")
	}
	if len(fom.Transportations()) != 2 {
		t.Fatal("MIM transportation declarations lost")
	}
	for name, source := range map[string]string{
		"empty": "", "malformed": "<objectModel>", "missing": "<objectModel/>",
		"trailing-root": base + "<objectModel/>", "trailing-text": base + "junk",
		"wrong-namespace":         strings.ReplaceAll(base, "http://standards.ieee.org/IEEE1516-2010", "urn:wrong"),
		"missing-service":         strings.Replace(base, "<name>HLAfederateHandle</name>", "<name>missingHandle</name>", 1),
		"wrong-encoding":          strings.Replace(base, "<dataType>HLAtoken</dataType>", "<dataType>HLAinteger32BE</dataType>", 1),
		"unknown-element":         strings.Replace(base, "</objects>", "<unknown/></objects>", 1),
		"duplicate-name":          strings.Replace(base, "<name>HLAobjectRoot</name>", "<name>HLAobjectRoot</name><name>HLAobjectRoot</name>", 1),
		"wrong-reliability":       strings.Replace(base, "<reliable>Yes</reliable>", "<reliable>No</reliable>", 1),
		"foreign-xmlns-attribute": strings.Replace(base, "<objectModel", `<objectModel xmlns:probe="urn:probe" probe:xmlns="extra"`, 1),
		"service-name-whitespace": strings.Replace(base, "<name>HLAobjectRoot</name>", "<name>HLAobjectRoot </name>", 1),
		"DTD":                     "<!DOCTYPE objectModel []>" + base,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseSelected([]byte(source)); err == nil {
				t.Fatal("unsupported MIM accepted")
			}
		})
	}
}

func TestMIMDecodeMergeRetainsTransportationAndRatePresence(t *testing.T) {
	base, err := decodeMIM([]byte(`<objectModel><transportations><transportation><name>HLAreliable</name><reliable>Yes</reliable><semantics></semantics></transportation></transportations><updateRates><updateRate><name>rate</name><rate>1.000000000000000000000001</rate><semantics></semantics></updateRate></updateRates></objectModel>`))
	if err != nil {
		t.Fatal(err)
	}
	merged, diags := Merge(base, model.NewFOM(nil, nil, nil))
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	if got := merged.Transportations(); len(got) != 1 || !got[0].Semantics.Present || got[0].Semantics.Value != "" {
		t.Fatalf("transportation metadata: %+v", got)
	}
	rates := merged.UpdateRates()
	if len(rates) != 1 || !rates[0].SemanticsDeclaration().Present {
		t.Fatal("empty rate semantics presence lost")
	}
	if decimal, err := rates[0].DecimalString(); err != nil || decimal != "1.000000000000000000000001" {
		t.Fatalf("rate decimal %s: %v", decimal, err)
	}
	descriptionOnly := model.NewFOM(nil, nil, nil).WithTransportations([]model.Transportation{{Name: "HLAreliable", Reliable: "Yes", Semantics: model.OptionalString{Present: true, Value: "different"}}})
	if result, diags := Merge(base, descriptionOnly); result == nil || len(diags) != 0 || result.Transportations()[0].Semantics != base.Transportations()[0].Semantics {
		t.Fatal("non-FDD description rejected or replaced the first supplied description")
	}
}
