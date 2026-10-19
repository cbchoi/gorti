package model

import (
	"fmt"
	"sort"
)

// AdvisorySwitch is a present FDD boolean switch declaration. The historical
// name also covers the six non-advisory switches; absence remains unknown.
type AdvisorySwitch struct {
	Name    string
	Enabled bool
}

// WithAdvisorySwitches returns an independent metadata view of this immutable
// FOM. It does not invent values for switches absent from the source.
func (f *FOM) WithAdvisorySwitches(values []AdvisorySwitch) *FOM {
	cloned := *f
	cloned.advisorySwitches = append([]AdvisorySwitch(nil), values...)
	sort.Slice(cloned.advisorySwitches, func(i, j int) bool { return cloned.advisorySwitches[i].Name < cloned.advisorySwitches[j].Name })
	return &cloned
}

func (f *FOM) AdvisorySwitches() []AdvisorySwitch {
	return append([]AdvisorySwitch(nil), f.advisorySwitches...)
}

func MergeAdvisorySwitches(base, incoming []AdvisorySwitch) ([]AdvisorySwitch, error) {
	out := append([]AdvisorySwitch(nil), base...)
	known := make(map[string]bool, len(out))
	for _, value := range out {
		known[value.Name] = value.Enabled
	}
	for _, value := range incoming {
		if previous, exists := known[value.Name]; exists {
			if previous != value.Enabled {
				return nil, fmt.Errorf("conflicting advisory switch %q", value.Name)
			}
			continue
		}
		known[value.Name] = value.Enabled
		out = append(out, value)
	}
	return out, nil
}
