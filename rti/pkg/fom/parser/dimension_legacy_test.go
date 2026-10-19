package parser_test

import (
	"os"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

func TestDimensionLegacyNAProductionRegistrationFixture(t *testing.T) {
	source, err := os.ReadFile("../../../tests/integration/ddm_registration/testdata/registration.xml")
	if err != nil {
		t.Fatal(err)
	}
	for _, parse := range []func([]parser.Module) (parser.Result, error){parser.Parse, parser.ParseAdditive} {
		result, err := parse([]parser.Module{{Path: "registration.xml", XML: source}})
		if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
			t.Fatalf("previously accepted production fixture rejected: %v %+v", err, result.Diagnostics)
		}
		want := []model.Dimension{
			{Name: "X", DataType: "HLAinteger64BE", UpperBound: 1000, NormalizationKey: "Linear", DefaultValue: "NA"},
			{Name: "Y", DataType: "HLAinteger64BE", UpperBound: 1000, NormalizationKey: "Linear", DefaultValue: "NA"},
		}
		if got := result.FOM.(*model.FOM).Dimensions(); !reflect.DeepEqual(got, want) {
			t.Fatalf("legacy dimension metadata was dropped or invented: got %+v, want %+v", got, want)
		}
	}
}

func TestDimensionLegacyNAConflictsWithStandardDefault(t *testing.T) {
	module := func(path, value string) parser.Module {
		return parser.Module{Path: path, XML: []byte(`<objectModel><dimensions><dimension><name>X</name><upperBound>1000</upperBound><value>` + value + `</value></dimension></dimensions></objectModel>`)}
	}
	for _, parse := range []func([]parser.Module) (parser.Result, error){parser.Parse, parser.ParseAdditive} {
		for _, values := range [][2]string{{"NA", "Excluded"}, {"Excluded", "NA"}} {
			result, err := parse([]parser.Module{module("first.xml", values[0]), module("second.xml", values[1])})
			if err != nil {
				t.Fatal(err)
			}
			if result.FOM != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "FOM-015" || result.Diagnostics[0].ModulePath != "second.xml" {
				t.Fatalf("legacy value must be retained for merge conflict detection: %+v", result)
			}
		}
		result, err := parse([]parser.Module{module("first.xml", "NA"), module("same.xml", "NA")})
		if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
			t.Fatalf("identical legacy declarations rejected: %v %+v", err, result.Diagnostics)
		}
	}
}
