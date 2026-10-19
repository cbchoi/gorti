package declaration

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func cloneDeclarationTable[K comparable, V any](table map[K]map[core.FederateHandle]V) map[K]map[core.FederateHandle]V {
	result := make(map[K]map[core.FederateHandle]V, len(table))
	for key, members := range table {
		result[key] = maps.Clone(members)
	}
	return result
}

func cloneFederationState(st *federationState) *federationState {
	if st == nil {
		return newFederationState()
	}
	cloned := *st
	cloned.objPubs = cloneDeclarationTable(st.objPubs)
	cloned.objPubHistory = maps.Clone(st.objPubHistory)
	cloned.objSubs = cloneDeclarationTable(st.objSubs)
	cloned.intPubs = cloneDeclarationTable(st.intPubs)
	cloned.intSubs = cloneDeclarationTable(st.intSubs)
	cloned.objOptions = cloneDeclarationTable(st.objOptions)
	cloned.intActive = cloneDeclarationTable(st.intActive)
	cloned.regionalObjects = cloneDeclarationTable(st.regionalObjects)
	cloned.regionalInteractions = cloneDeclarationTable(st.regionalInteractions)
	cloned.switches = make(map[core.FederateHandle]map[core.AdvisorySwitchKind]bool, len(st.switches))
	for h, kinds := range st.switches {
		cloned.switches[h] = maps.Clone(kinds)
	}
	cloned.objSubSnapshots = maps.Clone(st.objSubSnapshots)
	cloned.intSubSnapshots = maps.Clone(st.intSubSnapshots)
	cloned.rateObjects = make(map[core.ObjectHandle]rateObject, len(st.rateObjects))
	for obj, state := range st.rateObjects {
		state.owners = maps.Clone(state.owners)
		cloned.rateObjects[obj] = state
	}
	cloned.regionalRates = slices.Clone(st.regionalRates)
	cloned.pendingAdvice = maps.Clone(st.pendingAdvice)
	return &cloned
}

// Caller holds the declaration lock through admission and delivery. Outbox
// implementations must not reenter declaration services. Production reservations
// hold only recipient queue locks, and readers see the committed state after
// delivery. All callback capacity is secured before replacing the live state.
func (m *Manager) commitDeclarationChange(ctx context.Context, fed core.FederationName, previous, candidate *federationState, deliveries []core.OutboxDelivery, mandatory bool) error {
	if mandatory {
		defer func() { m.fed[fed] = candidate }()
	}
	var reservation core.OutboxReservation
	if len(deliveries) > 0 {
		if outbox, ok := m.advisoryOutbox.(core.ReservableOutbox); ok {
			var err error
			reservation, err = outbox.Reserve(ctx, fed, deliveries)
			if err != nil {
				return err
			}
			if reservation == nil {
				return fmt.Errorf("declaration: outbox returned nil reservation")
			}
			defer reservation.Release()
		} else if len(deliveries) > 1 {
			return fmt.Errorf("declaration: atomic advisory fanout requires a reservable outbox")
		}
	}
	m.fed[fed] = candidate
	var err error
	if reservation != nil {
		err = reservation.Commit()
	} else if len(deliveries) == 1 {
		err = m.advisoryOutbox.Send(ctx, fed, deliveries[0].Recipient, deliveries[0].Event)
	}
	if err != nil {
		if previous == nil {
			delete(m.fed, fed)
		} else {
			m.fed[fed] = previous
		}
	}
	return err
}
