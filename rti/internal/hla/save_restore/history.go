package savepoint

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// SaveHistoryKey distinguishes an exact execution (including generation zero)
// from legacy storage without execution provenance.
type SaveHistoryKey struct {
	Federation    core.FederationName
	Generation    uint64
	HasGeneration bool
}

// SaveHistory records the coordinator's successful-save decision, not SDK
// callback invocation. Known without HasSave means proven no-save-yet;
// a nil Time means the last successful save was immediate (or none exists).
type SaveHistoryOf[T core.LogicalTimeScalar] struct {
	Known   bool
	HasSave bool
	Name    string
	Time    *T
}

// SaveHistoryStore persists completion decisions separately from precompletion
// snapshots. A missing legacy record loads as unknown, never as no-save-yet.
// Store must atomically publish a complete record; an error leaves the previous
// record unchanged. Callers serialize writes for each execution.
type SaveHistoryStoreOf[T core.LogicalTimeScalar] interface {
	LoadSaveHistory(SaveHistoryKey) (SaveHistoryOf[T], error)
	StoreSaveHistory(SaveHistoryKey, SaveHistoryOf[T]) error
}

// UncommittedSaveDiscarder permits same-label retry after history persistence
// failed. It may discard only a bundle just written by this save operation,
// before any completion decision or successful callback has been published.
type UncommittedSaveDiscarder interface {
	DiscardUncommittedSave(SaveHistoryKey, string) error
}

func (h SaveHistoryOf[T]) Validate() error {
	if !h.Known && (h.HasSave || h.Name != "" || h.Time != nil) {
		return fmt.Errorf("savepoint: unknown history contains a save")
	}
	if !h.HasSave && (h.Name != "" || h.Time != nil) {
		return fmt.Errorf("savepoint: no-save history contains save metadata")
	}
	if h.Time != nil {
		if _, err := core.ScalarTimeValue(*h.Time); err != nil {
			return fmt.Errorf("savepoint: invalid last save time: %w", err)
		}
	}
	return nil
}

func cloneSaveHistoryOf[T core.LogicalTimeScalar](history SaveHistoryOf[T]) SaveHistoryOf[T] {
	if history.Time != nil {
		value := *history.Time
		history.Time = &value
	}
	return history
}

func historyKey(fed core.FederationName, generation *uint64) SaveHistoryKey {
	key := SaveHistoryKey{Federation: fed}
	if generation != nil {
		key.Generation, key.HasGeneration = *generation, true
	}
	return key
}

// InitializeSaveHistory is for a positively identified newly created execution.
// It preserves any durable same-execution record. Do not use it to recover an
// old execution with missing history; RecoverSaveHistory preserves that unknown.
// The caller owns lifecycle admission and supplies the new generation through
// Options.Generation. Neither method acquires checkpoint admission again.
func (m *ManagerOf[T]) InitializeSaveHistory(fed core.FederationName) error {
	return m.loadSaveHistory(fed, true)
}

// RecoverSaveHistory loads only this execution's durable decision. Restoring a
// snapshot does not rewind this history or import its source execution's label.
// This current-execution policy remains subject to full normative review.
func (m *ManagerOf[T]) RecoverSaveHistory(fed core.FederationName) error {
	return m.loadSaveHistory(fed, false)
}

func (m *ManagerOf[T]) loadSaveHistory(fed core.FederationName, fresh bool) error {
	unlock := m.lockOperation(fed)
	defer unlock()
	generation, _, err := m.resolveProvenance(fed, false)
	if err != nil {
		return err
	}
	key := historyKey(fed, generation)
	m.mu.Lock()
	history := cloneSaveHistoryOf[T](m.history[key])
	m.mu.Unlock()
	if m.opts.HistoryStore != nil {
		history, err = m.opts.HistoryStore.LoadSaveHistory(key)
		if err != nil {
			return err
		}
	}
	if err := history.Validate(); err != nil {
		return err
	}
	if fresh && !history.Known {
		history = SaveHistoryOf[T]{Known: true}
		if m.opts.HistoryStore != nil {
			if err := m.opts.HistoryStore.StoreSaveHistory(key, history); err != nil {
				return err
			}
		}
	}
	m.setSaveHistory(key, history)
	return nil
}

func (m *ManagerOf[T]) setSaveHistory(key SaveHistoryKey, history SaveHistoryOf[T]) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.history == nil {
		m.history = make(map[SaveHistoryKey]SaveHistoryOf[T])
	}
	m.history[key] = cloneSaveHistoryOf[T](history)
}

// commitSaveHistory runs after bundle persistence and full callback reservation,
// before the reservation's guaranteed publication. A process crash after this
// decision may lose undelivered callbacks but cannot turn an earlier snapshot
// into evidence that all SDK callbacks executed.
func (m *ManagerOf[T]) commitSaveHistory(key SaveHistoryKey, history SaveHistoryOf[T]) error {
	if err := history.Validate(); err != nil {
		return err
	}
	if m.opts.HistoryStore != nil {
		if err := m.opts.HistoryStore.StoreSaveHistory(key, cloneSaveHistoryOf[T](history)); err != nil {
			if discard, ok := m.opts.BundleStore.(UncommittedSaveDiscarder); ok {
				if cleanupErr := discard.DiscardUncommittedSave(key, history.Name); cleanupErr != nil {
					return fmt.Errorf("savepoint: history write: %w; uncommitted bundle cleanup: %v", err, cleanupErr)
				}
			}
			return fmt.Errorf("savepoint: history write: %w", err)
		}
	}
	m.setSaveHistory(key, history)
	return nil
}

func (m *ManagerOf[T]) reserveSaveTerminal(ctx context.Context, fed core.FederationName, label string, recipients []core.FederateHandle) (core.OutboxReservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	outbox, ok := m.opts.Outbox.(core.ReservableOutbox)
	if !ok {
		return nil, nil
	}
	deliveries := make([]core.OutboxDelivery, 0, len(recipients))
	for _, recipient := range recipients {
		deliveries = append(deliveries, core.OutboxDelivery{Recipient: recipient, Event: federationSavedEvent(label)})
	}
	reservation, err := outbox.Reserve(ctx, fed, deliveries)
	if err != nil || reservation == nil {
		if reservation != nil {
			reservation.Release()
		}
		if err == nil {
			err = fmt.Errorf("nil completion reservation")
		}
		return nil, fmt.Errorf("savepoint: reserve save completion callbacks: %w", err)
	}
	return reservation, nil
}
