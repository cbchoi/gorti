package model

import (
	"math"
	"testing"
)

func TestUpdateRateLosslessDecimalExport(t *testing.T) {
	r, err := NewUpdateRate("Precise", testPreciseDecimal, "exact")
	if err != nil {
		t.Fatal(err)
	}
	decimal, ok := any(r).(interface{ DecimalString() (string, error) })
	if !ok {
		t.Fatal("exact update-rate decimal is retained but inaccessible to exporters")
	}
	got, err := decimal.DecimalString()
	if err != nil || got != testPreciseDecimal {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestUpdateRateMergeRetainsIncomingExactDecimal(t *testing.T) {
	exact, err := NewUpdateRate("Precise", testPreciseDecimal, "")
	if err != nil {
		t.Fatal(err)
	}
	merged, err := MergeUpdateRates([]UpdateRate{{Name: "Precise", Rate: 1}}, []UpdateRate{exact})
	if err != nil {
		t.Fatal(err)
	}
	got, err := merged[0].DecimalString()
	if err != nil || got != testPreciseDecimal {
		t.Fatalf("merge lost exact metadata: %q %v", got, err)
	}
}

func TestUpdateRateDecimalFormsAndMutation(t *testing.T) {
	for _, input := range []string{"1", "+12.500", ".00000000000000000000000000000000000001", "10000000000000000000000000000.5"} {
		r, err := NewUpdateRate("r", input, "")
		if err != nil {
			t.Fatal(err)
		}
		text, err := r.DecimalString()
		if err != nil {
			t.Fatal(err)
		}
		again, err := NewUpdateRate("r", text, "")
		if err != nil || r.exactRate != again.exactRate {
			t.Fatalf("%q roundtrip = %q: %v", input, text, err)
		}
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := (UpdateRate{Name: "bad", Rate: value}).DecimalString(); err == nil {
			t.Fatal("nonfinite rate accepted")
		}
	}
	r, _ := NewUpdateRate("mutated", "1.25", "")
	r.Rate = 2
	if _, err := r.DecimalString(); err == nil {
		t.Fatal("mutated runtime value hid exact metadata")
	}
}
