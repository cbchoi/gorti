package exporter_test

import (
	"os"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/exporter"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

func BenchmarkRetainedFDD(b *testing.B) {
	data, err := os.ReadFile("testdata/retained.xml")
	if err != nil {
		b.Fatal(err)
	}
	modules := []parser.Module{{Path: "retained.xml", XML: data}}
	parsed, err := parser.Parse(modules)
	if err != nil || len(parsed.Diagnostics) != 0 {
		b.Fatalf("parse: %v %+v", err, parsed.Diagnostics)
	}
	fom := parsed.FOM.(*model.FOM)
	b.Run("Parse", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			result, err := parser.Parse(modules)
			if err != nil || len(result.Diagnostics) != 0 {
				b.Fatalf("parse: %v %+v", err, result.Diagnostics)
			}
		}
	})
	b.Run("Export", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			encoded, err := exporter.Marshal(fom)
			if err != nil || len(encoded) == 0 {
				b.Fatalf("export: %v", err)
			}
		}
	})
}
