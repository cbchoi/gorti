package exporter_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/exporter"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

func TestDimensionLegacyNAIsRetainedButNotInventedInFDD(t *testing.T) {
	source, err := os.ReadFile("testdata/descriptive.xml")
	if err != nil {
		t.Fatal(err)
	}
	source = []byte(strings.Replace(string(source), "<value>[2..8)</value>", "<value>NA</value>", 1))
	f := parse(t, source)
	for _, d := range f.Dimensions() {
		if d.Name == "Area" && d.DefaultValue != "NA" {
			t.Fatalf("legacy default was changed to %q", d.DefaultValue)
		}
	}
	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatal(err)
	}
	merged, diags := mim.Merge(base, f)
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	for _, candidate := range []*model.FOM{f, merged} {
		data, err := exporter.Marshal(candidate)
		if data != nil || !errors.Is(err, exporter.ErrUnsupported) || !strings.Contains(err.Error(), `invalid dimension value "NA" on Area`) {
			t.Fatalf("legacy NA must not become schema-valid fabricated FDD: data=%q error=%v", data, err)
		}
	}
}
