package federate

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
)

type restoreEventStream[T any] struct {
	grpc.ClientStream
	ctx    context.Context
	events []*T
}

func TestRestoreTerminalFailureAndLegacyIdentity(t *testing.T) {
	for _, fail := range []bool{false, true} {
		tables, err := tablesFromFOMView(authoritativeView(2))
		if err != nil {
			t.Fatal(err)
		}
		fetchErr := errors.New("authoritative view unavailable")
		f := &Federate{federateHandle: 3, handles: tables, federationName: testFederationName, eventCh: make(chan Event, 2), drainDone: make(chan struct{})}
		f.conn = &Connection{fed: fomViewClient{get: func(_ context.Context, req *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
			if req.FederateHandle != 3 {
				t.Errorf("legacy identity changed: %d", req.FederateHandle)
			}
			if fail {
				return nil, fetchErr
			}
			return authoritativeView(1), nil
		}}}
		event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_RestoreCompleted{RestoreCompleted: &rtiv1.FederationRestored{Label: "legacy"}}}
		f.drainEventBatches(&restoreEventStream[rtiv1.FederateEventBatch]{ctx: context.Background(), events: []*rtiv1.FederateEventBatch{{Events: []*rtiv1.FederateEvent{event, {Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: 99}}}}}}})
		got := <-f.eventCh
		if fail {
			failure, ok := got.(RestoreStateError)
			if !ok || !errors.Is(failure, fetchErr) {
				t.Fatalf("failure=%#v", got)
			}
			if _, open := <-f.eventCh; open {
				t.Fatal("callback after failed restore synchronization")
			}
			if f.transportLifetime().ctx.Err() == nil {
				t.Fatal("failed restore re-enabled transport")
			}
		} else {
			if _, ok := got.(FederationRestored); !ok {
				t.Fatalf("legacy terminal=%T", got)
			}
			if f.Handle() != 3 || f.CallbackEpoch() != 0 || f.FOMRevision() != 1 {
				t.Fatal("legacy restore fabricated identity or retained FOM")
			}
			if _, ok := (<-f.eventCh).(TimeAdvanceGrant); !ok {
				t.Fatal("next callback missing")
			}
		}
	}
}

type restoreBlockingObjectClient struct {
	rtiv1.ObjectServiceClient
	entered chan struct{}
}

func (c restoreBlockingObjectClient) SendInteraction(ctx context.Context, _ *rtiv1.SendInteractionRequest, _ ...grpc.CallOption) (*rtiv1.Empty, error) {
	close(c.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRestoreCancelsOutstandingTransportAndInvalidatesLocalAdmission(t *testing.T) {
	tables, err := tablesFromFOMView(authoritativeView(2))
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	f := &Federate{federateHandle: 3, federationName: testFederationName, handles: tables}
	f.conn = &Connection{obj: restoreBlockingObjectClient{entered: entered}, fed: fomViewClient{get: func(context.Context, *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
		return authoritativeView(1), nil
	}}}
	f.conn.localLRCEnabled = true
	f.conn.localLRC = struct{ rtiv1.LocalLRCServiceClient }{}
	if !f.useLocalLRCForReceiveOrder() {
		t.Fatal("fixture did not enable local admission before restore")
	}
	f.localLRC = &localLRC{next: 1, changed: make(chan struct{}), stop: make(chan struct{}), cancel: func() {}}
	operationDone := make(chan error, 1)
	go func() { operationDone <- f.SendInteractionByHandleConfirmed(context.Background(), 27, nil, nil) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("operation never entered")
	}
	restoreDone := make(chan error, 1)
	handle, epoch := uint64(7), uint64(1)
	go func() {
		restoreDone <- f.applyRestoredState(context.Background(), FederationRestored{FederateHandle: &handle, CallbackEpoch: &epoch})
	}()
	select {
	case err := <-restoreDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("restore waited for a transport it must cancel")
	}
	if err := <-operationDone; err == nil {
		t.Fatal("cancelled old operation reported success")
	}
	if f.transportLifetime().ctx.Err() != nil {
		t.Fatal("new confirmed transport remains cancelled")
	}
	if err := f.AwaitLocalLRC(context.Background(), 1); !errors.Is(err, ErrLocalLRCIndeterminate) {
		t.Fatalf("old sequence result=%v", err)
	}
	if _, err := f.QueueInteractionByHandle(context.Background(), 27, nil); !errors.Is(err, ErrLocalLRCRestoreStateUnavailable) {
		t.Fatalf("queue interaction=%v", err)
	}
	if _, err := f.QueueAttributeValuesByHandle(context.Background(), 80, nil); !errors.Is(err, ErrLocalLRCRestoreStateUnavailable) {
		t.Fatalf("queue attributes=%v", err)
	}
	if f.useLocalLRCForReceiveOrder() {
		t.Fatal("local admission re-enabled without restored mirror")
	}
	if f.CurrentHandle() != 7 || f.CallbackEpoch() != 1 {
		t.Fatal("restored identity missing")
	}
}

func TestRestoreIdentityConcurrentReadersAndEpochValidation(t *testing.T) {
	tables, err := tablesFromFOMView(authoritativeView(1))
	if err != nil {
		t.Fatal(err)
	}
	f := &Federate{federateHandle: 3, federationName: testFederationName, handles: tables, conn: &Connection{fed: fomViewClient{get: func(context.Context, *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
		return authoritativeView(1), nil
	}}}}
	var readers sync.WaitGroup
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for range 2000 {
				if h := f.Handle(); h != 3 && h != 7 {
					t.Errorf("invalid identity %d", h)
					return
				}
				if f.CallbackEpoch() > 30 {
					t.Error("invalid callback epoch")
					return
				}
				_, _ = f.ObjectClassHandle("Z")
			}
		}()
	}
	for epoch := uint64(1); epoch <= 30; epoch++ {
		handle := uint64(7)
		if err := f.applyRestoredState(context.Background(), FederationRestored{FederateHandle: &handle, CallbackEpoch: &epoch}); err != nil {
			t.Fatal(err)
		}
	}
	readers.Wait()
	oldIdentity := f.restoredIdentity.Load()
	zero, stale := uint64(0), uint64(29)
	for _, event := range []FederationRestored{{FederateHandle: &zero}, {CallbackEpoch: &stale}} {
		if err := f.applyRestoredState(context.Background(), event); err == nil {
			t.Fatal("invalid restore metadata accepted")
		}
		if f.restoredIdentity.Load() != oldIdentity {
			t.Fatal("invalid metadata changed identity")
		}
	}
}

func (s *restoreEventStream[T]) Context() context.Context { return s.ctx }
func (s *restoreEventStream[T]) Recv() (*T, error) {
	if len(s.events) == 0 {
		return nil, io.EOF
	}
	e := s.events[0]
	s.events = s.events[1:]
	return e, nil
}

func TestRestoreTerminalRefreshesBeforePublication(t *testing.T) {
	for _, batched := range []bool{false, true} {
		name := "single"
		if batched {
			name = "batch"
		}
		t.Run(name, func(t *testing.T) {
			tables, err := tablesFromFOMView(authoritativeView(2))
			if err != nil {
				t.Fatal(err)
			}
			refreshEntered := make(chan struct{})
			releaseRefresh := make(chan struct{})
			defer close(releaseRefresh)
			f := &Federate{federateHandle: 3, federationName: testFederationName, handles: tables,
				eventCh: make(chan Event, 2), drainDone: make(chan struct{}),
				objectClasses:             map[uint64]uint64{80: 19},
				publishedObjectAttributes: map[uint64]map[uint64]struct{}{19: {2: {}}},
				publishedInteractions:     map[uint64]struct{}{27: {}},
				ownedObjectAttributes:     map[uint64]map[uint64]struct{}{80: {2: {}}},
			}
			f.conn = &Connection{fed: fomViewClient{get: func(ctx context.Context, req *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
				if req.FederateHandle != 9 {
					t.Errorf("restore lookup used old handle %d", req.FederateHandle)
				}
				close(refreshEntered)
				select {
				case <-releaseRefresh:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				return authoritativeView(1), nil
			}}}
			handle, epoch := uint64(9), uint64(4)
			event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_RestoreCompleted{RestoreCompleted: &rtiv1.FederationRestored{Label: "saved", FederateHandle: &handle, CallbackEpoch: &epoch}}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if batched {
				go f.drainEventBatches(&restoreEventStream[rtiv1.FederateEventBatch]{ctx: ctx, events: []*rtiv1.FederateEventBatch{{Events: []*rtiv1.FederateEvent{event}}}})
			} else {
				go f.drainEvents(&restoreEventStream[rtiv1.FederateEvent]{ctx: ctx, events: []*rtiv1.FederateEvent{event}})
			}
			select {
			case <-refreshEntered:
			case got := <-f.eventCh:
				t.Fatalf("restore published before authoritative refresh: %T", got)
			case <-time.After(2 * time.Second):
				t.Fatal("restore never fetched FOM")
			}
			select {
			case got := <-f.eventCh:
				t.Fatalf("restore published while refresh blocked: %T", got)
			default:
			}
			releaseRefresh <- struct{}{}
			select {
			case got := <-f.eventCh:
				if _, ok := got.(FederationRestored); !ok {
					t.Fatalf("terminal=%T", got)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("restore terminal missing")
			}
			if f.Handle() != 9 || f.FOMRevision() != 1 {
				t.Fatalf("restored handle/revision=%d/%d", f.Handle(), f.FOMRevision())
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.objectClasses)+len(f.publishedObjectAttributes)+len(f.publishedInteractions)+len(f.ownedObjectAttributes) != 0 {
				t.Fatal("pre-restore state retained")
			}
		})
	}
}
