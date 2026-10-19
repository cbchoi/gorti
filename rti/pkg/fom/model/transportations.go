package model

import (
	"fmt"
	"sort"
	"strings"
)

// Transportation retains an FDD declaration, not a runtime transport capability.
// Reliable is the supplied Yes/No value; it is never inferred from Name.
type Transportation struct {
	Name      string
	Reliable  string
	Semantics OptionalString
}

func (f *FOM) Transportations() []Transportation {
	return append([]Transportation(nil), f.transportations...)
}

func (f *FOM) WithTransportations(values []Transportation) *FOM {
	cloned := *f
	cloned.transportations = append([]Transportation(nil), values...)
	sort.SliceStable(cloned.transportations, func(i, j int) bool {
		return cloned.transportations[i].Name < cloned.transportations[j].Name
	})
	return &cloned
}

// ValidateTransportation checks the retained schema vocabulary only. It does
// not select a runtime implementation or certify the declared reliability.
func ValidateTransportation(value Transportation) error {
	if value.Name == "" || strings.TrimSpace(value.Name) != value.Name {
		return fmt.Errorf("transportation requires a nonempty normalized name")
	}
	if value.Reliable != "Yes" && value.Reliable != "No" {
		return fmt.Errorf("transportation %q requires reliable Yes or No", value.Name)
	}
	return nil
}

// MergeTransportations compares the FDD name and reliability fields per IEEE
// 1516.1-2010 4.1.4.1. Descriptions cannot cause RTI merge rejection; retain the
// first supplied description, filling an absent one from a later declaration.
func MergeTransportations(base, incoming []Transportation) ([]Transportation, error) {
	var out []Transportation
	index := map[string]int{}
	for _, declarations := range [][]Transportation{base, incoming} {
		for _, value := range declarations {
			if err := ValidateTransportation(value); err != nil {
				return nil, err
			}
			if i, exists := index[value.Name]; exists {
				old := out[i]
				if old.Reliable != value.Reliable {
					return nil, fmt.Errorf("conflicting transportation %q", value.Name)
				}
				if !old.Semantics.Present {
					out[i].Semantics = value.Semantics
				}
				continue
			}
			index[value.Name] = len(out)
			out = append(out, value)
		}
	}
	return out, nil
}
