package mim

import (
	"os"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

func TestDescriptiveMetadataMIMDecoder(t *testing.T) {
	source, err := os.ReadFile("../exporter/testdata/descriptive.xml")
	if err != nil {
		t.Fatal(err)
	}
	f, err := decodeMIM(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range f.ObjectClasses() {
		if c.Name != "Sensor" {
			continue
		}
		if c.Semantics != (model.OptionalString{Present: true, Value: "  A & B sensor.  "}) {
			t.Fatalf("class semantics: %+v", c.Semantics)
		}
		for _, a := range c.Attributes {
			if !a.UpdateCondition.Present {
				t.Fatalf("missing update condition: %+v", a)
			}
			if a.Name == "Quiet" && (a.UpdateCondition.Value != "" || a.Semantics.Present) {
				t.Fatalf("absent/empty confused: %+v", a)
			}
		}
	}
	for _, d := range f.Dimensions() {
		if d.Name == "Area" && (d.DataType != "Scalar" || d.NormalizationKey != "  identity map  " || d.DefaultValue != "[2..8)") {
			t.Fatalf("dimension metadata: %+v", d)
		}
	}
	for _, d := range f.DataTypes() {
		switch d := d.(type) {
		case *model.EnumeratedData:
			if d.Semantics != (model.OptionalString{Present: true}) {
				t.Fatalf("enum empty semantics: %+v", d)
			}
		case *model.VariantRecordData:
			if d.Semantics.Value != "Variant description" || d.Alternatives[1].Semantics != (model.OptionalString{Present: true}) {
				t.Fatalf("variant semantics: %+v", d)
			}
		}
	}
}

func TestDescriptiveMetadataMIMMerge(t *testing.T) {
	for _, old := range []model.OptionalString{{}, {Present: true}, {Present: true, Value: "base"}} {
		base := model.NewFOM([]model.ObjectClass{{Name: "Object", Semantics: old}}, []model.InteractionClass{{Name: "Interaction", Semantics: old}}, nil)
		for _, next := range []model.OptionalString{{}, {Present: true}, {Present: true, Value: "base"}, {Present: true, Value: "different"}} {
			user := model.NewFOM([]model.ObjectClass{{Name: "Object", Semantics: next}}, []model.InteractionClass{{Name: "Interaction", Semantics: next}}, nil)
			merged, diags := Merge(base, user)
			conflict := old.Present && next.Present && old.Value != next.Value
			if conflict {
				if merged != nil || len(diags) == 0 {
					t.Fatalf("MIM conflicting semantics accepted: %+v %+v", old, next)
				}
				continue
			}
			if merged == nil || len(diags) != 0 {
				t.Fatal(diags)
			}
			want := old
			if !want.Present {
				want = next
			}
			if merged.ObjectClasses()[0].Semantics != want || merged.InteractionClasses()[0].Semantics != want {
				t.Fatal("MIM merge dropped metadata")
			}
		}
	}
	for _, mutate := range []func(*model.Dimension){
		func(d *model.Dimension) { d.DataType = "Other" },
		func(d *model.Dimension) { d.DefaultValue = "[0..8)" },
	} {
		d := model.Dimension{Name: "X", DataType: "Scalar", DefaultValue: "Excluded"}
		base := model.NewFOMWithDimensions(nil, nil, nil, []model.Dimension{d})
		mutate(&d)
		merged, diags := Merge(base, model.NewFOMWithDimensions(nil, nil, nil, []model.Dimension{d}))
		if merged != nil || len(diags) == 0 {
			t.Fatal("MIM dimension conflict accepted")
		}
	}
}

func TestDescriptiveMetadataMIMRejectsEmptyDimension(t *testing.T) {
	for _, metadata := range []string{"<dataType/>", "<normalization/>", "<value/>", "<value>Included</value>"} {
		_, err := decodeMIM([]byte(`<objectModel><dimensions><dimension><name>X</name>` + metadata + `</dimension></dimensions></objectModel>`))
		if err == nil {
			t.Fatalf("invalid MIM metadata accepted: %s", metadata)
		}
	}
}

func TestDimensionLegacyNAMIMDecoder(t *testing.T) {
	f, err := decodeMIM([]byte(`<objectModel><dimensions><dimension><name>X</name><upperBound>1000</upperBound><value>NA</value></dimension></dimensions></objectModel>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Dimensions(); len(got) != 1 || got[0].DefaultValue != "NA" || got[0].UpperBound != 1000 {
		t.Fatalf("MIM decoder dropped legacy dimension metadata: %+v", got)
	}
}
