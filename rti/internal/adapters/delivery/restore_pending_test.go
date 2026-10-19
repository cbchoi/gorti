package delivery

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

func TestRestorePendingBatchesTerminalFirstAtExactCapacity(t *testing.T) {
	ctx := context.Background()
	m := NewObservedWithBatch(4, 2, 0)
	ch, cancel, err := m.Subscribe(ctx, "f", 7)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestSubscription(t, cancel)
	old := &savepoint.RestoredEvent{}
	if err := m.Send(ctx, "f", 7, old); err != nil {
		t.Fatal(err)
	}
	terminal := &savepoint.RestoredEvent{FederateHandle: 1}
	a, b, c := &savepoint.RestoredEvent{}, &savepoint.RestoredEvent{}, &savepoint.RestoredEvent{}
	pending := []core.OutboundEvent{a, b, c}
	r, epochs, err := m.PrepareRestore(ctx, "f", []RestoreDelivery{{CurrentHandle: 7, SavedHandle: 1, Event: terminal, Pending: pending}})
	if err != nil || epochs[7] != 1 {
		t.Fatalf("prepare: epochs=%v err=%v", epochs, err)
	}
	pending[0] = old
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	r.Release()
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	want := []core.OutboundEvent{terminal, a, b, c}
	for i := 0; i < 2; i++ {
		batch := <-ch
		if len(batch) != 2 || batch[0] != want[2*i] || batch[1] != want[2*i+1] {
			t.Fatalf("batch %d: %v", i, batch)
		}
	}
	if len(ch) != 0 {
		t.Fatal("duplicate publication")
	}
	observation, err := m.ObserveQueue("f", 1)
	if err != nil {
		t.Fatal(err)
	}
	counts := observation.Inspect()
	if counts.Admitted != 4 || counts.Drained != 4 || counts.Scratch != 0 || counts.Queued != 0 {
		t.Fatalf("counts: %+v", counts)
	}
}

type pendingRejectGuard struct{ err error }

func (g pendingRejectGuard) Validate() error { return g.err }

func TestRestorePendingRejectsBeforeMutation(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("stale projection")
	for _, tc := range []struct {
		name     string
		terminal core.OutboundEvent
		pending  []core.OutboundEvent
		want     error
	}{
		{"overflow", &savepoint.RestoredEvent{}, []core.OutboundEvent{&savepoint.RestoredEvent{}, &savepoint.RestoredEvent{}}, core.ErrFederateOverflow},
		{"nil-pending", &savepoint.RestoredEvent{}, []core.OutboundEvent{nil}, nil},
		{"typed-nil-pending", &savepoint.RestoredEvent{}, []core.OutboundEvent{(*savepoint.RestoredEvent)(nil)}, nil},
		{"typed-nil-terminal", (*savepoint.RestoredEvent)(nil), nil, nil},
		{"guarded-nil-pending", &savepoint.RestoredEvent{}, []core.OutboundEvent{GuardEvent((*savepoint.RestoredEvent)(nil), pendingRejectGuard{})}, nil},
		{"guard-pending", &savepoint.RestoredEvent{}, []core.OutboundEvent{GuardEvent(&savepoint.RestoredEvent{}, pendingRejectGuard{failure})}, failure},
		{"guard-terminal", GuardEvent(&savepoint.RestoredEvent{}, pendingRejectGuard{failure}), nil, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewWithBatch(2, 1, 0)
			ch, cancel, err := m.Subscribe(ctx, "f", 7)
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestSubscription(t, cancel)
			old := &savepoint.RestoredEvent{}
			if err := m.Send(ctx, "f", 7, old); err != nil {
				t.Fatal(err)
			}
			r, _, err := m.PrepareRestore(ctx, "f", []RestoreDelivery{{CurrentHandle: 7, SavedHandle: 1, Event: tc.terminal, Pending: tc.pending}})
			if err == nil {
				r.Release()
				t.Fatal("invalid pending restore admitted")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error: %v", err)
			}
			if (<-ch)[0] != old {
				t.Fatal("failure changed old queue")
			}
			if epoch, err := m.CallbackEpoch("f", 7); err != nil || epoch != 0 {
				t.Fatalf("failure consumed epoch: %d %v", epoch, err)
			}
			if err := m.Send(ctx, "f", 7, old); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRestorePendingMultiRecipientFailureReleasesAll(t *testing.T) {
	ctx := context.Background()
	m := NewWithBatch(2, 2, 0)
	for _, h := range []core.FederateHandle{1, 2} {
		m.Bind("f", h)
	}
	r, _, err := m.PrepareRestore(ctx, "f", []RestoreDelivery{
		{CurrentHandle: 2, SavedHandle: 12, Event: &savepoint.RestoredEvent{}, Pending: []core.OutboundEvent{&savepoint.RestoredEvent{}, &savepoint.RestoredEvent{}}},
		{CurrentHandle: 1, SavedHandle: 11, Event: &savepoint.RestoredEvent{}, Pending: []core.OutboundEvent{&savepoint.RestoredEvent{}}},
	})
	if err == nil {
		r.Release()
		t.Fatal("second-recipient overflow admitted")
	}
	if !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{1, 2} {
		if err := m.Send(ctx, "f", h, &savepoint.RestoredEvent{}); err != nil {
			t.Fatal(err)
		}
		if epoch, err := m.CallbackEpoch("f", h); err != nil || epoch != 0 {
			t.Fatalf("recipient %d changed: %d %v", h, epoch, err)
		}
	}
}

func TestRestorePendingPreservesExactTypedMetadataAndEpochFloor(t *testing.T) {
	ctx := context.Background()
	realization, err := core.NewRegionRealizationSnapshot(7, []core.RegionDimensionRange{{Dimension: 1, Bounds: core.DDMRange{Lower: 1<<53 + 1, Upper: ^uint64(0)}}})
	if err != nil {
		t.Fatal(err)
	}
	metadata := &core.ObjectCallbackMetadata{Producer: 2, Regions: []core.DDMRegionHandleCore{7}, ConveyedRegions: []core.ConveyedRegionCopy{{Designator: 8, Realization: realization}}, AttributeConveyedRegions: map[core.AttributeHandle][]core.ConveyedRegionCopy{1: {{Designator: 8, Realization: realization}}}}
	integer, floating := int64(1<<53+1), core.LogicalTime(5.125)
	a := &object.CallbackEventOf[int64]{Kind: object.CallbackReceive, Sequence: 33, InteractionClass: 1, LogicalTime: &integer, UserSuppliedTag: []byte{0, 255}, Parameters: map[uint64][]byte{1: {0, 255}}, Metadata: metadata}
	b := &object.CallbackEvent{Kind: object.CallbackReflect, Sequence: 33, Object: 1, LogicalTime: &floating, Attributes: map[uint64][]byte{1: {255, 0}}, Metadata: metadata}
	// Delivery is intentionally opaque: parent snapshot/receipt adapters own
	// scalar and carrier validation, and no conversion occurs here.
	m := NewWithBatch(4, 4, 0)
	ch, cancel, err := m.Subscribe(ctx, "f", 7)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestSubscription(t, cancel)
	terminal := &savepoint.RestoredEvent{}
	r, epochs, err := m.PrepareRestoreWithEpochFloor(ctx, "f", []RestoreDelivery{{CurrentHandle: 7, SavedHandle: 3, Event: terminal, Pending: []core.OutboundEvent{a, b}}}, map[core.FederateHandle]uint64{7: 19})
	if err != nil || epochs[7] != 20 {
		t.Fatalf("prepare floor: %v %v", epochs, err)
	}
	if len(ch) != 0 {
		t.Fatal("prepare published callbacks before ticket admission")
	}
	terminal.CallbackEpoch = epochs[7]
	epochs[7] = 1
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	batch := <-ch
	if !reflect.DeepEqual(batch, []core.OutboundEvent{terminal, a, b}) {
		t.Fatal("typed metadata or ordering changed")
	}
	if batch[1] != a || batch[2] != b || *a.LogicalTime != 1<<53+1 || *b.LogicalTime != 5.125 {
		t.Fatal("opaque callbacks converted")
	}
	if epoch, err := m.CallbackEpoch("f", 3); err != nil || epoch != 20 {
		t.Fatalf("installed epoch: %d %v", epoch, err)
	}
	if len(ch) != 0 {
		t.Fatal("partial final batch retained")
	}
}

func TestRestorePendingConcurrentReaderMaintainsTerminalBoundary(t *testing.T) {
	ctx := context.Background()
	m := NewObservedWithBatch(32, 4, 0)
	ch, cancel, err := m.Subscribe(ctx, "f", 7)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestSubscription(t, cancel)
	for round := 0; round < 30; round++ {
		old := &fakeOutboundEvent{seq: 1}
		for i := 0; i < 32; i++ {
			if err := m.Send(ctx, "f", 7, old); err != nil {
				t.Fatal(err)
			}
		}
		oldObservation, err := m.ObserveQueue("f", 7)
		if err != nil {
			t.Fatal(err)
		}
		terminal := &fakeOutboundEvent{seq: 2}
		pending := make([]core.OutboundEvent, 31)
		for i := range pending {
			pending[i] = &fakeOutboundEvent{seq: uint64(i + 3)}
		}
		r, _, err := m.PrepareRestore(ctx, "f", []RestoreDelivery{{CurrentHandle: 7, SavedHandle: 7, Event: terminal, Pending: pending}})
		if err != nil {
			t.Fatal(err)
		}
		result := make(chan []core.OutboundEvent, 1)
		go func() {
			var restored []core.OutboundEvent
			for len(restored) < 32 {
				for _, event := range <-ch {
					if len(restored) != 0 || event == terminal {
						restored = append(restored, event)
					}
				}
				runtime.Gosched()
			}
			result <- restored
		}()
		if err := r.Commit(); err != nil {
			t.Fatal(err)
		}
		want := append([]core.OutboundEvent{terminal}, pending...)
		if got := <-result; !reflect.DeepEqual(got, want) {
			t.Fatal("reader observed old callback after terminal or reordered pending")
		}
		counts := oldObservation.Inspect()
		if !counts.Closed || counts.Admitted != counts.Drained+counts.Invalidated {
			t.Fatalf("old epoch conservation: %+v", counts)
		}
		observation, err := m.ObserveQueue("f", 7)
		if err != nil {
			t.Fatal(err)
		}
		inspectQueue(t, observation, 32, 32, 0, 0, 0, false)
	}
}

func TestRestorePendingConcurrentCommitIsIdempotent(t *testing.T) {
	m := NewWithBatch(2, 2, 0)
	ch, cancel, err := m.Subscribe(context.Background(), "f", 7)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestSubscription(t, cancel)
	r, _, err := m.PrepareRestore(context.Background(), "f", []RestoreDelivery{{CurrentHandle: 7, SavedHandle: 1, Event: &savepoint.RestoredEvent{}, Pending: []core.OutboundEvent{&savepoint.RestoredEvent{}}}})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.Commit(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(ch) != 1 || len(<-ch) != 2 {
		t.Fatal("concurrent commit duplicated pending callbacks")
	}
}

func TestRestorePendingReleasePreservesOldQueue(t *testing.T) {
	ctx := context.Background()
	m := NewWithBatch(2, 2, 0)
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
	r, _, err := m.PrepareRestore(ctx, "f", []RestoreDelivery{{CurrentHandle: 7, SavedHandle: 1, Event: &savepoint.RestoredEvent{}, Pending: []core.OutboundEvent{&savepoint.RestoredEvent{}}}})
	if err != nil {
		t.Fatal(err)
	}
	r.Release()
	r.Release()
	batch := <-ch
	if len(batch) != 2 || batch[0] != old || batch[1] != old {
		t.Fatal("release changed queue")
	}
	if epoch, err := m.CallbackEpoch("f", 7); err != nil || epoch != 0 {
		t.Fatalf("release consumed epoch: %d %v", epoch, err)
	}
}
