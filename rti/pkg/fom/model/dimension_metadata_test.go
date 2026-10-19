package model

import "testing"

func TestDimensionDefaultRange(t *testing.T) {
	for _, tc := range []struct {
		value             string
		lo, hi            uint64
		excluded, invalid bool
	}{
		{"", 0, 100, false, false}, {"NA", 0, 100, false, false}, {"Excluded", 0, 0, true, false},
		{"[2..8)", 2, 8, false, false}, {"[5)", 5, 6, false, false}, {"99", 99, 100, false, false},
		{"[\u0665)", 5, 6, false, false}, {"\U0001d7d3", 5, 6, false, false},
		{"[8..2)", 0, 0, false, true}, {"[2..101)", 0, 0, false, true}, {"100", 0, 0, false, true},
		{"[5..5)", 0, 0, false, true}, {"18446744073709551615", 0, 0, false, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			lo, hi, excluded, err := DimensionDefaultRange(Dimension{Name: "X", UpperBound: 100, DefaultValue: tc.value})
			if (err != nil) != tc.invalid || err == nil && (lo != tc.lo || hi != tc.hi || excluded != tc.excluded) {
				t.Fatalf("%d %d %v %v", lo, hi, excluded, err)
			}
		})
	}
}
