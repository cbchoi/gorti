package model

import (
	"math"
	"strings"
	"testing"
)

func TestIEEE2010UpdateRateStrictlyPositive(t *testing.T) {
	for _, decimal := range []string{"0", "-0.000", "-0.25"} {
		t.Run(decimal, func(t *testing.T) {
			if _, err := NewUpdateRate("Invalid", decimal, ""); err == nil || !strings.Contains(err.Error(), "greater than zero") {
				t.Fatalf("nonpositive decimal accepted or wrong diagnostic: %v", err)
			}
		})
	}
}

func TestIEEE2010UpdateRatePositivePrecision(t *testing.T) {
	for _, decimal := range []string{"0.00000000000000000000000000000000000001", "0." + strings.Repeat("0", 323) + "5", testPreciseDecimal} {
		rate, err := NewUpdateRate("Precise", decimal, "retained")
		if err != nil {
			t.Fatal(err)
		}
		got, err := rate.DecimalString()
		if err != nil || got != decimal || rate.Rate <= 0 {
			t.Fatalf("positive exact decimal lost: %q %v", got, err)
		}
	}
}

func TestIEEE2010UpdateRateMergeRejectsInvalidAtomically(t *testing.T) {
	valid, _ := NewUpdateRate("Valid", "1.00000000000000000001", "retained")
	for _, value := range []float64{0, math.Copysign(0, -1), -0.25, math.NaN(), math.Inf(1)} {
		bad := UpdateRate{Name: "Invalid", Rate: value}
		if _, err := bad.DecimalString(); err == nil {
			t.Errorf("invalid decimal export accepted: %v", value)
		}
		for _, invalidBase := range []bool{false, true} {
			base, incoming := []UpdateRate{valid}, []UpdateRate{{Name: "Added", Rate: 2}, bad}
			if invalidBase {
				base, incoming = incoming, base
			}
			before := append(append([]UpdateRate(nil), base...), incoming...)
			out, err := MergeUpdateRates(base, incoming)
			if out != nil || err == nil {
				t.Errorf("invalid merge produced partial result: %+v %v", out, err)
			}
			after := append(append([]UpdateRate(nil), base...), incoming...)
			for i, original := range before {
				current := after[i]
				if math.Float64bits(original.Rate) != math.Float64bits(current.Rate) {
					t.Fatal("failed merge mutated a rate")
				}
				original.Rate, current.Rate = 0, 0
				if original != current {
					t.Fatal("failed merge mutated metadata")
				}
			}
		}
	}
}

func TestIEEE2010UpdateRateRangeIsNotPositivity(t *testing.T) {
	for _, tc := range []struct{ decimal, message string }{
		{"0." + strings.Repeat("0", 323) + "1", "supported float64 range"},
		{"-0." + strings.Repeat("0", 323) + "1", "greater than zero"},
		{"1" + strings.Repeat("0", 309), "supported float64 range"},
	} {
		if _, err := NewUpdateRate("Rate", tc.decimal, ""); err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Fatalf("%q: %v, want %s", tc.decimal, err, tc.message)
		}
	}
}

func TestUpdateRatesExactDecimalConflictAndCopies(t *testing.T) {
	a, err := NewUpdateRate("Fast", "1.00000000000000000001", "details")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewUpdateRate("Fast", "1.00000000000000000002", "details")
	if err != nil {
		t.Fatal(err)
	}
	if a.Rate != b.Rate {
		t.Fatal("fixture must share a rounded float64")
	}
	if _, err := MergeUpdateRates([]UpdateRate{a}, []UpdateRate{b}); err == nil {
		t.Fatal("rounded rates hid distinct decimal definitions")
	}
	rates := []UpdateRate{a}
	f := NewFOMWithUpdateRates(nil, nil, nil, nil, rates)
	rates[0].Name = testChangedMetadata
	view := f.UpdateRates()
	view[0].Rate = 42
	if got := f.UpdateRates()[0]; got.Name != "Fast" || got.Rate != a.Rate || got.Semantics != "details" {
		t.Fatalf("model leaked metadata slice: %+v", got)
	}
	merged, err := MergeUpdateRates(f.UpdateRates(), []UpdateRate{a})
	if err != nil || len(merged) != 1 {
		t.Fatalf("equivalence: %v %v", merged, err)
	}
}

func TestAdvisoryMetadataCopiesAndConflict(t *testing.T) {
	values := []AdvisorySwitch{{Name: "attributeScopeAdvisory", Enabled: true}}
	base := NewFOM(nil, nil, nil)
	f := base.WithAdvisorySwitches(values)
	values[0].Enabled = false
	view := f.AdvisorySwitches()
	view[0].Name = testChangedMetadata
	if len(base.AdvisorySwitches()) != 0 || !f.AdvisorySwitches()[0].Enabled || f.AdvisorySwitches()[0].Name != "attributeScopeAdvisory" {
		t.Fatal("advisory metadata mutation leaked")
	}
	if _, err := MergeAdvisorySwitches(f.AdvisorySwitches(), []AdvisorySwitch{{Name: "attributeScopeAdvisory", Enabled: false}}); err == nil {
		t.Fatal("conflicting metadata accepted")
	}
}

func TestUpdateRateLongDecimalForms(t *testing.T) {
	for _, input := range []string{
		strings.Repeat("0", 5000) + "1.25",
		"1.25" + strings.Repeat("0", 5000),
		"+" + strings.Repeat("0", 5000) + "1.25" + strings.Repeat("0", 5000),
	} {
		rate, err := NewUpdateRate("long", input, "")
		if err != nil {
			t.Fatal(err)
		}
		if decimal, err := rate.DecimalString(); err != nil || decimal != "1.25" {
			t.Fatalf("long decimal round trip: %q, %v", decimal, err)
		}
	}
	rate := UpdateRate{Name: "retained", Rate: 1.25, exactRate: strings.Repeat("0", 5000) + "5/" + strings.Repeat("0", 5000) + "4"}
	if decimal, err := rate.DecimalString(); err != nil || decimal != "1.25" {
		t.Fatalf("long retained rational: %q, %v", decimal, err)
	}
}

func TestUpdateRateDecimalGrammar(t *testing.T) {
	for _, input := range []string{"+1", "1.", ".5", "+.5", "  +1.0  "} {
		if _, err := NewUpdateRate("valid", input, ""); err != nil {
			t.Errorf("valid decimal %q: %v", input, err)
		}
	}
	for _, input := range []string{"1e1000000000", "0x1p1000000000", ".", "+.", "-.", "+", "-", "++1", "--1", "+-1", "1.2.3", "1/2", "-.5", "-1.", "-0"} {
		if _, err := NewUpdateRate("invalid", input, ""); err == nil {
			t.Errorf("invalid or nonpositive decimal accepted: %q", input)
		}
	}
	for _, input := range []string{"1e1000000000", "1/1e1000000000", "0x1p1000000000", "1/0", "+/1", "1/.", "1/2/3"} {
		if _, ok := parseRetainedRate(input); ok {
			t.Errorf("invalid retained rational accepted: %q", input)
		}
	}
}
