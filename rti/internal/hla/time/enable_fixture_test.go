package time

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// Grant/message fixtures consume enable acknowledgements during setup. Enable
// tests use enableTestOutbox instead and retain every callback for assertions.
func isEnableCallback(e core.OutboundEvent) bool {
	switch e.(type) {
	case *TimeRegulationEnabled, *TimeConstrainedEnabled:
		return true
	}
	return false
}

func enableFixtureAdmission(ds []core.OutboxDelivery) (core.OutboxReservation, bool) {
	if len(ds) == 0 {
		return nil, false
	}
	for _, d := range ds {
		if !isEnableCallback(d.Event) {
			return nil, false
		}
	}
	return enableFixtureReservation{}, true
}

type enableFixtureReservation struct{}

func (enableFixtureReservation) Commit() error { return nil }
func (enableFixtureReservation) Release()      {}
