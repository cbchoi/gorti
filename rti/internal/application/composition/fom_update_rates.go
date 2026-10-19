package composition

import "github.com/cbchoi/gorti/rti/internal/hla/core"

var _ core.FOMUpdateRates = (*fomHandle)(nil)

func (h *fomHandle) UpdateRateValue(name string) (float64, bool) {
	if !h.IsValid() {
		return 0, false
	}
	for _, rate := range h.fom.UpdateRates() {
		if rate.Name == name {
			return rate.Rate, true
		}
	}
	return 0, false
}
