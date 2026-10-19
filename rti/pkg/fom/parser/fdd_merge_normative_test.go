package parser_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

// IEEE 1516.1-2010 4.1.4/4.1.4.1 limit RTI combination to FDD:
// transportation descriptions and update-rate descriptions are not FDD.
func TestFDDTransportationDescriptionsDoNotRejectMerge(t *testing.T) {
	testFDDDescriptions(t, "transportation", func(description string) string {
		return `<transportations><transportation><name>HLAreliable</name><reliable>Yes</reliable><semantics>` + description + `</semantics></transportation></transportations>`
	}, func(f *model.FOM) model.OptionalString {
		values := f.Transportations()
		if len(values) != 1 || values[0].Name != "HLAreliable" || values[0].Reliable != testReliableYes {
			t.Fatalf("transportation FDD changed: %+v", values)
		}
		return values[0].Semantics
	})
}

func TestFDDUpdateRateDescriptionsDoNotRejectMerge(t *testing.T) {
	testFDDDescriptions(t, "updateRate", func(description string) string {
		return `<updateRates><updateRate><name>Fast</name><rate>1.000000000000000000000001</rate><semantics>` + description + `</semantics></updateRate></updateRates>`
	}, func(f *model.FOM) model.OptionalString {
		values := f.UpdateRates()
		if len(values) != 1 || values[0].Name != "Fast" {
			t.Fatalf("update-rate FDD changed: %+v", values)
		}
		decimal, err := values[0].DecimalString()
		if err != nil || decimal != "1.000000000000000000000001" {
			t.Fatalf("exact rate lost: %q, %v", decimal, err)
		}
		return values[0].SemanticsDeclaration()
	})
}

func testFDDDescriptions(t *testing.T, kind string, fragment func(string) string, description func(*model.FOM) model.OptionalString) {
	t.Helper()
	for name, parse := range map[string]func([]parser.Module) (parser.Result, error){"parse": parser.Parse, "additive": parser.ParseAdditive} {
		for i, descriptions := range [][2]string{{"first", "second"}, {"", "second"}, {" first ", "first"}, {"second", ""}} {
			t.Run(fmt.Sprintf("%s/%s/%d", name, kind, i), func(t *testing.T) {
				modules := []parser.Module{
					{Path: "first.xml", XML: []byte(`<objectModel>` + fragment(descriptions[0]) + `</objectModel>`)},
					{Path: "second.xml", XML: []byte(`<objectModel>` + fragment(descriptions[1]) + `</objectModel>`)},
				}
				result, err := parse(modules)
				if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
					t.Fatalf("identical FDD rejected for non-FDD description: %+v, %v", result, err)
				}
				want := model.OptionalString{Present: true, Value: descriptions[0]}
				if got := description(result.FOM.(*model.FOM)); got != want {
					t.Fatalf("first supplied description changed: got %+v, want %+v", got, want)
				}
			})
		}
	}
}

func TestFDDTransportRateMIMMergeUsesOnlyOperationalFields(t *testing.T) {
	standard, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatal(err)
	}
	rate, err := model.NewUpdateRate("Fast", "1.000000000000000000000001", "base rate")
	if err != nil {
		t.Fatal(err)
	}
	base := model.NewFOMWithUpdateRates(standard.ObjectClasses(), standard.InteractionClasses(), standard.DataTypes(), standard.Dimensions(), []model.UpdateRate{rate}).WithTransportations(standard.Transportations())
	changedRate := rate
	changedRate.Semantics = "different rate description"
	transport := base.Transportations()[0]
	transport.Semantics = model.OptionalString{Present: true, Value: "different transport description"}
	user := model.NewFOMWithUpdateRates(nil, nil, nil, nil, []model.UpdateRate{changedRate}).WithTransportations([]model.Transportation{transport})
	merged, diags := mim.Merge(base, user)
	if merged == nil || len(diags) != 0 {
		t.Fatalf("MIM merge rejected only descriptive differences: %+v", diags)
	}
	if !reflect.DeepEqual(merged.Transportations(), base.Transportations()) || !reflect.DeepEqual(merged.UpdateRates(), base.UpdateRates()) {
		t.Fatal("MIM declarations replaced by descriptive differences")
	}
	if got := user.UpdateRates()[0]; got.Semantics != changedRate.Semantics || user.Transportations()[0] != transport {
		t.Fatal("merge mutated source declarations")
	}
	for _, kind := range []string{"reliability", "rate"} {
		t.Run(kind, func(t *testing.T) {
			var conflict *model.FOM
			if kind == "reliability" {
				bad := transport
				if bad.Reliable == testReliableYes {
					bad.Reliable = "No"
				} else {
					bad.Reliable = testReliableYes
				}
				conflict = user.WithTransportations([]model.Transportation{bad})
			} else {
				bad, err := model.NewUpdateRate("Fast", "1.000000000000000000000002", changedRate.Semantics)
				if err != nil {
					t.Fatal(err)
				}
				conflict = model.NewFOMWithUpdateRates(nil, nil, nil, nil, []model.UpdateRate{bad})
			}
			if got, diags := mim.Merge(base, conflict); got != nil || len(diags) == 0 {
				t.Fatalf("operational conflict accepted: %+v, %+v", got, diags)
			}
		})
	}
}
