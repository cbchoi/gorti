package composition

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var _ core.FOMSubscriptionEvaluation = (*fomHandle)(nil)

func (h *fomHandle) DelaySubscriptionEvaluation() (bool, bool) {
	if !h.IsValid() {
		return false, false
	}
	for _, setting := range h.fom.AdvisorySwitches() {
		if setting.Name == "delaySubscriptionEvaluation" {
			return setting.Enabled, true
		}
	}
	return false, false
}

func validateDelayedSubscriptionExtension(base, merged *fomHandle) error {
	before, _ := base.DelaySubscriptionEvaluation()
	after, _ := merged.DelaySubscriptionEvaluation()
	if before != after {
		return fmt.Errorf("FOM extension changes the federation's immutable delaySubscriptionEvaluation switch")
	}
	return nil
}
