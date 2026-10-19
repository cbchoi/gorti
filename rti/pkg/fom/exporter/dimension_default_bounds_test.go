package exporter_test

import (
	"github.com/cbchoi/gorti/rti/pkg/fom/exporter"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"testing"
)

func TestDimensionDefaultBoundsExportRejectsInvalid(t *testing.T) {
	for _, value := range []string{"[2..2)", "[8..2)", "[0..101)", "100"} {
		t.Run(value, func(t *testing.T) {
			f := model.NewFOMWithDimensions(nil, nil, nil, []model.Dimension{{Name: "X", UpperBound: 100, DefaultValue: value}})
			if _, err := exporter.Marshal(f); err == nil {
				t.Fatal("invalid dimension default exported")
			}
		})
	}
}
