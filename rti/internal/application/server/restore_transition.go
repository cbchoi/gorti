package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

// newRestoreTransition requires the save coordinator's exclusive checkpoint
// admission. It does not upgrade the RPC's existing shared membership lease.
// State preparation/commit must not call membership or send callbacks while
// this reservation holds roster -> outbox table -> recipient locks.
func newRestoreTransition(mgr *federation.Manager, outbox *delivery.MultiOutbox) func(context.Context, core.FederationName, []savepoint.RestoreParticipant, []core.OutboxDelivery) (core.OutboxReservation, error) {
	return newRestoreTransitionWithEpochFloor(mgr, outbox, nil)
}

func newRestoreTransitionWithEpochFloor(mgr *federation.Manager, outbox *delivery.MultiOutbox, floors map[core.FederateHandle]uint64) func(context.Context, core.FederationName, []savepoint.RestoreParticipant, []core.OutboxDelivery) (core.OutboxReservation, error) {
	return newRestoreTransitionWithPending(mgr, outbox, floors, nil)
}

func newRestoreTransitionWithPending(mgr *federation.Manager, outbox *delivery.MultiOutbox, floors map[core.FederateHandle]uint64, pending map[core.FederateHandle][]core.OutboundEvent) func(context.Context, core.FederationName, []savepoint.RestoreParticipant, []core.OutboxDelivery) (core.OutboxReservation, error) {
	return func(ctx context.Context, fed core.FederationName, participants []savepoint.RestoreParticipant, terminal []core.OutboxDelivery) (core.OutboxReservation, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		generation, exists := mgr.GenerationFor(fed)
		if !exists {
			return nil, core.ErrFederationNotFound
		}
		byHandle := make(map[core.FederateHandle]*savepoint.RestoredEvent, len(terminal))
		for _, d := range terminal {
			event, ok := d.Event.(*savepoint.RestoredEvent)
			if !ok || event == nil || byHandle[d.Recipient] != nil {
				return nil, fmt.Errorf("restore transition: invalid terminal callback")
			}
			clone := *event
			byHandle[d.Recipient] = &clone
		}
		if len(byHandle) != len(participants) {
			return nil, fmt.Errorf("restore transition: terminal roster mismatch")
		}
		identities := make([]federation.RestoreIdentity, 0, len(participants))
		deliveries := make([]delivery.RestoreDelivery, 0, len(participants))
		for _, p := range participants {
			event := byHandle[p.CurrentHandle]
			if event == nil {
				return nil, fmt.Errorf("restore transition: missing terminal callback")
			}
			identities = append(identities, federation.RestoreIdentity{Name: p.Name, CurrentHandle: p.CurrentHandle, SavedHandle: p.SavedHandle, SavedModules: p.SavedModules, ModulesKnown: p.ModulesKnown, SavedAutomaticResign: p.SavedAutomaticResign})
			deliveries = append(deliveries, delivery.RestoreDelivery{CurrentHandle: p.CurrentHandle, SavedHandle: p.SavedHandle, Event: event, Pending: pending[p.SavedHandle]})
		}
		membership, err := mgr.PrepareRestoreIdentity(fed, generation, identities)
		if err != nil {
			return nil, err
		}
		queue, epochs, err := outbox.PrepareRestoreWithEpochFloor(ctx, fed, deliveries, floors)
		if err != nil {
			membership.Release()
			return nil, err
		}
		for _, p := range participants {
			byHandle[p.CurrentHandle].FederateHandle = p.SavedHandle
			byHandle[p.CurrentHandle].CallbackEpoch = epochs[p.CurrentHandle]
		}
		return &restoreTransitionReservation{membership: membership, queue: queue, epochs: epochs}, nil
	}
}

type restoreTransitionReservation struct {
	mu                sync.Mutex
	membership, queue core.OutboxReservation
	done              bool
	epochs            map[core.FederateHandle]uint64
}

func (r *restoreTransitionReservation) Commit() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return nil
	}
	// Both reservations are fully validated and cannot fail their commit.
	if err := r.membership.Commit(); err != nil {
		r.queue.Release()
		r.membership.Release()
		r.done = true
		return err
	}
	err := r.queue.Commit()
	r.done = true
	return err
}

func (r *restoreTransitionReservation) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.done {
		r.queue.Release()
		r.membership.Release()
		r.done = true
	}
}
