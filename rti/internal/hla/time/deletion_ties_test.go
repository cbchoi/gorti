package time

import (
	"context"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type deletionTieEvent struct {
	seq        uint64
	object     core.ObjectHandle
	reflection bool
	deletion   bool
}

func (e *deletionTieEvent) Seq() uint64 { return e.seq }
func (e *deletionTieEvent) ReflectionObject() core.ObjectHandle {
	if e.reflection {
		return e.object
	}
	return 0
}
func (e *deletionTieEvent) DeletionObject() core.ObjectHandle {
	if e.deletion {
		return e.object
	}
	return 0
}

func TestObjectDeletionTiesPreserveUnrelatedQueueSlots(t *testing.T) {
	t.Run("Float64", func(t *testing.T) { checkObjectDeletionTies(t, float64(10)) })
	t.Run("Integer64", func(t *testing.T) { checkObjectDeletionTies(t, int64(1<<53+17)) })
}

func checkObjectDeletionTies[T core.LogicalTimeScalar](t *testing.T, timestamp T) {
	type entry struct {
		event *deletionTieEvent
		delta T
	}
	for _, tc := range []struct {
		name        string
		entries     []entry
		want        []uint64
		constrained bool
	}{
		{"matching-object-only", []entry{
			{&deletionTieEvent{seq: 1, object: 1, deletion: true}, 0},
			{&deletionTieEvent{seq: 2}, 0},
			{&deletionTieEvent{seq: 3, object: 2, reflection: true}, 0},
			{&deletionTieEvent{seq: 4, object: 1, reflection: true}, 0},
			{&deletionTieEvent{seq: 5}, 0},
			{&deletionTieEvent{seq: 6, object: 2, deletion: true}, 0},
			{&deletionTieEvent{seq: 7, object: 1, reflection: true}, 0},
		}, []uint64{4, 2, 3, 7, 5, 6, 1}, true},
		{"two-objects", []entry{
			{&deletionTieEvent{seq: 1, object: 1, deletion: true}, 0},
			{&deletionTieEvent{seq: 2, object: 2, deletion: true}, 0},
			{&deletionTieEvent{seq: 3}, 0},
			{&deletionTieEvent{seq: 4, object: 1, reflection: true}, 0},
			{&deletionTieEvent{seq: 5, object: 2, reflection: true}, 0},
		}, []uint64{4, 5, 3, 1, 2}, true},
		{"different-timestamps", []entry{
			{&deletionTieEvent{seq: 1, object: 1, deletion: true}, 0},
			{&deletionTieEvent{seq: 2, object: 1, reflection: true}, 1},
		}, []uint64{1, 2}, true},
		{"already-ordered", []entry{
			{&deletionTieEvent{seq: 1, object: 1, reflection: true}, 0},
			{&deletionTieEvent{seq: 2}, 0},
			{&deletionTieEvent{seq: 3, object: 1, deletion: true}, 0},
		}, []uint64{1, 2, 3}, true},
		{"no-matching-reflection", []entry{
			{&deletionTieEvent{seq: 1, object: 1, deletion: true}, 0},
			{&deletionTieEvent{seq: 2, object: 2, reflection: true}, 0},
			{&deletionTieEvent{seq: 3}, 0},
		}, []uint64{1, 2, 3}, true},
		{"receive-order-unchanged", []entry{
			{&deletionTieEvent{seq: 1, object: 1, deletion: true}, 0},
			{&deletionTieEvent{seq: 2}, 0},
			{&deletionTieEvent{seq: 3, object: 1, reflection: true}, 0},
		}, []uint64{1, 2, 3}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, err := NewOf[T](OptionsOf[T]{Clock: core.NewManualClock(zeroTime()), Outbox: nopOutbox{}})
			if err != nil {
				t.Fatal(err)
			}
			if err := mgr.EnableConstrained(context.Background(), "fed", 2); err != nil {
				t.Fatal(err)
			}
			var deliveries []core.MessageDeliveryOf[T]
			for _, entry := range tc.entries {
				deliveries = append(deliveries, core.MessageDeliveryOf[T]{Recipient: 2, Timestamp: timestamp + entry.delta, Event: entry.event, Sender: 1, RetractionHandle: entry.event.seq})
			}
			reservation := mgr.ReserveTSO("fed", deliveries)
			reservation.Commit(context.Background())
			if !tc.constrained {
				mgr.states.mu.Lock()
				mgr.states.getLocked("fed", 2).constrained = false
				mgr.states.mu.Unlock()
			}
			for attempt := 0; attempt < 2; attempt++ {
				released := mgr.takeBufferedTSO("fed", 2, nil, true)
				var got []uint64
				for _, item := range released {
					got = append(got, item.event.Seq())
					original := tc.entries[item.event.Seq()-1]
					if item.event != original.event || item.timestamp != timestamp+original.delta || item.sender != 1 || item.retractionHandle != item.event.Seq() {
						t.Fatal("tie ordering lost or changed the message envelope")
					}
				}
				if !slices.Equal(got, tc.want) {
					t.Fatalf("attempt %d: queue order=%v, want %v", attempt, got, tc.want)
				}
				mgr.restoreBufferedTSO("fed", 2, released)
			}
		})
	}
}
