package model

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// UpdateRate retains the named value and descriptive metadata from an FDD.
// exactRate distinguishes decimal values that round to the same float64.
type UpdateRate struct {
	Name      string
	Rate      float64
	Semantics string
	// SemanticsPresent distinguishes explicit empty XML from absent metadata.
	// A legacy nonempty Semantics string remains present without this flag.
	SemanticsPresent bool
	exactRate        string
}

var decimalRate = regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)$`)

// NewUpdateRate accepts the xs:decimal lexical form used by the supplied
// IEEE1516-FDD-2010.xsd and requires a positive value per IEEE 1516.2-2010
// 4.11.2 and 6.2.11. Finite, nonzero float64 representation is a product
// limitation, separate from the exact-decimal positivity requirement.
func NewUpdateRate(name, decimal, semantics string) (UpdateRate, error) {
	name, decimal = strings.TrimSpace(name), strings.TrimSpace(decimal)
	if name == "" {
		return UpdateRate{}, fmt.Errorf("update rate requires a name")
	}
	if !decimalRate.MatchString(decimal) {
		return UpdateRate{}, fmt.Errorf("update rate %q requires an xs:decimal value", name)
	}
	exact, ok := parseDecimalRate(decimal)
	if !ok {
		return UpdateRate{}, fmt.Errorf("update rate %q has an invalid decimal", name)
	}
	if exact.Sign() <= 0 {
		return UpdateRate{}, fmt.Errorf("update rate %q must be greater than zero", name)
	}
	value, _ := exact.Float64()
	if math.IsInf(value, 0) || (value == 0 && exact.Sign() != 0) {
		return UpdateRate{}, fmt.Errorf("update rate %q is outside the supported float64 range", name)
	}
	return UpdateRate{Name: name, Rate: value, Semantics: semantics, exactRate: exact.RatString()}, nil
}

func (f *FOM) UpdateRates() []UpdateRate { return append([]UpdateRate(nil), f.updateRates...) }

func (r UpdateRate) SemanticsDeclaration() OptionalString {
	return OptionalString{Value: r.Semantics, Present: r.SemanticsPresent || r.Semantics != ""}
}

// DecimalString returns an exact, non-exponent xs:decimal representation.
// Parsed rates use the retained rational, not the rounded runtime float64.
// Programmatic rates without a retained decimal use the shortest decimal
// that round-trips through float64. A mutated or nonfinite rate is rejected.
func (r UpdateRate) DecimalString() (string, error) {
	if math.IsNaN(r.Rate) || math.IsInf(r.Rate, 0) {
		return "", fmt.Errorf("update rate %q is not finite", r.Name)
	}
	if r.exactRate == "" {
		if r.Rate <= 0 {
			return "", fmt.Errorf("update rate %q must be greater than zero", r.Name)
		}
		return strconv.FormatFloat(r.Rate, 'f', -1, 64), nil
	}
	exact, ok := parseRetainedRate(r.exactRate)
	if !ok {
		return "", fmt.Errorf("update rate %q has invalid retained decimal", r.Name)
	}
	if exact.Sign() <= 0 {
		return "", fmt.Errorf("update rate %q must be greater than zero", r.Name)
	}
	value, _ := exact.Float64()
	if value != r.Rate {
		return "", fmt.Errorf("update rate %q differs from its retained decimal", r.Name)
	}
	denominator := new(big.Int).Set(exact.Denom())
	places := 0
	for _, factor := range []int64{2, 5} {
		count := 0
		divisor := big.NewInt(factor)
		for new(big.Int).Mod(denominator, divisor).Sign() == 0 {
			denominator.Quo(denominator, divisor)
			count++
		}
		if count > places {
			places = count
		}
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return "", fmt.Errorf("update rate %q is not a terminating decimal", r.Name)
	}
	return exact.FloatString(places), nil
}

// MergeUpdateRates compares the FDD name and maximum rate fields per IEEE
// 1516.1-2010 4.1.4.1. Non-FDD descriptions retain their first supplied value,
// filling absent metadata without affecting acceptance. Inputs are not mutated.
func MergeUpdateRates(base, incoming []UpdateRate) ([]UpdateRate, error) {
	var out []UpdateRate
	index := make(map[string]int)
	for _, declarations := range [][]UpdateRate{base, incoming} {
		for _, rate := range declarations {
			if _, err := rate.DecimalString(); err != nil {
				return nil, err
			}
			if i, exists := index[rate.Name]; exists {
				old := out[i]
				conflict := old.Rate != rate.Rate || (old.exactRate != "" && rate.exactRate != "" && old.exactRate != rate.exactRate)
				if conflict {
					return nil, fmt.Errorf("conflicting update rate %q", rate.Name)
				}
				if !old.SemanticsDeclaration().Present {
					out[i].Semantics = rate.Semantics
					out[i].SemanticsPresent = rate.SemanticsPresent
				}
				if old.exactRate == "" {
					out[i].exactRate = rate.exactRate
				}
				continue
			}
			index[rate.Name] = len(out)
			out = append(out, rate)
		}
	}
	return out, nil
}

// The constructor has checked the decimal grammar. The exponent is the number
// of fractional digits already present in the input, never a concise exponent.
func parseDecimalRate(decimal string) (*big.Rat, bool) {
	whole, fraction, _ := strings.Cut(decimal, ".")
	numerator, ok := new(big.Int).SetString(whole+fraction, 10)
	if !ok {
		return nil, false
	}
	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(len(fraction))), nil)
	return new(big.Rat).SetFrac(numerator, denominator), true
}

func parseRetainedRate(text string) (*big.Rat, bool) {
	numeratorText, denominatorText, fraction := strings.Cut(text, "/")
	numerator, ok := new(big.Int).SetString(numeratorText, 10)
	if !ok {
		return nil, false
	}
	denominator := big.NewInt(1)
	if fraction {
		var valid bool
		denominator, valid = new(big.Int).SetString(denominatorText, 10)
		if !valid || denominator.Sign() == 0 {
			return nil, false
		}
	}
	return new(big.Rat).SetFrac(numerator, denominator), true
}
