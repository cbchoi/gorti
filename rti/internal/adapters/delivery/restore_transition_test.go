package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

func TestRestoreOutboxReservationReleaseAndRepeatedEpoch(t *testing.T) {
	ctx := context.Background()
	m := NewWithBatch(2, 1, 0)
	ch, cancel, err := m.Subscribe(ctx, "f", 7)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestSubscription(t, cancel)
	old := &savepoint.RestoredEvent{}
	for i := 0; i < 2; i++ {
		if err := m.Send(ctx, "f", 7, old); err != nil {
			t.Fatal(err)
		}
	}
	terminal := &savepoint.RestoredEvent{FederateHandle: 1, CallbackEpoch: 1}
	r, epochs, err := m.PrepareRestore(ctx, "f", []RestoreDelivery{{CurrentHandle: 7, SavedHandle: 1, Event: terminal}})
	if err != nil || epochs[7] != 1 {
		t.Fatalf("prepare full queue: %v %+v", err, epochs)
	}
	r.Release()
	r.Release()
	for i := 0; i < 2; i++ {
		if batch := <-ch; len(batch) != 1 || batch[0] != old {
			t.Fatal("release changed old queue")
		}
	}
	if err := m.Send(ctx, "f", 7, old); err != nil {
		t.Fatal(err)
	}
	r, epochs, err = m.PrepareRestore(ctx, "f", []RestoreDelivery{{CurrentHandle: 7, SavedHandle: 1, Event: terminal}})
	if err != nil || epochs[7] != 1 {
		t.Fatalf("release consumed epoch: %v %+v", err, epochs)
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	r.Release()
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	if batch := <-ch; len(batch) != 1 || batch[0] != terminal {
		t.Fatal("old callbacks not purged")
	}
	if len(ch) != 0 {
		t.Fatal("duplicate commit republished terminal")
	}
	if !errors.Is(m.Send(ctx, "f", 7, old), core.ErrOutboxUnavailable) {
		t.Fatal("old handle accepted")
	}
	r, epochs, err = m.PrepareRestore(ctx, "f", []RestoreDelivery{{CurrentHandle: 1, SavedHandle: 1, Event: terminal}})
	if err != nil || epochs[1] != 2 {
		t.Fatalf("same-handle epoch did not advance: %v %+v", err, epochs)
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	<-ch
	if err := cancel(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(m.Send(ctx, "f", 1, old), core.ErrOutboxUnavailable) {
		t.Fatal("subscription cancel lost rekeyed state")
	}
}

func TestRestoreOutboxSwapsStreamsAndPreservesOtherFederations(t *testing.T) {
	ctx := context.Background()
	m := NewWithBatch(8, 1, 0)
	a, ca, _ := m.Subscribe(ctx, "f", 1)
	defer closeTestSubscription(t, ca)
	b, cb, _ := m.Subscribe(ctx, "f", 2)
	defer closeTestSubscription(t, cb)
	other, co, _ := m.Subscribe(ctx, "other", 1)
	defer closeTestSubscription(t, co)
	ea, eb, eo := &savepoint.RestoredEvent{}, &savepoint.RestoredEvent{}, &savepoint.RestoredEvent{}
	if err := m.Send(ctx, "other", 1, eo); err != nil {
		t.Fatal(err)
	}
	r, _, err := m.PrepareRestore(ctx, "f", []RestoreDelivery{{CurrentHandle: 1, SavedHandle: 2, Event: ea}, {CurrentHandle: 2, SavedHandle: 1, Event: eb}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	if (<-a)[0] != ea || (<-b)[0] != eb || (<-other)[0] != eo {
		t.Fatal("stream ownership changed")
	}
	if err := m.Send(ctx, "f", 2, eb); err != nil {
		t.Fatal(err)
	}
	if (<-a)[0] != eb {
		t.Fatal("saved key not mapped to name's live stream")
	}
}

func TestRestoreOutboxRejectsIncompleteOrDuplicateMappings(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name     string
		bindings []RestoreDelivery
	}{
		{"missing", []RestoreDelivery{{CurrentHandle: 1, SavedHandle: 3, Event: &savepoint.RestoredEvent{}}}},
		{"duplicate-target", []RestoreDelivery{{CurrentHandle: 1, SavedHandle: 3, Event: &savepoint.RestoredEvent{}}, {CurrentHandle: 2, SavedHandle: 3, Event: &savepoint.RestoredEvent{}}}},
		{"duplicate-source", []RestoreDelivery{{CurrentHandle: 1, SavedHandle: 3, Event: &savepoint.RestoredEvent{}}, {CurrentHandle: 1, SavedHandle: 4, Event: &savepoint.RestoredEvent{}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := NewWithBatch(8, 1, 0)
			m.Bind("f", 1)
			m.Bind("f", 2)
			r, _, err := m.PrepareRestore(ctx, "f", test.bindings)
			if err == nil {
				r.Release()
				t.Fatal("invalid transition admitted")
			}
			for _, h := range []core.FederateHandle{1, 2} {
				if err := m.Send(ctx, "f", h, &savepoint.RestoredEvent{}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
