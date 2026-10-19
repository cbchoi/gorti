package model

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Vocabulary from the supplied IEEE1516-FDD-2010.xsd dimensionValuePattern.
// No default, range arithmetic, or bound constraint is inferred here.
var dimensionDefaultValue = regexp.MustCompile(`^(\p{Nd}+|\[\p{Nd}+\.\.\p{Nd}+\)|\[\p{Nd}+\)|Excluded)$`)

func ValidDimensionDefaultValue(value string) bool {
	return dimensionDefaultValue.MatchString(value)
}

// DimensionDefaultRange interprets the FDD value when unspecified. A single
// value (including [n)) denotes [n,n+1), not [0,n). Legacy absent/NA values
// retain the full-range behavior of existing model files.
func DimensionDefaultRange(d Dimension) (lower, upper uint64, excluded bool, err error) {
	limit := d.UpperBound
	if limit == 0 {
		limit = ^uint64(0)
	}
	if d.DefaultValue == "Excluded" {
		return 0, 0, true, nil
	}
	if d.DefaultValue == "" || d.DefaultValue == "NA" {
		return 0, limit, false, nil
	}
	if !ValidDimensionDefaultValue(d.DefaultValue) {
		return 0, 0, false, fmt.Errorf("invalid dimension default %q", d.DefaultValue)
	}
	value := strings.TrimSuffix(strings.TrimPrefix(d.DefaultValue, "["), ")")
	parts := strings.Split(value, "..")

	lower, err = parseDimensionDecimal(parts[0])
	if err != nil {
		return 0, 0, false, err
	}
	switch {
	case len(parts) == 2:
		upper, err = parseDimensionDecimal(parts[1])
	case lower == ^uint64(0):
		err = fmt.Errorf("point range overflows")
	default:
		upper = lower + 1
	}
	if err != nil {
		return 0, 0, false, err
	}
	if lower >= upper || upper > limit {
		return 0, 0, false, fmt.Errorf("dimension %q default is outside [0,%d)", d.Name, limit)
	}
	return lower, upper, false, nil
}

func parseDimensionDecimal(s string) (uint64, error) {
	var normalized strings.Builder
	for _, r := range s {
		found := false
		for _, group := range unicode.Nd.R16 {
			if uint32(r) >= uint32(group.Lo) && uint32(r) <= uint32(group.Hi) && (uint32(r)-uint32(group.Lo))%uint32(group.Stride) == 0 {
				normalized.WriteByte('0' + byte(((uint32(r)-uint32(group.Lo))/uint32(group.Stride))%10))
				found = true
				break
			}
		}
		if !found {
			for _, group := range unicode.Nd.R32 {
				if uint32(r) >= group.Lo && uint32(r) <= group.Hi && (uint32(r)-group.Lo)%group.Stride == 0 {
					normalized.WriteByte('0' + byte(((uint32(r)-group.Lo)/group.Stride)%10))
					found = true
					break
				}
			}
		}
		if !found {
			return 0, fmt.Errorf("invalid decimal digit")
		}
	}
	return strconv.ParseUint(normalized.String(), 10, 64)
}
