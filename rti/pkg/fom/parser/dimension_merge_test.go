package parser

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

func TestDimensionDeclarationsMerge(t *testing.T) {
	module := func(path, bound, normalization string) Module {
		return Module{Path: path, XML: []byte(`<objectModel><dimensions><dimension><name>X</name><upperBound>` + bound + `</upperBound><normalization>` + normalization + `</normalization></dimension></dimensions></objectModel>`)}
	}
	first := module("first.xml", "100", "meters")
	for _, tc := range []struct {
		name, bound, normalization string
		conflict                   bool
	}{
		{"identical", "100", "meters", false},
		{"equivalent_bound_lexical", "+00100", "meters", false},
		{"normalization_whitespace_conflict", "100", " meters ", true},
		{"upper_bound_conflict", "101", "meters", true},
		{"normalization_conflict", "100", "feet", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			second := module("second.xml", tc.bound, tc.normalization)
			for _, modules := range [][]Module{{first, second}, {second, first}} {
				res, err := Parse(modules)
				if err != nil {
					t.Fatal(err)
				}
				if tc.conflict {
					if res.FOM != nil || !res.HasCode("FOM-015") {
						t.Fatalf("conflicting dimensions accepted: %+v", res)
					}
					d := res.Diagnostics[0]
					if d.ModulePath != modules[1].Path || !strings.Contains(d.Message, `"X"`) {
						t.Fatalf("missing conflict context: %+v", d)
					}
					continue
				}
				if res.FOM == nil || len(res.Diagnostics) != 0 {
					t.Fatalf("compatible definitions rejected: %+v", res.Diagnostics)
				}
				want := []model.Dimension{{Name: "X", UpperBound: 100, NormalizationKey: "meters"}}
				if got := res.FOM.(*model.FOM).Dimensions(); !reflect.DeepEqual(got, want) {
					t.Fatalf("dimension declarations not deduplicated: got %+v, want %+v", got, want)
				}
			}
		})
	}
}
