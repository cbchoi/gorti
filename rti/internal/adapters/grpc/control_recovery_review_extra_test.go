package grpc

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

func controlRecoveryExtraLedger(t *testing.T, completed int, pending func(core.FederationName, core.FederateHandle, int)) *CallbackReceipts {
	t.Helper()
	r, err := NewCallbackReceipts(CallbackReceiptOptions{
		MaxPending: 4, MaxCompleted: completed, Pending: pending,
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func controlRecoveryExtraMarkers(t *testing.T, r *CallbackReceipts, fed core.FederationName, values ...byte) []*rtiv1.FederateEvent {
	t.Helper()
	data := controlRecoveryExtraImage(t, values...)
	pending, reservation, err := r.PrepareCheckpointReplay(context.Background(), fed, data, []core.RestoreParticipant{{CurrentHandle: 1, SavedHandle: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Release()
	reservation.Admit()
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	r.restoreCommitted(fed, "saved", map[core.FederateHandle]core.FederateHandle{1: 1})
	var result []*rtiv1.FederateEvent
	for _, event := range pending[1] {
		wire, err := EncodeOutboundEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, wire)
	}
	return result
}

func controlRecoveryExtraImage(t *testing.T, values ...byte) []byte {
	t.Helper()
	image := callbackCheckpointImage{Version: 2}
	for i, value := range values {
		image.Deliveries = append(image.Deliveries, callbackCheckpointDelivery{Recipient: 1, Order: uint64(i + 1), Identity: hex.EncodeToString(bytes.Repeat([]byte{value}, 32)), Phase: callbackPhaseCompleted})
	}
	data, err := json.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestControlRecoveryExtraOtherFederationCapacityAndRollback(t *testing.T) {
	r := controlRecoveryExtraLedger(t, 2, nil)
	marker := controlRecoveryExtraMarkers(t, r, "alpha", 0xaa)[0]
	participants := []core.RestoreParticipant{{CurrentHandle: 1, SavedHandle: 1}}
	_, reservation, err := r.PrepareCheckpointReplay(context.Background(), "beta", controlRecoveryExtraImage(t, 0xbb), participants)
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Release()
	_, rejected, err := r.PrepareCheckpointReplay(context.Background(), "gamma", controlRecoveryExtraImage(t, 0xcc), participants)
	if err == nil {
		rejected.Release()
		t.Fatal("other-federation live marker plus reservation did not consume capacity")
	}
	r.mu.Lock()
	entries, pending := len(r.entries), r.pending
	r.mu.Unlock()
	if entries != 2 || pending != 0 {
		t.Fatalf("failed capacity admission leaked state: entries=%d pending=%d", entries, pending)
	}
	reservation.Release()
	reservation.Release()
	r.mu.Lock()
	entries, pending = len(r.entries), r.pending
	r.mu.Unlock()
	if entries != 1 || pending != 0 {
		t.Fatalf("rollback did not release exactly its reservation: entries=%d pending=%d", entries, pending)
	}
	if _, err := r.completedRecoveryDelivery("alpha", 1, marker); err != nil {
		t.Fatal("rollback damaged other-federation marker", err)
	}
	controlRecoveryExtraMarkers(t, r, "gamma", 0xcc)
}

func TestControlRecoveryExtraPartialReservationRollback(t *testing.T) {
	r := controlRecoveryExtraLedger(t, 3, nil)
	marker := controlRecoveryExtraMarkers(t, r, "alpha", 0xaa)[0]
	r.mu.Lock()
	r.nextCheckpointOrder = ^uint64(0) - 1
	r.mu.Unlock()
	_, reservation, err := r.PrepareCheckpointReplay(context.Background(), "beta", controlRecoveryExtraImage(t, 0xbb, 0xcc), []core.RestoreParticipant{{CurrentHandle: 1, SavedHandle: 1}})
	if err == nil {
		reservation.Release()
		t.Fatal("exhausted ordinal unexpectedly admitted replay")
	}
	r.mu.Lock()
	entries, pending, completed := len(r.entries), r.pending, len(r.completed)
	r.mu.Unlock()
	if entries != 1 || pending != 0 || completed != 1 {
		t.Fatalf("partial reservation leaked state: entries=%d pending=%d completed=%d", entries, pending, completed)
	}
	if _, err := r.completedRecoveryDelivery("alpha", 1, marker); err != nil {
		t.Fatal("partial rollback damaged existing marker", err)
	}
}

func TestControlRecoveryExtraDisconnectCannotOverflowPinnedCapacity(t *testing.T) {
	r := controlRecoveryExtraLedger(t, 1, nil)
	marker := controlRecoveryExtraMarkers(t, r, "alpha", 0xaa)[0]
	sent, err := r.completedRecoveryDelivery("alpha", 1, marker)
	if err != nil {
		t.Fatal(err)
	}
	sent(true)
	_, reservation, err := r.PrepareCheckpointReplay(context.Background(), "beta", controlRecoveryExtraImage(t, 0xbb), []core.RestoreParticipant{{CurrentHandle: 1, SavedHandle: 1}})
	if err != nil {
		return // Rejecting replay before publication is a valid bounded policy.
	}
	defer reservation.Release()
	r.markDisconnected("alpha", 1)
	reservation.Admit()
	if err := reservation.Commit(); err != nil {
		return
	}
	r.restoreCommitted("beta", "saved", map[core.FederateHandle]core.FederateHandle{1: 1})
	r.mu.Lock()
	completed, pinned := len(r.completed), r.pinnedCompletedOtherLocked("")
	r.mu.Unlock()
	if completed > 1 || pinned > 1 {
		t.Fatalf("disconnect after replay preparation overflowed retention: completed=%d pinned=%d limit=1", completed, pinned)
	}
}

func TestControlRecoveryExtraQueuedMarkerSurvivesOtherRecipientCompletion(t *testing.T) {
	r := controlRecoveryExtraLedger(t, 1, nil)
	marker := controlRecoveryExtraMarkers(t, r, "f", 0xaa)[0]
	// A different recipient completes while recipient 1's marker is still queued.
	event := &rtiv1.FederateEvent{Seq: 2, Event: &rtiv1.FederateEvent_ReservationSucceeded{ReservationSucceeded: &rtiv1.ObjectInstanceNameReservationSucceeded{ObjectName: "other"}}}
	ticket, sent, err := r.PrepareRequired(context.Background(), "f", 2, event)
	if err != nil {
		t.Fatal(err)
	}
	sent(true)
	if err := r.ReportEntry(context.Background(), "f", 2, ticket); err != nil {
		t.Fatal(err)
	}
	if err := r.Report(context.Background(), "f", 2, ticket, true, ""); err != nil {
		t.Fatal(err)
	}
	commit, err := r.completedRecoveryDelivery("f", 1, marker)
	if err != nil {
		t.Fatal("ordinary completed-receipt eviction made an unsent queued reconciliation marker undeliverable", err)
	}
	commit(false)
	retry := r.pendingRedelivery("f", 1)
	if len(retry) != 1 || !retry[0].GetCallbackRecovery().GetCompleted() || len(retry[0].CallbackReceipt) != 0 {
		t.Fatal("queued marker lost retry retention", retry)
	}
}

func TestControlRecoveryExtraMarkerOrderingIsFederationScoped(t *testing.T) {
	r := controlRecoveryExtraLedger(t, 8, nil)
	markers := controlRecoveryExtraMarkers(t, r, "alpha", 0xaa, 0xbb)
	controlRecoveryExtraMarkers(t, r, "beta", 0xaa)
	r.markDisconnected("alpha", 1)
	// Map iteration must not let beta's equal marker body supply alpha's ordinal.
	for iteration := 0; iteration < 256; iteration++ {
		retry := r.pendingRedelivery("alpha", 1)
		if len(retry) != 2 || !bytes.Equal(retry[0].GetCallbackRecovery().InvocationIdentity, markers[0].GetCallbackRecovery().InvocationIdentity) || !bytes.Equal(retry[1].GetCallbackRecovery().InvocationIdentity, markers[1].GetCallbackRecovery().InvocationIdentity) {
			t.Fatalf("foreign marker changed recipient admission order on iteration %d: %v", iteration, retry)
		}
	}
}

func TestControlRecoveryExtraConcurrentCancellationAccountsOnce(t *testing.T) {
	var accounting atomic.Int64
	r := controlRecoveryExtraLedger(t, 8, func(_ core.FederationName, _ core.FederateHandle, delta int) { accounting.Add(int64(delta)) })
	raw := delivery.NewWithBatch(8, 1, 0)
	raw.Bind("f", 1)
	defer raw.UnbindFederation("f")
	queue, closeQueue, err := raw.Subscribe(context.Background(), "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer checkTransportCleanup(t, closeQueue)
	guard := &streamKnowledgeGuard{}
	event := delivery.GuardCancellableEvent(&ownership.CallbackEvent{Sequence: 1, Kind: ownership.CallbackAssumption, Object: 7, Attributes: []core.AttributeHandle{1}}, guard, object.ErrStaleObjectKnowledge)
	if err := NewMandatoryCallbackOutbox(raw, func() *CallbackReceipts { return r }).Send(context.Background(), "f", 1, event); err != nil {
		t.Fatal(err)
	}
	queued := (<-queue)[0]
	guard.stale.Store(true)
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, _ = toDispatchFederateEvent(queued)
			r.markDisconnected("f", 1)
			r.pendingRedelivery("f", 1)
			_, _ = (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return r }}).Marshal("f")
		}()
	}
	workers.Wait()
	r.mu.Lock()
	pending, entries := r.pending, len(r.entries)
	r.mu.Unlock()
	if pending != 0 || entries != 0 || accounting.Load() != 0 {
		t.Fatalf("cancellation accounting: pending=%d entries=%d net=%d", pending, entries, accounting.Load())
	}
}
