package parser_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

func TestDescriptiveMetadataMergeConflicts(t *testing.T) {
	for _, tc := range []struct{ name, fragment, before, after, code string }{
		{"dimension_datatype", `<dimensions><dimension><name>X</name><dataType>HLAinteger32BE</dataType></dimension></dimensions>`, "HLAinteger32BE", "HLAinteger64BE", "FOM-015"},
		{"dimension_default", `<dimensions><dimension><name>X</name><value>Excluded</value></dimension></dimensions>`, "Excluded", "[0..8)", "FOM-015"},
		{"normalization_whitespace", `<dimensions><dimension><name>X</name><normalization> map </normalization></dimension></dimensions>`, " map ", "map", "FOM-015"},
		{testObjectKind, `<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Entity</name><semantics>first</semantics></objectClass></objectClass></objects>`, "first", "second", ""},
		{testInteractionKind, `<interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Event</name><semantics>first</semantics></interactionClass></interactionClass></interactions>`, "first", "second", ""},
		{"attribute", `<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Entity</name><attribute><name>A</name><semantics>first</semantics></attribute></objectClass></objectClass></objects>`, "first", "second", ""},
		{testUpdateCondition, `<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Entity</name><attribute><name>A</name><updateCondition>first</updateCondition></attribute></objectClass></objectClass></objects>`, "first", "second", ""},
		{"parameter", `<interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Event</name><parameter><name>P</name><semantics>first</semantics></parameter></interactionClass></interactionClass></interactions>`, "first", "second", ""},
		{"explicit_empty", `<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Entity</name><semantics>first</semantics></objectClass></objectClass></objects>`, "first", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := "<objectModel>" + tc.fragment + "</objectModel>"
			b := strings.Replace(a, tc.before, tc.after, 1)
			for _, parse := range []func([]parser.Module) (parser.Result, error){parser.Parse, parser.ParseAdditive} {
				result, err := parse([]parser.Module{{Path: "a.xml", XML: []byte(a)}, {Path: "b.xml", XML: []byte(b)}})
				if err != nil {
					t.Fatal(err)
				}
				if tc.code == "" {
					baseline, err := parse([]parser.Module{{XML: []byte(a)}})
					if err != nil || baseline.FOM == nil || !reflect.DeepEqual(result, baseline) {
						t.Errorf("description-only merge changed the first declaration: %v %+v", err, result)
					}
				} else if result.FOM != nil || !result.HasCode(tc.code) {
					t.Errorf("conflicting metadata accepted: %+v", result)
				}
				same, err := parse([]parser.Module{{Path: "a.xml", XML: []byte(a)}, {Path: "same.xml", XML: []byte(a)}})
				if err != nil || same.FOM == nil || len(same.Diagnostics) != 0 {
					t.Fatalf("identical metadata rejected: %v %+v", err, same.Diagnostics)
				}
			}
		})
	}
}

func TestDescriptiveMetadataAbsentAndEmpty(t *testing.T) {
	for _, content := range []string{"", "specified"} {
		for _, parse := range []func([]parser.Module) (parser.Result, error){parser.Parse, parser.ParseAdditive} {
			absent := `<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Entity</name></objectClass></objectClass></objects><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Event</name></interactionClass></interactionClass></interactions></objectModel>`
			present := strings.ReplaceAll(absent, "</name></objectClass>", "</name><semantics>"+content+"</semantics></objectClass>")
			present = strings.ReplaceAll(present, "</name></interactionClass>", "</name><semantics>"+content+"</semantics></interactionClass>")
			for _, inputs := range [][2]string{{absent, present}, {present, absent}} {
				result, err := parse([]parser.Module{{XML: []byte(inputs[0])}, {XML: []byte(inputs[1])}})
				if err != nil || len(result.Diagnostics) != 0 {
					t.Fatalf("parse: %v %+v", err, result.Diagnostics)
				}
				f := result.FOM.(*model.FOM)
				for _, c := range f.ObjectClasses() {
					want := model.OptionalString{}
					if c.Name == "Entity" {
						want = model.OptionalString{Present: true, Value: content}
					}
					if c.Semantics != want {
						t.Fatalf("object %s semantics=%+v want %+v", c.Name, c.Semantics, want)
					}
				}
				for _, c := range f.InteractionClasses() {
					want := model.OptionalString{}
					if c.Name == "Event" {
						want = model.OptionalString{Present: true, Value: content}
					}
					if c.Semantics != want {
						t.Fatalf("interaction %s semantics=%+v want %+v", c.Name, c.Semantics, want)
					}
				}
			}
		}
	}
}

func TestDescriptiveMetadataInvalidDimension(t *testing.T) {
	for _, tc := range []struct{ name, metadata, code string }{
		{"empty_datatype", "<dataType/>", "FOM-015"},
		{"unknown_datatype", "<dataType>Unknown</dataType>", "FOM-001"},
		{"empty_normalization", "<normalization/>", "FOM-015"},
		{"empty_default", "<value/>", "FOM-015"},
		{"invented_default", "<value>Included</value>", "FOM-015"},
		{"closed_range", "<value>[1..2]</value>", "FOM-015"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			xml := `<objectModel><dimensions><dimension><name>X</name>` + tc.metadata + `</dimension></dimensions></objectModel>`
			result, err := parser.Parse([]parser.Module{{XML: []byte(xml)}})
			if err != nil {
				t.Fatal(err)
			}
			if result.FOM != nil || !result.HasCode(tc.code) {
				t.Fatalf("invalid dimension accepted: %+v", result)
			}
		})
	}
}

func TestDescriptiveMetadataMemberEmptyIsNotAbsent(t *testing.T) {
	for _, member := range []string{"attribute", "parameter"} {
		fragment := `<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Entity</name><attribute><name>A</name></attribute></objectClass></objectClass></objects>`
		if member == "parameter" {
			fragment = `<interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Event</name><parameter><name>A</name></parameter></interactionClass></interactionClass></interactions>`
		}
		a := "<objectModel>" + fragment + "</objectModel>"
		b := strings.Replace(a, "</"+member+">", "<semantics/></"+member+">", 1)
		for _, parse := range []func([]parser.Module) (parser.Result, error){parser.Parse, parser.ParseAdditive} {
			for _, inputs := range [][2]string{{a, b}, {b, a}} {
				result, err := parse([]parser.Module{{XML: []byte(inputs[0])}, {XML: []byte(inputs[1])}})
				if err != nil || result.FOM == nil || len(result.Diagnostics) != 0 {
					t.Fatalf("%s explicit empty rejected: %v %+v", member, err, result)
				}
				f := result.FOM.(*model.FOM)
				var got model.OptionalString
				if member == "attribute" {
					for _, c := range f.ObjectClasses() {
						if c.Name == "Entity" && len(c.Attributes) == 1 {
							got = c.Attributes[0].Semantics
						}
					}
				} else {
					for _, c := range f.InteractionClasses() {
						if c.Name == "Event" && len(c.Parameters) == 1 {
							got = c.Parameters[0].Semantics
						}
					}
				}
				if got != (model.OptionalString{Present: true}) {
					t.Fatalf("%s explicit empty lost: %+v", member, got)
				}
			}
		}
	}
}
