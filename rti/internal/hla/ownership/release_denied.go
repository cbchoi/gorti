package ownership

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var _ core.OwnershipReleaseDenier = (*Manager)(nil)

func (m *Manager) ReleaseDenied(ctx context.Context, fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) error {
	return m.releaseDenied(ctx, fed, owner, obj, attrs, canonicalPreparation(m.opts.PrepareReleaseDenied))
}

func (m *Manager) releaseDenied(ctx context.Context, fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, prepare ownershipPreparation) error {
	attrs = slices.Clone(attrs)
	slices.Sort(attrs)
	attrs = slices.Compact(attrs)
	for {
		_, mapped, expected, unlock, err := m.lockReleaseDenied(ctx, fed, owner, obj, attrs, prepare)
		if err != nil {
			return err
		}
		unlock()
		if len(expected) == 0 {
			return nil
		}
		var deliveries []core.OutboxDelivery
		for i := 0; i < len(expected); {
			acquirer := expected[i].acquirer
			var unavailable []core.AttributeHandle
			for i < len(expected) && expected[i].acquirer == acquirer {
				unavailable = append(unavailable, expected[i].attr)
				i++
			}
			deliveries = append(deliveries, core.OutboxDelivery{Recipient: acquirer, Event: unavailableEvent(obj, unavailable)})
		}
		reservable, ok := m.opts.Outbox.(core.ReservableOutbox)
		if !ok {
			return fmt.Errorf("ownership release denial requires atomic callback admission")
		}
		// Delivery reservations precede object/ownership locks, matching timed
		// deletion. Revalidate after admission because acquisitions may change.
		reservation, err := reservable.Reserve(ctx, fed, deliveries)
		if err != nil {
			return err
		}
		if reservation == nil {
			return fmt.Errorf("ownership release denial received nil callback reservation")
		}
		retry, err := m.commitReleaseDenied(ctx, fed, owner, obj, attrs, mapped, expected, deliveries, prepare, reservation)
		if !retry {
			return err
		}
	}
}

func (m *Manager) lockReleaseDenied(ctx context.Context, fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, prepare ownershipPreparation) (*federationState, []core.AttributeHandle, []acquireKey, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	attrs, releaseObject, err := prepare(ctx, fed, owner, obj, attrs)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	attrs = slices.Clone(attrs)
	slices.Sort(attrs)
	attrs = slices.Compact(attrs)
	m.mu.Lock()
	unlock := sync.OnceFunc(func() { m.mu.Unlock(); releaseObject() })
	st := m.fed[fed]
	for _, attr := range attrs {
		if st == nil || owner == core.InvalidFederateHandle || st.owners[ownershipKey{obj, attr}].owner != owner {
			unlock()
			return nil, nil, nil, nil, core.ErrAttributeNotOwned
		}
	}
	if st == nil {
		return nil, attrs, nil, unlock, nil
	}
	var pending []acquireKey
	for key := range st.pendingAcquires {
		if key.obj == obj && slices.Contains(attrs, key.attr) {
			pending = append(pending, key)
		}
	}
	slices.SortFunc(pending, func(a, b acquireKey) int {
		if a.acquirer != b.acquirer {
			return compareUint64(uint64(a.acquirer), uint64(b.acquirer))
		}
		return compareUint64(uint64(a.attr), uint64(b.attr))
	})
	return st, attrs, pending, unlock, nil
}
func (m *Manager) commitReleaseDenied(ctx context.Context, fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, attrs, mapped []core.AttributeHandle, expected []acquireKey, deliveries []core.OutboxDelivery, prepare ownershipPreparation, reservation core.OutboxReservation) (bool, error) {
	defer reservation.Release()
	st, remapped, current, unlock, err := m.lockReleaseDenied(ctx, fed, owner, obj, attrs, prepare)
	if err != nil {
		return false, err
	}
	defer unlock()
	if !slices.Equal(mapped, remapped) || !slices.Equal(expected, current) {
		return true, nil
	}
	if err := m.validateTerminalCallbacksLocked(st, deliveries); err != nil {
		return false, err
	}
	if m.opts.EventLog != nil {
		if err := m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtReleaseDenied, obj: obj, attrs: mapped, from: owner}); err != nil {
			return false, err
		}
	}
	if err := m.registerTerminalCallbacksLocked(st, fed, deliveries); err != nil {
		return false, err
	}
	for _, key := range current {
		delete(st.pendingAcquires, key)
	}
	unlock()
	err = reservation.Commit()
	m.finishTerminalAdmission(fed, deliveries, err)
	return false, err
}
