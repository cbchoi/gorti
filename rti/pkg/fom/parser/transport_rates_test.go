package parser_test

import (
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

func TestTransportRateConflictingDeclarations(t *testing.T) {
	for _, tc := range []struct{ name, fragment, before, after, code string }{
		{"reliability", `<transportations><transportation><name>HLAreliable</name><reliable>Yes</reliable></transportation></transportations>`, testReliableYes, "No", "FOM-016"},
		{"maximum_rate", `<updateRates><updateRate><name>Fast</name><rate>1</rate><semantics>one</semantics></updateRate></updateRates>`, "<rate>1</rate>", "<rate>2</rate>", "FOM-016"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := "<objectModel>" + tc.fragment + "</objectModel>"
			b := strings.Replace(a, tc.before, tc.after, 1)
			for _, parse := range []func([]parser.Module) (parser.Result, error){parser.Parse, parser.ParseAdditive} {
				for _, inputs := range [][2]string{{a, b}, {b, a}} {
					result, err := parse([]parser.Module{{Path: "a.xml", XML: []byte(inputs[0])}, {Path: "b.xml", XML: []byte(inputs[1])}})
					if err != nil {
						t.Fatal(err)
					}
					if result.FOM != nil || !result.HasCode(tc.code) {
						t.Errorf("conflicting declarations accepted: %+v", result)
					}
				}
			}
		})
	}
}

func TestTransportRateAbsentDescriptionsMerge(t *testing.T) {
	for _, semantics := range []string{"", "  description  "} {
		for _, parse := range []func([]parser.Module) (parser.Result, error){parser.Parse, parser.ParseAdditive} {
			absent := `<objectModel><transportations><transportation><name>HLAreliable</name><reliable>Yes</reliable></transportation></transportations><updateRates><updateRate><name>Fast</name><rate>1</rate></updateRate></updateRates></objectModel>`
			present := strings.ReplaceAll(absent, "</reliable>", "</reliable><semantics>"+semantics+"</semantics>")
			present = strings.ReplaceAll(present, "</rate>", "</rate><semantics>"+semantics+"</semantics>")
			for _, inputs := range [][2]string{{absent, present}, {present, absent}, {present, present}} {
				result, err := parse([]parser.Module{{XML: []byte(inputs[0])}, {XML: []byte(inputs[1])}})
				if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
					t.Fatalf("merge: %+v %v", result, err)
				}
				f := result.FOM.(*model.FOM)
				want := model.OptionalString{Present: true, Value: semantics}
				if len(f.Transportations()) != 1 || f.Transportations()[0].Semantics != want || len(f.UpdateRates()) != 1 || f.UpdateRates()[0].SemanticsDeclaration() != want {
					t.Fatalf("presence lost: %+v %+v", f.Transportations(), f.UpdateRates())
				}
			}
		}
	}
}

func TestTransportDeclarationsInvalidAndLegacyReferences(t *testing.T) {
	for _, body := range []string{
		`<name>HLAreliable</name>`, `<name>HLAreliable</name><reliable/>`,
		`<name>HLAreliable</name><reliable>true</reliable>`, `<reliable>Yes</reliable>`,
	} {
		xml := `<objectModel><transportations><transportation>` + body + `</transportation></transportations></objectModel>`
		result, err := parser.Parse([]parser.Module{{Path: "invalid.xml", XML: []byte(xml)}})
		if err != nil {
			t.Fatal(err)
		}
		if result.FOM != nil || !result.HasCode("FOM-016") || result.Diagnostics[0].ModulePath != "invalid.xml" {
			t.Fatalf("invalid declaration accepted: %+v", result)
		}
	}
	for _, xml := range []string{
		`<objectModel><interactions><interactionClass><name>HLAinteractionRoot</name><transportation>HLAreliable</transportation><order>Receive</order></interactionClass></interactions></objectModel>`,
		`<objectModel><transportations><transportation><name>Custom</name><reliable>No</reliable><semantics/></transportation></transportations></objectModel>`,
	} {
		result, err := parser.Parse([]parser.Module{{XML: []byte(xml)}})
		if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
			t.Fatalf("legacy/reference or metadata-only input rejected: %+v %v", result, err)
		}
	}
}
