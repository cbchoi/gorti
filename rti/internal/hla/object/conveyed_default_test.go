package object

import (
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestConveyedDefaultOverlapRejectsEmptyRealizations(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dimensions []core.RegionDimensionRange
		want       bool
	}{
		{"all-excluded", nil, false},
		{"zero-width-default", []core.RegionDimensionRange{{Dimension: 1, Bounds: core.DDMRange{Lower: 10, Upper: 10}}}, false},
		{"one-empty-axis", []core.RegionDimensionRange{{Dimension: 1, Bounds: core.DDMRange{Lower: 0, Upper: 10}}, {Dimension: 2, Bounds: core.DDMRange{Lower: 5, Upper: 5}}}, false},
		{"included-with-excluded-omitted", []core.RegionDimensionRange{{Dimension: 1, Bounds: core.DDMRange{Lower: 0, Upper: 10}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := core.NewRegionRealizationSnapshot(7, tc.dimensions)
			if err != nil {
				t.Fatal(err)
			}
			if got := conveyedOverlapsDefault([]core.ConveyedRegionCopy{{Designator: 99, Realization: snapshot}}); got != tc.want {
				t.Fatalf("default overlap=%v want=%v", got, tc.want)
			}
		})
	}
}
