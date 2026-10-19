package savebundle

import (
	"math"
	"testing"
)

func TestSnapshotScalarBoundaryCompatibility(t *testing.T) {
	for _, test := range []struct {
		name          string
		value         float64
		valid, finite bool
	}{
		{"zero", 0, true, true},
		{"negative zero", math.Copysign(0, -1), true, true},
		{"finite maximum", math.MaxFloat64, true, true},
		{"legacy final time", math.Inf(1), true, false},
		{"negative infinity", math.Inf(-1), false, false},
		{"not a number", math.NaN(), false, false},
		{"negative", -1, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validSnapshotScalar(test.value); got != test.valid {
				t.Fatalf("validSnapshotScalar(%v) = %v, want %v", test.value, got, test.valid)
			}
			if got := finiteSnapshotScalar(test.value); got != test.finite {
				t.Fatalf("finiteSnapshotScalar(%v) = %v, want %v", test.value, got, test.finite)
			}
		})
	}
	for _, value := range []int64{math.MinInt64, -1, 0, 1, math.MaxInt64} {
		if validSnapshotScalar(value) != (value >= 0) || finiteSnapshotScalar(value) != (value >= 0) {
			t.Fatalf("integer snapshot boundary changed for %d", value)
		}
	}
}
