package grpc

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

func completedMarker(sent, retry bool) *callbackReceipt {
	return &callbackReceipt{applied: true, sent: sent, retry: retry,
		event: &rtiv1.FederateEvent{CallbackRecovery: &rtiv1.CallbackRecovery{Completed: true}}}
}

func assertCompletedVacanciesCleared(t *testing.T, backing []string, live []string) {
	t.Helper()
	active := make(map[*string]bool)
	for i := range live {
		active[&live[i]] = true
	}
	for i, key := range backing {
		if !active[&backing[i]] && key != "" {
			t.Fatalf("retired backing slot %d retains %q", i, key)
		}
	}
}

func TestCallbackCompletedEvictionOrderAndReleasedReferences(t *testing.T) {
	for _, victim := range []int{0, 1, 3, 4} {
		t.Run(fmt.Sprint(victim), func(t *testing.T) {
			r := &CallbackReceipts{opts: CallbackReceiptOptions{MaxCompleted: 4}, entries: make(map[string]*callbackReceipt)}
			r.completed = []string{"a", "b", "c", "d", "e"}
			before := slices.Clone(r.completed)
			backing := r.completed[:cap(r.completed)]
			for i, key := range r.completed {
				r.entries[key] = &callbackReceipt{applied: true}
				if i < victim {
					r.entries[key] = completedMarker(false, false)
				}
			}
			r.trimCompletedLocked()
			want := append(slices.Clone(before[:victim]), before[victim+1:]...)
			if !slices.Equal(r.completed, want) || len(r.entries) != 4 || r.entries[before[victim]] != nil {
				t.Fatalf("completed=%v entries=%v want=%v", r.completed, r.entries, want)
			}
			assertCompletedVacanciesCleared(t, backing, r.completed)
		})
	}
}

func TestCallbackCompletedPruneClearsDetachedKeys(t *testing.T) {
	r := &CallbackReceipts{completed: []string{"old", "a", "removed", "b"}, entries: map[string]*callbackReceipt{"a": {}, "b": {}}}
	backing := r.completed[:cap(r.completed)]
	r.pruneCompletedLocked()
	if !slices.Equal(r.completed, []string{"a", "b"}) {
		t.Fatal(r.completed)
	}
	assertCompletedVacanciesCleared(t, backing, r.completed)
	clear(r.entries)
	r.pruneCompletedLocked()
	assertCompletedVacanciesCleared(t, backing, r.completed)
}

func TestCallbackCompletedQueueMatchesEvictionPolicy(t *testing.T) {
	rng := rand.New(rand.NewSource(1516))
	r := &CallbackReceipts{opts: CallbackReceiptOptions{MaxCompleted: 32}, entries: make(map[string]*callbackReceipt)}
	var order []string
	pinned := make(map[string]bool)
	for step := 0; step < 20000; step++ {
		if len(order) != 0 && rng.Intn(4) == 0 {
			key := order[rng.Intn(len(order))]
			switch rng.Intn(3) {
			case 0:
				delete(r.entries, key)
				delete(pinned, key)
				order = slices.DeleteFunc(order, func(s string) bool { return s == key })
				r.pruneCompletedLocked()
			case 1:
				r.entries[key] = completedMarker(true, false)
				pinned[key] = false
			case 2:
				r.entries[key] = completedMarker(true, true)
				pinned[key] = true
			}
		}
		key := fmt.Sprint(step)
		pin := rng.Intn(8) == 0
		if pin {
			r.entries[key] = completedMarker(false, false)
		} else {
			r.entries[key] = &callbackReceipt{applied: true}
		}
		r.completed = append(r.completed, key)
		order = append(order, key)
		pinned[key] = pin
		for len(order) > r.opts.MaxCompleted {
			victim := slices.IndexFunc(order, func(s string) bool { return !pinned[s] })
			if victim < 0 {
				break
			}
			delete(pinned, order[victim])
			order = slices.Delete(order, victim, victim+1)
		}
		r.trimCompletedLocked()
		if !slices.Equal(r.completed, order) || len(r.entries) != len(order) {
			t.Fatalf("step %d: completed=%v want=%v", step, r.completed, order)
		}
		for _, key := range order {
			if r.entries[key] == nil {
				t.Fatalf("step %d: retained key missing", step)
			}
		}
	}
}

func TestCallbackCompletedEvictionKeepsDuplicateOutcomeSemantics(t *testing.T) {
	r := controlRecoveryExtraLedger(t, 2, nil)
	ctx := context.Background()
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_ReservationSucceeded{ReservationSucceeded: &rtiv1.ObjectInstanceNameReservationSucceeded{ObjectName: "example"}}}
	var tickets [][]byte
	for i := 0; i < 4; i++ {
		ticket, sent, err := r.PrepareRequired(ctx, "f", 1, event)
		if err != nil {
			t.Fatal(err)
		}
		sent(true)
		if err := r.ReportEntry(ctx, "f", 1, ticket); err != nil {
			t.Fatal(err)
		}
		if err := r.Report(ctx, "f", 1, ticket, true, ""); err != nil {
			t.Fatal(err)
		}
		tickets = append(tickets, ticket)
	}
	for i, ticket := range tickets {
		err := r.Report(ctx, "f", 1, ticket, true, "")
		if (i < 2) != (err != nil) {
			t.Fatalf("ticket %d eviction status: %v", i, err)
		}
		if i >= 2 && r.Report(ctx, "f", 1, ticket, false, "different") == nil {
			t.Fatal("conflicting duplicate accepted")
		}
	}
	if !reflect.DeepEqual(r.completed, []string{string(tickets[2]), string(tickets[3])}) {
		t.Fatal("retention order changed")
	}
}

func BenchmarkCallbackCompletedRetention(b *testing.B) {
	for _, limit := range []int{256, 8192} {
		for _, pins := range []int{0, 1, 8} {
			b.Run(fmt.Sprintf("limit_%d/pinned_%d", limit, pins), func(b *testing.B) {
				r := &CallbackReceipts{opts: CallbackReceiptOptions{MaxCompleted: limit}, entries: make(map[string]*callbackReceipt, limit+1)}
				keys := make([]string, 2*limit)
				ordinary := &callbackReceipt{applied: true}
				for i := range keys {
					keys[i] = fmt.Sprintf("receipt-%024d", i)
				}
				for i := 0; i < pins; i++ {
					key := fmt.Sprint("pinned-", i)
					r.entries[key] = completedMarker(false, false)
					r.completed = append(r.completed, key)
				}
				for i := 0; i < limit-pins; i++ {
					r.entries[keys[i]] = ordinary
					r.completed = append(r.completed, keys[i])
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					key := keys[(i+limit-pins)%len(keys)]
					r.entries[key] = ordinary
					r.completed = append(r.completed, key)
					r.trimCompletedLocked()
				}
				b.StopTimer()
				if len(r.completed) != limit || len(r.entries) != limit {
					b.Fatal("invalid retention state")
				}
			})
		}
	}
}
