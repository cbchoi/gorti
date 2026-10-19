package federate

import (
	"context"
	"errors"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type hydrationClient struct {
	rtiv1.LocalLRCServiceClient
	get func(context.Context, *rtiv1.GetLocalValidationStateRequest) (*rtiv1.LocalValidationState, error)
}

func (c hydrationClient) GetLocalValidationState(ctx context.Context, req *rtiv1.GetLocalValidationStateRequest, _ ...grpc.CallOption) (*rtiv1.LocalValidationState, error) {
	return c.get(ctx, req)
}

func TestLocalLRCHydrationOrdersSnapshotBetweenCallbacks(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, replyFirst := range []bool{false, true} {
			name := "single"
			if batch {
				name = "batch"
			}
			if replyFirst {
				name += "/reply-first"
			} else {
				name += "/marker-first"
			}
			t.Run(name, func(t *testing.T) {
				f := newLocalLRCTestFederate(t, &recordingLocalLRCServer{ackEvery: 1}, 8)
				api, ok := any(f).(interface{ ReinitializeLocalLRC(context.Context) error })
				if !ok {
					t.Fatal("Go SDK cannot reinitialize LocalLRC from an ordered authoritative snapshot")
				}
				f.handles, _ = tablesFromFOMView(authoritativeView(1))
				f.restoreLocalDisabled.Store(true)
				f.eventCh = make(chan Event, 8)
				f.drainDone = make(chan struct{})
				f.localLRC = &localLRC{next: 2, acked: 1, changed: make(chan struct{}), stop: make(chan struct{}), cancel: func() {}}
				requests := make(chan *rtiv1.GetLocalValidationStateRequest, 1)
				reply := make(chan struct{})
				f.conn.localLRC = hydrationClient{LocalLRCServiceClient: f.conn.localLRC, get: func(ctx context.Context, req *rtiv1.GetLocalValidationStateRequest) (*rtiv1.LocalValidationState, error) {
					requests <- req
					select {
					case <-reply:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
					return &rtiv1.LocalValidationState{FederationGeneration: 9, FederateHandle: 7, SnapshotId: req.SnapshotId, FomView: authoritativeView(1), Publications: []*rtiv1.PublishedObjectClass{{ObjectClassHandle: 19, AttributeHandles: []uint64{2}}}, PublishedInteractionClasses: []uint64{27}, Objects: []*rtiv1.KnownObjectState{{ObjectHandle: 80, ObjectClassHandle: 19, OwnedAttributeHandles: []uint64{7}}, {ObjectHandle: 90, ObjectClassHandle: 19}}}, nil
				}}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- api.ReinitializeLocalLRC(ctx) }()
				req := <-requests
				if replyFirst {
					close(reply)
					select {
					case err := <-done:
						t.Fatalf("installed before barrier: %v", err)
					case <-time.After(20 * time.Millisecond):
					}
				}
				events := []*rtiv1.FederateEvent{
					{Event: &rtiv1.FederateEvent_Remove{Remove: &rtiv1.RemoveObjectInstance{ObjectHandle: 80}}},
					{Event: &rtiv1.FederateEvent_LocalValidationBarrier{LocalValidationBarrier: &rtiv1.LocalValidationBarrier{SnapshotId: req.SnapshotId, FederateHandle: 7}}},
					{Event: &rtiv1.FederateEvent_Remove{Remove: &rtiv1.RemoveObjectInstance{ObjectHandle: 90}}},
				}
				if batch {
					go f.drainEventBatches(&restoreEventStream[rtiv1.FederateEventBatch]{ctx: ctx, events: []*rtiv1.FederateEventBatch{{Events: events}}})
				} else {
					go f.drainEvents(&restoreEventStream[rtiv1.FederateEvent]{ctx: ctx, events: events})
				}
				if !replyFirst {
					select {
					case <-f.drainDone:
						t.Fatal("marker did not wait for response")
					case <-time.After(20 * time.Millisecond):
					}
					close(reply)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				<-f.drainDone
				if f.restoreLocalDisabled.Load() {
					t.Fatal("complete hydration did not enable local admission")
				}
				if err := f.validateQueuedAttributeUpdate(80, map[uint64][]byte{7: nil}); err != nil {
					t.Fatalf("old callback overwrote snapshot: %v", err)
				}
				if err := f.validateQueuedAttributeUpdate(80, map[uint64][]byte{2: nil}); !errors.Is(err, ErrAttributeNotOwned) {
					t.Fatalf("ownership inferred from publication: %v", err)
				}
				if err := f.validateQueuedAttributeUpdate(90, nil); !errors.Is(err, ErrObjectInstanceNotKnown) {
					t.Fatalf("postmarker callback lost: %v", err)
				}
				if len(f.eventCh) != 2 {
					t.Fatalf("internal barrier published: %d", len(f.eventCh))
				}
				if err := f.AwaitLocalLRC(ctx, 1); err != nil {
					t.Fatalf("old acknowledged ticket lost: %v", err)
				}
				if err := f.AwaitLocalLRC(ctx, 2); !errors.Is(err, ErrLocalLRCIndeterminate) {
					t.Fatalf("old unresolved ticket aliased: %v", err)
				}
				ticket, err := f.QueueInteractionByHandle(ctx, 27, map[uint64][]byte{4: nil})
				if err != nil {
					t.Fatal(err)
				}
				if ticket != 3 {
					t.Fatalf("public ticket restarted: %d", ticket)
				}
				if err := f.FlushLocalLRC(ctx); err != nil {
					t.Fatal(err)
				}
				if err := f.AwaitLocalLRC(ctx, ticket); err != nil {
					t.Fatal(err)
				}
				f.localLRC.fail(context.Canceled)
			})
		}
	}
}

func TestLocalLRCHydrationFailureRemainsConfirmed(t *testing.T) {
	for _, kind := range []string{"rpc", "no-marker", "wrong-epoch", "malformed", "restore"} {
		t.Run(kind, func(t *testing.T) {
			f := newLocalLRCTestFederate(t, &recordingLocalLRCServer{ackEvery: 1}, 8)
			f.handles, _ = tablesFromFOMView(authoritativeView(1))
			f.restoreLocalDisabled.Store(true)
			f.sessionToken = []byte{0, 1, 2, 255}
			requests := make(chan *rtiv1.GetLocalValidationStateRequest, 1)
			f.conn.localLRC = hydrationClient{LocalLRCServiceClient: f.conn.localLRC, get: func(ctx context.Context, req *rtiv1.GetLocalValidationStateRequest) (*rtiv1.LocalValidationState, error) {
				md, _ := metadata.FromOutgoingContext(ctx)
				if got := md.Get("gorti-session-token-bin"); len(got) != 1 || got[0] != string(f.sessionToken) {
					t.Error("session token missing or modified")
				}
				requests <- req
				if kind == "rpc" {
					return nil, errors.New("query rejected")
				}
				state := &rtiv1.LocalValidationState{FederationGeneration: 9, FederateHandle: 7, SnapshotId: req.SnapshotId, FomView: authoritativeView(1)}
				if kind == "wrong-epoch" {
					state.CallbackEpoch = 1
				}
				if kind == "malformed" {
					state.Objects = []*rtiv1.KnownObjectState{{ObjectHandle: 80, ObjectClassHandle: 19, OwnedAttributeHandles: []uint64{999}}}
				}
				return state, nil
			}}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- f.ReinitializeLocalLRC(ctx) }()
			req := <-requests
			if kind == "restore" {
				f.cancelLocalHydration()
			}
			marker := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_LocalValidationBarrier{LocalValidationBarrier: &rtiv1.LocalValidationBarrier{SnapshotId: req.SnapshotId, FederateHandle: 7}}}
			if kind != "rpc" && kind != "no-marker" {
				f.consumeLocalValidationBarrier(marker)
			}
			if err := <-done; err == nil {
				t.Fatal("failed hydration accepted")
			}
			if !f.restoreLocalDisabled.Load() || f.useLocalLRCForReceiveOrder() {
				t.Fatal("failed hydration enabled local admission")
			}
			if !f.consumeLocalValidationBarrier(marker) {
				t.Fatal("late marker leaked into public stream")
			}
			if f.localLRC != nil {
				t.Fatal("failed hydration retained a new stream")
			}
		})
	}
}

func TestLocalLRCPublicTicketOverflowRejectedBeforeAdmission(t *testing.T) {
	l := &localLRC{ticketBase: ^uint64(0) - 1, next: 1}
	called := false
	ticket, err := l.enqueueTicket(context.Background(), func(uint64) *rtiv1.LocalLRCOperation { called = true; return nil })
	if err == nil || ticket != 0 || called {
		t.Fatalf("overflow admitted: ticket=%d err=%v build=%v", ticket, err, called)
	}
}

type blockedHydrationOpenClient struct {
	rtiv1.LocalLRCServiceClient
	entered chan struct{}
}

func (c blockedHydrationOpenClient) Exchange(ctx context.Context, _ ...grpc.CallOption) (rtiv1.LocalLRCService_ExchangeClient, error) {
	close(c.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRestoreCancelsLocalLRCOpeningBeforeWaitingForLock(t *testing.T) {
	f := newLocalLRCTestFederate(t, &recordingLocalLRCServer{ackEvery: 1}, 8)
	f.handles, _ = tablesFromFOMView(authoritativeView(1))
	f.conn.fed = fomViewClient{get: func(context.Context, *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
		return authoritativeView(1), nil
	}}
	entered := make(chan struct{})
	f.conn.localLRC = blockedHydrationOpenClient{entered: entered}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opening := make(chan error, 1)
	go func() { _, err := f.ensureLocalLRC(ctx); opening <- err }()
	<-entered
	restored := make(chan error, 1)
	go func() {
		h, e := uint64(9), uint64(1)
		restored <- f.applyRestoredState(context.Background(), FederationRestored{FederateHandle: &h, CallbackEpoch: &e})
	}()
	select {
	case err := <-restored:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		cancel()
		<-opening
		<-restored
		t.Fatal("restore waited for an opening stream it did not cancel")
	}
	if err := <-opening; err == nil {
		t.Fatal("canceled opening accepted")
	}
}
