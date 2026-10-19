package delivery

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"sort"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// RestoreDelivery keeps a live stream while replacing its saved identity and
// pending callback epoch. Event is first in the new epoch, followed by Pending.
// The caller prepares and admits fresh callback tickets before Commit; saved
// receipt identities must not be reused in Pending.
type RestoreDelivery struct {
	CurrentHandle core.FederateHandle
	SavedHandle   core.FederateHandle
	Event         core.OutboundEvent
	Pending       []core.OutboundEvent
}

type restoreReservation struct {
	mu         sync.Mutex
	m          *MultiOutbox
	fed        core.FederationName
	deliveries []RestoreDelivery
	states     []*multiRecipientState
	next       map[fedHandleKey]*multiRecipientState
	generation uint64
	epochs     map[core.FederateHandle]uint64
	batches    [][][]core.OutboundEvent
	done       bool
}

// PrepareRestore reserves rekeying and terminal/pending batches, even if old queues are
// full. Release changes nothing. Commit purges only events still owned by the
// outbox; an already-dequeued transport frame precedes the epoch terminal on
// that same stream. SDKs must invalidate their queued old-epoch callbacks when
// they consume the terminal. Lock order: writeMu, ascending current handle.
func (m *MultiOutbox) PrepareRestore(ctx context.Context, fed core.FederationName, deliveries []RestoreDelivery) (core.OutboxReservation, map[core.FederateHandle]uint64, error) {
	return m.PrepareRestoreWithEpochFloor(ctx, fed, deliveries, nil)
}

// PrepareRestoreWithEpochFloor prevents restored callback identities from
// reusing an epoch carried by a saved invocation ledger, including after rejoin.
func (m *MultiOutbox) PrepareRestoreWithEpochFloor(ctx context.Context, fed core.FederationName, deliveries []RestoreDelivery, floors map[core.FederateHandle]uint64) (core.OutboxReservation, map[core.FederateHandle]uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	m.writeMu.Lock()
	r := &restoreReservation{m: m, fed: fed, deliveries: append([]RestoreDelivery(nil), deliveries...), generation: m.restoreGeneration.Load() + 1}
	for i := range r.deliveries {
		r.deliveries[i].Pending = append([]core.OutboundEvent(nil), r.deliveries[i].Pending...)
	}
	bad := func(err error) (core.OutboxReservation, map[core.FederateHandle]uint64, error) {
		r.finish()
		return nil, nil, err
	}
	if r.generation == 0 || len(deliveries) == 0 {
		return bad(fmt.Errorf("outbox restore: empty transition or exhausted epoch"))
	}
	sort.Slice(r.deliveries, func(i, j int) bool { return r.deliveries[i].CurrentHandle < r.deliveries[j].CurrentHandle })
	current := *m.subs.Load()
	if err := validateRestoreParticipants(fed, r.deliveries, floors, current); err != nil {
		return bad(err)
	}
	epochs := make(map[core.FederateHandle]uint64, len(deliveries))
	for _, d := range r.deliveries {
		state := current[fedHandleKey{fed: fed, h: d.CurrentHandle}]
		epoch, err := r.prepareRecipient(d, state, floors[d.CurrentHandle])
		if err != nil {
			return bad(err)
		}
		epochs[d.CurrentHandle] = epoch
	}
	r.epochs = maps.Clone(epochs)
	r.next = make(map[fedHandleKey]*multiRecipientState, len(current))
	for key, state := range current {
		if key.fed != fed {
			r.next[key] = state
		}
	}
	for i, d := range r.deliveries {
		r.next[fedHandleKey{fed: fed, h: d.SavedHandle}] = r.states[i]
	}
	return r, epochs, nil
}

func validateRestoreParticipants(fed core.FederationName, deliveries []RestoreDelivery, floors map[core.FederateHandle]uint64, current map[fedHandleKey]*multiRecipientState) error {
	sources, targets := map[core.FederateHandle]bool{}, map[core.FederateHandle]bool{}
	for _, d := range deliveries {
		if d.CurrentHandle == 0 || d.SavedHandle == 0 || d.Event == nil || sources[d.CurrentHandle] || targets[d.SavedHandle] {
			return fmt.Errorf("outbox restore: invalid or duplicate participant")
		}
		sources[d.CurrentHandle], targets[d.SavedHandle] = true, true
	}
	for h := range floors {
		if !sources[h] {
			return fmt.Errorf("outbox restore: epoch floor for unknown recipient %d", h)
		}
	}
	// A full federation transition cannot leave an unaccounted old-epoch queue.
	for key := range current {
		if key.fed == fed && !sources[key.h] {
			return fmt.Errorf("outbox restore: unaccounted recipient %d", key.h)
		}
	}
	return nil
}

// The reservation retains every acquired recipient lock, including on error;
// its caller releases the accumulated locks through finish.
func (r *restoreReservation) prepareRecipient(d RestoreDelivery, state *multiRecipientState, floor uint64) (uint64, error) {
	if state == nil {
		return 0, fmt.Errorf("%w: restore recipient %d", core.ErrOutboxUnavailable, d.CurrentHandle)
	}
	state.mu.Lock()
	r.states = append(r.states, state)
	previous := max(state.callbackEpoch, floor)
	if state.closed || previous == ^uint64(0) {
		return 0, fmt.Errorf("%w: closed recipient or exhausted epoch", core.ErrOutboxUnavailable)
	}
	if len(d.Pending) >= cap(state.ch)*r.m.batchSize {
		return 0, fmt.Errorf("%w: restore recipient %d terminal and pending exceed event capacity", core.ErrFederateOverflow, d.CurrentHandle)
	}
	if err := validateRestoreEvent(d.Event); err != nil {
		return 0, fmt.Errorf("outbox restore terminal for %d: %w", d.CurrentHandle, err)
	}
	for i, event := range d.Pending {
		if err := validateRestoreEvent(event); err != nil {
			return 0, fmt.Errorf("outbox restore pending %d for %d: %w", i, d.CurrentHandle, err)
		}
	}
	events := make([]core.OutboundEvent, 1, len(d.Pending)+1)
	events[0] = d.Event
	events = append(events, d.Pending...)
	batches := make([][]core.OutboundEvent, 0, (len(events)-1)/r.m.batchSize+1)
	for len(events) > 0 {
		n := min(len(events), r.m.batchSize)
		batches = append(batches, events[:n:n])
		events = events[n:]
	}
	r.batches = append(r.batches, batches)
	return previous + 1, nil
}

func validateRestoreEvent(event core.OutboundEvent) error {
	if event == nil {
		return fmt.Errorf("nil callback")
	}
	switch v := reflect.ValueOf(event); v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if v.IsNil() {
			return fmt.Errorf("nil callback carrier %T", event)
		}
	}
	if guarded, ok := event.(*guardedEvent); ok {
		if guarded.guard == nil {
			return fmt.Errorf("callback projection guard unavailable")
		}
		if err := guarded.guard.Validate(); err != nil {
			return err
		}
		return validateRestoreEvent(guarded.event)
	}
	return nil
}

func (r *restoreReservation) Commit() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return nil
	}
	for i, state := range r.states {
		observation := state.observation
		if observation != nil {
			observation.mu.Lock()
		}
		r.m.disarmFlushLocked(state)
		state.scratch = make([]core.OutboundEvent, 0, r.m.batchSize)
		var removed uint64
		for len(state.ch) > 0 {
			select {
			case batch := <-state.ch:
				removed += uint64(len(batch))
			default:
			}
		}
		if observation != nil {
			observation.restoredLocked(removed)
			observation.mu.Unlock()
		}
		state.key = fedHandleKey{fed: r.fed, h: r.deliveries[i].SavedHandle}
		state.callbackEpoch = r.epochs[r.deliveries[i].CurrentHandle]
		state.restoreGeneration = r.generation
		state.observation = r.m.newQueueObservation(state)
	}
	r.m.subs.Store(&r.next)
	// Publish after map replacement. A sender captures this number before its
	// map lookup and revalidates under the recipient lock, fencing parked sends.
	r.m.restoreGeneration.Store(r.generation)
	for i, state := range r.states {
		observation := state.observation
		if observation != nil {
			observation.mu.Lock()
		}
		for _, batch := range r.batches[i] {
			if observation != nil {
				for range batch {
					observation.admitLocked()
				}
			}
			state.ch <- batch
			if observation != nil {
				observation.batchLocked(uint64(len(batch)))
			}
		}
		if observation != nil {
			observation.mu.Unlock()
		}
	}
	r.finish()
	return nil
}

func (r *restoreReservation) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.done {
		r.finish()
	}
}

func (r *restoreReservation) finish() {
	for i := len(r.states) - 1; i >= 0; i-- {
		r.states[i].mu.Unlock()
	}
	r.done = true
	r.m.writeMu.Unlock()
}
