package grpc

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type currentCheckpointTestEvent struct{ wire *rtiv1.FederateEvent }

func (e currentCheckpointTestEvent) Seq() uint64                 { return e.wire.GetSeq() }
func (e currentCheckpointTestEvent) Inner() *rtiv1.FederateEvent { return e.wire }

type currentCheckpointTestRecipient struct {
	fed core.FederationName
	h   core.FederateHandle
}

type currentCheckpointTestObservation struct {
	recipient currentCheckpointTestRecipient
	seq       uint64
	current   bool
}

type currentCheckpointTestFixture struct {
	ctx      context.Context
	receipts *CallbackReceipts
	out      *MandatoryCallbackOutbox
	queues   map[currentCheckpointTestRecipient]<-chan []core.OutboundEvent
	observed []currentCheckpointTestObservation
}

func newCurrentCheckpointTestFixture(t *testing.T, capacity int) *currentCheckpointTestFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	f := &currentCheckpointTestFixture{ctx: ctx, queues: make(map[currentCheckpointTestRecipient]<-chan []core.OutboundEvent)}
	var err error
	f.receipts, err = NewCallbackReceipts(CallbackReceiptOptions{
		MaxPending:   capacity,
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			return nil
		},
		ObserveEntry: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent) error {
			f.observed = append(f.observed, currentCheckpointTestObservation{currentCheckpointTestRecipient{fed, h}, event.GetSeq(), CurrentCheckpointControlEntry(ctx)})
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := delivery.NewWithBatch(8, 1, 0)
	for _, recipient := range []currentCheckpointTestRecipient{{"f", 1}, {"f", 2}, {"other", 1}} {
		raw.Bind(recipient.fed, recipient.h)
		queue, release, err := raw.Subscribe(ctx, recipient.fed, recipient.h)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		f.queues[recipient] = queue
	}
	t.Cleanup(func() { raw.UnbindFederation("f"); raw.UnbindFederation("other") })
	f.out = NewMandatoryCallbackOutbox(raw, func() *CallbackReceipts { return f.receipts })
	return f
}

func currentCheckpointTestControl(kind string, seq uint64) *rtiv1.FederateEvent {
	event := &rtiv1.FederateEvent{Seq: seq}
	switch kind {
	case "save":
		event.Event = &rtiv1.FederateEvent_SaveInitiate{SaveInitiate: &rtiv1.InitiateFederateSave{Label: "same-label"}}
	case "restore-begun":
		event.Event = &rtiv1.FederateEvent_RestoreBegun{RestoreBegun: &rtiv1.FederationRestoreBegun{}}
	case "restore-initiate":
		event.Event = &rtiv1.FederateEvent_RestoreInitiate{RestoreInitiate: &rtiv1.InitiateFederateRestore{Label: "same-label", FederateHandle: 1}}
	default:
		panic("unknown checkpoint control kind")
	}
	return event
}

func (f *currentCheckpointTestFixture) publish(t *testing.T, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent) []byte {
	t.Helper()
	before := len(f.observed)
	if err := f.out.Send(f.ctx, fed, h, currentCheckpointTestEvent{event}); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-f.queues[currentCheckpointTestRecipient{fed, h}]:
		if len(batch) != 1 {
			t.Fatalf("callback batch length=%d want=1", len(batch))
		}
		wire, err := toDispatchFederateEvent(batch[0])
		if err != nil || wire.GetSeq() != event.GetSeq() || len(wire.GetCallbackReceipt()) != 32 {
			t.Fatalf("published callback=%v err=%v", wire, err)
		}
		if len(f.observed) != before {
			t.Fatal("queue publication invoked the entry observer")
		}
		return wire.GetCallbackReceipt()
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
		return nil
	}
}

func (f *currentCheckpointTestFixture) enter(t *testing.T, fed core.FederationName, h core.FederateHandle, ticket []byte, seq uint64, current bool) {
	t.Helper()
	before := len(f.observed)
	if err := f.receipts.ReportEntry(f.ctx, fed, h, ticket); err != nil {
		t.Fatal(err)
	}
	if len(f.observed) != before+1 {
		t.Fatalf("entry observations=%d want=%d", len(f.observed), before+1)
	}
	want := currentCheckpointTestObservation{currentCheckpointTestRecipient{fed, h}, seq, current}
	if got := f.observed[before]; got != want {
		t.Errorf("entry observation=%+v want=%+v", got, want)
	}
}

func TestCurrentCheckpointControlEntrySupersededAttempt(t *testing.T) {
	for _, oldKind := range []string{"save", "restore-begun", "restore-initiate"} {
		for _, newKind := range []string{"save", "restore-begun"} {
			for _, stage := range []string{checkpointStageQueued, callbackPhaseCompleted, checkpointStageEvicted} {
				t.Run(oldKind+"/by-"+newKind+"/"+stage, func(t *testing.T) {
					f := newCurrentCheckpointTestFixture(t, 8)
					f.receipts.opts.MaxCompleted = 1
					old := f.publish(t, "f", 1, currentCheckpointTestControl(oldKind, 1))
					newer := f.publish(t, "f", 1, currentCheckpointTestControl(newKind, 2))
					if stage != checkpointStageQueued {
						f.enter(t, "f", 1, newer, 2, true)
						if err := f.receipts.Report(f.ctx, "f", 1, newer, true, ""); err != nil {
							t.Fatal(err)
						}
					}
					if stage == checkpointStageEvicted {
						f.evictCompleted(t, newer)
					}
					f.enter(t, "f", 1, old, 1, false)
					if stage == checkpointStageQueued {
						f.enter(t, "f", 1, newer, 2, true)
					}
				})
			}
		}
	}
}

func (f *currentCheckpointTestFixture) evictCompleted(t *testing.T, ticket []byte) {
	t.Helper()
	other := f.publish(t, "other", 1, currentCheckpointTestControl("save", 999))
	f.enter(t, "other", 1, other, 999, true)
	if err := f.receipts.Report(f.ctx, "other", 1, other, true, ""); err != nil {
		t.Fatal(err)
	}
	f.receipts.mu.Lock()
	retained := f.receipts.entries[string(ticket)] != nil
	f.receipts.mu.Unlock()
	if retained {
		t.Fatal("completed receipt was not evicted")
	}
}

func currentCheckpointTestTerminal(kind string, seq uint64) *rtiv1.FederateEvent {
	event := &rtiv1.FederateEvent{Seq: seq}
	switch kind {
	case "save-completed":
		event.Event = &rtiv1.FederateEvent_SaveCompleted{SaveCompleted: &rtiv1.FederationSaved{Label: "same-label"}}
	case "save-failed":
		event.Event = &rtiv1.FederateEvent_SaveFailed{SaveFailed: &rtiv1.FederationNotSaved{Label: "same-label"}}
	case "restore-completed":
		event.Event = &rtiv1.FederateEvent_RestoreCompleted{RestoreCompleted: &rtiv1.FederationRestored{Label: "same-label"}}
	case "restore-failed":
		event.Event = &rtiv1.FederateEvent_RestoreFailed{RestoreFailed: &rtiv1.FederationNotRestored{Label: "same-label"}}
	default:
		panic("unknown checkpoint terminal kind")
	}
	return event
}

func currentCheckpointTestConfirmation(seq uint64) *rtiv1.FederateEvent {
	return &rtiv1.FederateEvent{Seq: seq, Event: &rtiv1.FederateEvent_SyncRegistrationSucceeded{SyncRegistrationSucceeded: &rtiv1.SyncRegistrationSucceeded{Label: "ready"}}}
}

func TestCurrentCheckpointTerminalSupersessionSurvivesCompletion(t *testing.T) {
	for _, kind := range []string{"save-completed", "save-failed", "restore-completed", "restore-failed"} {
		for _, stage := range []string{checkpointStageQueued, callbackPhaseCompleted, checkpointStageEvicted} {
			t.Run(kind+"/"+stage, func(t *testing.T) {
				f := newCurrentCheckpointTestFixture(t, 8)
				f.receipts.opts.MaxCompleted = 1
				f.publish(t, "f", 1, currentCheckpointTestTerminal(kind, 1))
				newer := f.publish(t, "f", 1, currentCheckpointTestControl("save", 2))
				if stage != checkpointStageQueued {
					f.enter(t, "f", 1, newer, 2, true)
					if err := f.receipts.Report(f.ctx, "f", 1, newer, true, ""); err != nil {
						t.Fatal(err)
					}
				}
				if stage == checkpointStageEvicted {
					f.evictCompleted(t, newer)
				}
				confirmation := f.publish(t, "f", 1, currentCheckpointTestConfirmation(3))
				// Only the receipt-layer terminal guard is under test here. The
				// server separately checks the newly active checkpoint manager.
				f.enter(t, "f", 1, confirmation, 3, false)
			})
		}
	}
}

func TestCurrentCheckpointTerminalUnpublishedReservationDoesNotBlock(t *testing.T) {
	for _, kind := range []string{"save-completed", "save-failed", "restore-completed", "restore-failed"} {
		t.Run(kind, func(t *testing.T) {
			f := newCurrentCheckpointTestFixture(t, 8)
			confirmation := f.publish(t, "f", 1, currentCheckpointTestConfirmation(1))
			reservation, err := f.out.Reserve(f.ctx, "f", []core.OutboxDelivery{{Recipient: 1, Event: currentCheckpointTestEvent{currentCheckpointTestTerminal(kind, 2)}}})
			if err != nil {
				t.Fatal(err)
			}
			defer reservation.Release()
			f.enter(t, "f", 1, confirmation, 1, false)
		})
	}
}

func TestCurrentCheckpointTerminalBlocksOnlyOwnerUntilEntry(t *testing.T) {
	for _, kind := range []string{"save-completed", "save-failed", "restore-completed", "restore-failed"} {
		t.Run(kind, func(t *testing.T) {
			f := newCurrentCheckpointTestFixture(t, 8)
			terminal := f.publish(t, "f", 1, currentCheckpointTestTerminal(kind, 1))
			confirmation := f.publish(t, "f", 1, currentCheckpointTestConfirmation(2))
			want := core.ErrSaveAlreadyInProgress
			if kind == "restore-completed" || kind == "restore-failed" {
				want = core.ErrRestoreAlreadyInProgress
			}
			if err := f.receipts.ReportEntry(f.ctx, "f", 1, confirmation); !errors.Is(err, want) {
				t.Fatalf("terminal guard=%v want=%v", err, want)
			}
			if len(f.observed) != 0 {
				t.Fatal("blocked confirmation reached observer")
			}
			unpublished, err := f.out.Reserve(f.ctx, "f", []core.OutboxDelivery{{Recipient: 1, Event: currentCheckpointTestEvent{currentCheckpointTestControl("save", 4)}}})
			if err != nil {
				t.Fatal(err)
			}
			defer unpublished.Release()
			if err := f.receipts.ReportEntry(f.ctx, "f", 1, confirmation); !errors.Is(err, want) {
				t.Fatalf("unpublished control bypassed the terminal guard: %v", err)
			}
			unpublished.Release()
			for _, peer := range []currentCheckpointTestRecipient{{"f", 2}, {"other", 1}} {
				ticket := f.publish(t, peer.fed, peer.h, currentCheckpointTestConfirmation(3))
				f.enter(t, peer.fed, peer.h, ticket, 3, false)
			}
			f.enter(t, "f", 1, terminal, 1, false)
			f.enter(t, "f", 1, confirmation, 2, false)
		})
	}
}

func TestCurrentCheckpointControlCommitFailurePreservesHistory(t *testing.T) {
	for _, kind := range []string{"save", "restore-begun"} {
		t.Run(kind, func(t *testing.T) {
			f := newCurrentCheckpointTestFixture(t, 8)
			old := f.publish(t, "f", 1, currentCheckpointTestControl("save", 1))
			failure := errors.New("queue commit failed")
			out := NewMandatoryCallbackOutbox(checkpointReviewQueue{commit: func() error { return failure }}, func() *CallbackReceipts { return f.receipts })
			if err := out.Send(f.ctx, "f", 1, currentCheckpointTestEvent{currentCheckpointTestControl(kind, 2)}); !errors.Is(err, failure) {
				t.Fatalf("commit error=%v", err)
			}
			f.enter(t, "f", 1, old, 1, true)
		})
	}
}

func TestCurrentCheckpointPublicationEntryWaitsForCommitOutcome(t *testing.T) {
	for _, failCommit := range []bool{false, true} {
		name := "success"
		if failCommit {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			f := newCurrentCheckpointTestFixture(t, 8)
			old := f.publish(t, "f", 1, currentCheckpointTestControl("save", 1))
			committing, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			failure := errors.New("queue commit failed")
			out := NewMandatoryCallbackOutbox(checkpointReviewQueue{commit: func() error {
				close(committing)
				<-release
				if failCommit {
					return failure
				}
				return nil
			}}, func() *CallbackReceipts { return f.receipts })
			reservation, err := out.Reserve(f.ctx, "f", []core.OutboxDelivery{{Recipient: 1, Event: currentCheckpointTestEvent{currentCheckpointTestControl("restore-begun", 2)}}})
			if err != nil {
				t.Fatal(err)
			}
			commitDone := make(chan error, 1)
			go func() { commitDone <- reservation.Commit() }()
			select {
			case <-committing:
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
			authenticated := make(chan struct{})
			f.receipts.opts.Authenticate = func(context.Context, core.FederationName, core.FederateHandle) error {
				close(authenticated)
				return nil
			}
			entryDone := make(chan error, 1)
			go func() { entryDone <- f.receipts.ReportEntry(f.ctx, "f", 1, old) }()
			select {
			case <-authenticated:
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
			select {
			case err := <-entryDone:
				unblock()
				<-commitDone
				t.Fatalf("entry observed an undecided queue commit: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			unblock()
			if err := <-commitDone; failCommit && !errors.Is(err, failure) || !failCommit && err != nil {
				t.Fatalf("commit result=%v", err)
			}
			if err := <-entryDone; err != nil {
				t.Fatal(err)
			}
			if len(f.observed) != 1 || f.observed[0].seq != 1 || f.observed[0].current != failCommit {
				t.Fatalf("post-commit entry observations=%+v", f.observed)
			}
		})
	}
}

type currentCheckpointDelayedStream struct {
	rtiv1.StreamService_EventsServer
	ctx  context.Context
	send func(*rtiv1.FederateEvent) error
}

func (s currentCheckpointDelayedStream) Context() context.Context              { return s.ctx }
func (s currentCheckpointDelayedStream) Send(event *rtiv1.FederateEvent) error { return s.send(event) }

func TestCurrentCheckpointLateStreamEntryRemainsSuperseded(t *testing.T) {
	for _, kind := range []string{"save", "restore-begun", "restore-initiate"} {
		for _, stage := range []string{checkpointStageQueued, callbackPhaseCompleted, checkpointStageEvicted} {
			t.Run(kind+"/"+stage, func(t *testing.T) {
				f := newCurrentCheckpointTestFixture(t, 8)
				f.receipts.opts.MaxCompleted = 1
				delivered, finish := make(chan []byte, 1), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(finish) }) }
				defer unblock()
				stream := receiptEventsStream{receipts: f.receipts, fed: "f", handle: 1, StreamService_EventsServer: currentCheckpointDelayedStream{ctx: f.ctx, send: func(event *rtiv1.FederateEvent) error {
					delivered <- bytes.Clone(event.GetCallbackReceipt())
					<-finish
					return nil
				}}}
				sent := make(chan error, 1)
				go func() { sent <- stream.Send(currentCheckpointTestControl(kind, 1)) }()
				var old []byte
				select {
				case old = <-delivered:
				case <-f.ctx.Done():
					t.Fatal(f.ctx.Err())
				}
				newer := f.publish(t, "f", 1, currentCheckpointTestControl("save", 2))
				if stage != checkpointStageQueued {
					f.enter(t, "f", 1, newer, 2, true)
					if err := f.receipts.Report(f.ctx, "f", 1, newer, true, ""); err != nil {
						t.Fatal(err)
					}
				}
				if stage == checkpointStageEvicted {
					f.evictCompleted(t, newer)
				}
				f.enter(t, "f", 1, old, 1, false)
				unblock()
				if err := <-sent; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestCurrentCheckpointLaterMandatoryPublicationUsesCommitOrder(t *testing.T) {
	f := newCurrentCheckpointTestFixture(t, 8)
	out := NewMandatoryCallbackOutbox(checkpointReviewQueue{commit: func() error { return nil }}, func() *CallbackReceipts { return f.receipts })
	prepared, err := out.Reserve(f.ctx, "f", []core.OutboxDelivery{{Recipient: 1, Event: currentCheckpointTestEvent{currentCheckpointTestControl("save", 1)}}})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Release()
	first := f.publish(t, "f", 1, currentCheckpointTestControl("restore-begun", 2))
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	last := prepared.(*mandatoryCallbackReservation).tickets[0]
	f.enter(t, "f", 1, first, 2, false)
	f.enter(t, "f", 1, last, 1, true)
}

func TestCurrentCheckpointControlEntryRecoveryIsNotCurrent(t *testing.T) {
	for _, kind := range []string{"save", "restore-begun", "restore-initiate"} {
		t.Run(kind, func(t *testing.T) {
			f := newCurrentCheckpointTestFixture(t, 8)
			live := f.publish(t, "f", 1, currentCheckpointTestControl("save", 1))
			recovered := currentCheckpointTestControl(kind, 2)
			recovered.CallbackRecovery = &rtiv1.CallbackRecovery{InvocationIdentity: bytes.Repeat([]byte{0x2a}, 32)}
			ticket := f.publish(t, "f", 1, recovered)
			f.enter(t, "f", 1, ticket, 2, false)
			f.enter(t, "f", 1, live, 1, true)
		})
	}
}

func TestCurrentCheckpointControlEntryRestoreBegunSurvivesInitiate(t *testing.T) {
	for _, completeInitiate := range []bool{false, true} {
		name := "queued-initiate"
		if completeInitiate {
			name = "completed-initiate"
		}
		t.Run(name, func(t *testing.T) {
			f := newCurrentCheckpointTestFixture(t, 8)
			begun := f.publish(t, "f", 1, currentCheckpointTestControl("restore-begun", 1))
			initiate := f.publish(t, "f", 1, currentCheckpointTestControl("restore-initiate", 2))
			if completeInitiate {
				f.enter(t, "f", 1, initiate, 2, true)
				if err := f.receipts.Report(f.ctx, "f", 1, initiate, true, ""); err != nil {
					t.Fatal(err)
				}
			}
			f.enter(t, "f", 1, begun, 1, true)
			if !completeInitiate {
				f.enter(t, "f", 1, initiate, 2, true)
			}
		})
	}
}

func TestCurrentCheckpointControlEntryFailedPreparationPreservesOld(t *testing.T) {
	for _, failure := range []string{"receipt-capacity", "recipient-unavailable", "unpublished-release"} {
		for _, newKind := range []string{"save", "restore-begun"} {
			t.Run(failure+"/"+newKind, func(t *testing.T) {
				capacity := 8
				if failure == "receipt-capacity" {
					capacity = 1
				}
				f := newCurrentCheckpointTestFixture(t, capacity)
				old := f.publish(t, "f", 1, currentCheckpointTestControl("save", 1))
				deliveries := []core.OutboxDelivery{{Recipient: 1, Event: currentCheckpointTestEvent{currentCheckpointTestControl(newKind, 2)}}}
				if failure == "recipient-unavailable" {
					deliveries = append(deliveries, core.OutboxDelivery{Recipient: 99, Event: currentCheckpointTestEvent{currentCheckpointTestControl(newKind, 3)}})
				}
				reservation, err := f.out.Reserve(f.ctx, "f", deliveries)
				if failure == "unpublished-release" {
					if err != nil {
						t.Fatal(err)
					}
					defer reservation.Release()
					// Entry may race a preparation that will be abandoned. No new
					// attempt is published while its queue reservation is held.
					f.enter(t, "f", 1, old, 1, true)
					reservation.Release()
				} else {
					if err == nil {
						reservation.Release()
						t.Fatal("new checkpoint preparation unexpectedly succeeded")
					}
					f.enter(t, "f", 1, old, 1, true)
				}
				f.receipts.mu.Lock()
				pending, entries := f.receipts.pending, len(f.receipts.entries)
				f.receipts.mu.Unlock()
				if pending != 1 || entries != 1 {
					t.Fatalf("failed preparation retained receipts: pending=%d entries=%d", pending, entries)
				}
			})
		}
	}
}

func TestCurrentCheckpointControlEntryIsRecipientScoped(t *testing.T) {
	for _, other := range []currentCheckpointTestRecipient{{"f", 2}, {"other", 1}} {
		t.Run(string(other.fed), func(t *testing.T) {
			f := newCurrentCheckpointTestFixture(t, 8)
			old := f.publish(t, "f", 1, currentCheckpointTestControl("save", 1))
			newer := f.publish(t, other.fed, other.h, currentCheckpointTestControl("restore-begun", 2))
			f.enter(t, "f", 1, old, 1, true)
			f.enter(t, other.fed, other.h, newer, 2, true)
		})
	}
}

const (
	checkpointStageQueued  = "queued"
	checkpointStageEvicted = "evicted"
)
