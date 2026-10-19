package time

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func retractionManager(t *testing.T) (*Manager, core.RetractionIDAdmission) {
	t.Helper()
	m, err := New(Options{Clock: core.NewManualClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	a, ok := any(m).(core.RetractionIDAdmission)
	if !ok {
		t.Fatal("time manager has no originating retraction ID admission")
	}
	return m, a
}

func TestRetractionIDAdmissionExactReuseAndRollback(t *testing.T) {
	_, admission := retractionManager(t)
	ctx := context.Background()
	first, err := admission.ReserveRetractionID(ctx, "fed", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admission.ReserveRetractionID(ctx, "fed", 1, 100); !errors.Is(err, core.ErrRetractionIDAlreadyUsed) {
		t.Fatalf("concurrent duplicate: %v", err)
	}
	first.Release()
	retry, err := admission.ReserveRetractionID(ctx, "fed", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := retry.Commit(); err != nil {
		t.Fatal(err)
	}
	retry.Release()
	if _, err := admission.ReserveRetractionID(ctx, "fed", 1, 100); !errors.Is(err, core.ErrRetractionIDAlreadyUsed) {
		t.Fatalf("accepted duplicate: %v", err)
	}
	for _, id := range []uint64{2, 99, 1, 101} {
		r, err := admission.ReserveRetractionID(ctx, "fed", 1, id)
		if err != nil {
			t.Fatalf("unique nonmonotonic ID %d: %v", id, err)
		}
		if err := r.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	other, err := admission.ReserveRetractionID(ctx, "fed", 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	other.Release()
}

func TestRetractionIDAdmissionOneConcurrentWinner(t *testing.T) {
	_, admission := retractionManager(t)
	var wg sync.WaitGroup
	results := make(chan error, 24)
	for i := 0; i < cap(results); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := admission.ReserveRetractionID(context.Background(), "fed", 1, 7)
			if err == nil {
				err = r.Commit()
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, core.ErrRetractionIDAlreadyUsed) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("accepted %d operations sharing one ID", winners)
	}
}

func TestRetractionIDAdmissionHistorySurvivesResignButNotDestroy(t *testing.T) {
	m, admission := retractionManager(t)
	ctx := context.Background()
	r, err := admission.ReserveRetractionID(ctx, "fed", 1, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	m.OnFederateResign(ctx, "fed", 1)
	if _, err := admission.ReserveRetractionID(ctx, "fed", 1, 7); !errors.Is(err, core.ErrRetractionIDAlreadyUsed) {
		t.Fatalf("history lost on resign: %v", err)
	}
	stale, err := admission.ReserveRetractionID(ctx, "fed", 1, 8)
	if err != nil {
		t.Fatal(err)
	}
	m.OnFederationDestroyed("fed")
	fresh, err := admission.ReserveRetractionID(ctx, "fed", 1, 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := stale.Commit(); err == nil {
		t.Fatal("stale generation reservation committed")
	}
	stale.Release()
	if err := fresh.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := admission.ReserveRetractionID(ctx, "fed", 1, 8); !errors.Is(err, core.ErrRetractionIDAlreadyUsed) {
		t.Fatalf("stale release erased new generation: %v", err)
	}
}

func TestRetractionHistoryUnionPreservesPostSaveAndDeliveredIDs(t *testing.T) {
	m, admission := retractionManager(t)
	accept := func(id uint64) {
		t.Helper()
		r, err := admission.ReserveRetractionID(context.Background(), "fed", 1, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	accept(7)
	saved, err := m.ExportRetractionHistory("fed")
	if err != nil {
		t.Fatal(err)
	}
	accept(23)
	prepared, err := m.PrepareRetractionHistory("fed", saved)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Release()
	saved.Producers[0].Accepted[0].Last = 9
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint64{7, 23} {
		if _, err := admission.ReserveRetractionID(context.Background(), "fed", 1, id); !errors.Is(err, core.ErrRetractionIDAlreadyUsed) {
			t.Fatalf("history lost ID %d: %v", id, err)
		}
	}
	accept(3)
	accept(8)
	live, err := m.ExportRetractionHistory("fed")
	if err != nil {
		t.Fatal(err)
	}
	want := []RetractionIDRange{{First: 3, Last: 3}, {First: 7, Last: 8}, {First: 23, Last: 23}}
	if !reflect.DeepEqual(live.Producers[0].Accepted, want) {
		t.Fatalf("ranges %+v", live.Producers)
	}
	if next, err := m.PreviewNextRetractionHandle("fed", &RetractionHistory{Complete: true}, 1); err != nil || next != 24 {
		t.Fatalf("next = %d, %v", next, err)
	}
}

func TestRetractionHistorySavedOnlyIDsAndGenerationFence(t *testing.T) {
	m, admission := retractionManager(t)
	history := &RetractionHistory{Complete: true, Producers: []RetractionProducerHistory{{Producer: 1, Accepted: []RetractionIDRange{{First: 7, Last: 9}}}}}
	prepared, err := m.PrepareRetractionHistory("fed", history)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	prepared.Release()
	if _, err := admission.ReserveRetractionID(context.Background(), "fed", 1, 8); !errors.Is(err, core.ErrRetractionIDAlreadyUsed) {
		t.Fatalf("saved-only ID not installed: %v", err)
	}
	stale, err := m.PrepareRetractionHistory("fed", history)
	if err != nil {
		t.Fatal(err)
	}
	m.OnFederationDestroyed("fed")
	if err := stale.Commit(); !errors.Is(err, core.ErrRetractionHistoryUnavailable) {
		t.Fatalf("stale prepared import: %v", err)
	}
	stale.Release()
	fresh, err := admission.ReserveRetractionID(context.Background(), "fed", 1, 8)
	if err != nil {
		t.Fatal(err)
	}
	fresh.Release()
}

func TestRetractionHistoryRejectsIncompletePendingAndOverflow(t *testing.T) {
	m, admission := retractionManager(t)
	for _, history := range []*RetractionHistory{nil, {}, {Complete: false}} {
		if _, err := m.PrepareRetractionHistory("fed", history); !errors.Is(err, core.ErrRetractionHistoryUnavailable) {
			t.Fatalf("legacy history accepted: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := admission.ReserveRetractionID(ctx, "fed", 1, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	pending, err := admission.ReserveRetractionID(context.Background(), "fed", 1, math.MaxUint64)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ExportRetractionHistory("fed"); !errors.Is(err, core.ErrRetractionHistoryUnavailable) {
		t.Fatalf("pending snapshot accepted: %v", err)
	}
	if _, err := m.PrepareRetractionHistory("fed", &RetractionHistory{Complete: true}); !errors.Is(err, core.ErrRetractionHistoryUnavailable) {
		t.Fatalf("pending import accepted: %v", err)
	}
	if err := pending.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PreviewNextRetractionHandle("fed", &RetractionHistory{Complete: true}, 1); !errors.Is(err, core.ErrRetractionIDExhausted) {
		t.Fatalf("overflow hint: %v", err)
	}
	gap, err := admission.ReserveRetractionID(context.Background(), "fed", 1, 1)
	if err != nil {
		t.Fatalf("unique gap after maximum: %v", err)
	}
	gap.Release()
}
