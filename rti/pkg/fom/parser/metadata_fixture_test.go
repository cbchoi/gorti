package parser

import (
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"os"
	"testing"
)

func TestOfficialFDDRatesSwitchesFixtureRetained(t *testing.T) {
	xml, err := os.ReadFile("testdata/fdd2010-rates-switches.xml")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Parse([]Module{{Path: "fdd2010-rates-switches.xml", XML: xml}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("parse: %v %v", err, result.Diagnostics)
	}
	f := result.FOM.(*model.FOM)
	if len(f.UpdateRates()) != 4 || len(f.AdvisorySwitches()) != 10 {
		t.Fatalf("metadata lost: %+v %+v", f.UpdateRates(), f.AdvisorySwitches())
	}
}

func TestAdvisorySwitchXMLCommentsAreNotContent(t *testing.T) {
	result, err := Parse([]Module{{Path: "comments.xml", XML: []byte(`<objectModel><switches><attributeScopeAdvisory isEnabled="true"><!-- explanation --></attributeScopeAdvisory></switches></objectModel>`)}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("XML comments must remain allowed: %v %v", err, result.Diagnostics)
	}
}
