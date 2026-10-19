package server

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
)

type syncIntegrationOutbox struct {
	mu   sync.Mutex
	fail bool
	sent []core.OutboxDelivery
}

func (o *syncIntegrationOutbox) Send(_ context.Context, _ core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fail {
		return core.ErrFederateOverflow
	}
	o.sent = append(o.sent, core.OutboxDelivery{Recipient: h, Event: event})
	return nil
}
func (o *syncIntegrationOutbox) Reserve(_ context.Context, _ core.FederationName, events []core.OutboxDelivery) (core.OutboxReservation, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fail {
		return nil, core.ErrFederateOverflow
	}
	return &syncIntegrationReservation{out: o, events: append([]core.OutboxDelivery(nil), events...)}, nil
}

type syncIntegrationReservation struct {
	once   sync.Once
	out    *syncIntegrationOutbox
	events []core.OutboxDelivery
}

func (r *syncIntegrationReservation) Commit() error {
	r.once.Do(func() { r.out.mu.Lock(); defer r.out.mu.Unlock(); r.out.sent = append(r.out.sent, r.events...) })
	return nil
}
func (r *syncIntegrationReservation) Release()     { r.once.Do(func() {}) }
func (o *syncIntegrationOutbox) setFail(fail bool) { o.mu.Lock(); o.fail = fail; o.mu.Unlock() }
func (o *syncIntegrationOutbox) events() []core.OutboxDelivery {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]core.OutboxDelivery(nil), o.sent...)
}
func newSyncIntegrationManager(t *testing.T, out *syncIntegrationOutbox) *syncpkg.Manager {
	t.Helper()
	m, err := syncpkg.New(syncpkg.Options{Outbox: out, RequireInvocations: true, Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1, 2} }})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSynchronizationExistingOpaqueInvocationReceipt(t *testing.T) {
	ctx := context.Background()
	out := &syncIntegrationOutbox{}
	m := newSyncIntegrationManager(t, out)
	if err := m.RegisterBy(ctx, "f", 1, "p", nil, nil); err != nil {
		t.Fatal(err)
	}
	entry := syncCallbackEntryObserver(m)
	for _, delivery := range out.events() {
		if announced, ok := delivery.Event.(*syncpkg.AnnounceEvent); ok {
			wire := &rtiv1.FederateEvent{Seq: announced.Seq(), Event: &rtiv1.FederateEvent_SyncAnnounced{SyncAnnounced: &rtiv1.SynchronizationPointAnnounced{Label: announced.Label()}}}
			if err := entry(ctx, "f", delivery.Recipient, wire); err != nil {
				t.Fatal(err)
			}
			// Reentrant handler body, before generic invocation completion.
			if err := m.Achieve(ctx, "f", delivery.Recipient, "p"); err != nil {
				t.Fatal(err)
			}
		}
	}
	receipts, err := grpcadapter.NewCallbackReceipts(grpcadapter.CallbackReceiptOptions{
		Authenticate: func(_ context.Context, _ core.FederationName, h core.FederateHandle) error {
			if h != 1 && h != 2 {
				return errors.New("unauthorized")
			}
			return nil
		},
		Observe: syncCallbackInvocationObserver(m),
	})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for _, delivery := range out.events() {
		callback, ok := delivery.Event.(*syncpkg.SynchronizedEvent)
		if !ok {
			continue
		}
		wire := &rtiv1.FederateEvent{Seq: callback.Seq(), Event: &rtiv1.FederateEvent_SyncSynchronized{SyncSynchronized: &rtiv1.FederationSynchronized{Label: callback.Label()}}}
		ticket, commit, err := receipts.Prepare(ctx, "f", delivery.Recipient, wire)
		if err != nil || len(ticket) != 32 {
			t.Fatal("missing existing opaque receipt", err)
		}
		commit(true)
		if err := entry(ctx, "f", delivery.Recipient, wire); err != nil {
			t.Fatal(err)
		}
		if m.QueryState("f", "p") != syncpkg.StateAchieved {
			t.Fatal("stream send retired label")
		}
		if err := receipts.Report(ctx, "f", 99, ticket, true, ""); err == nil {
			t.Fatal("unauthorized report accepted")
		}
		// A throwing callback is still an actual invocation.
		if err := receipts.Report(ctx, "f", delivery.Recipient, ticket, false, "FederateInternalError"); err != nil {
			t.Fatal(err)
		}
		if err := receipts.Report(ctx, "f", delivery.Recipient, ticket, false, "FederateInternalError"); err != nil {
			t.Fatal("duplicate report", err)
		}
		count++
		if count == 1 && m.QueryState("f", "p") != syncpkg.StateAchieved {
			t.Fatal("first recipient retired label")
		}
	}
	if count != 2 || m.QueryState("f", "p") != syncpkg.StateUnknown {
		t.Fatal("invocation multicast did not retire label")
	}
}

func TestSynchronizationLifecycleRetryAndExactCheckpoint(t *testing.T) {
	ctx := context.Background()
	out := &syncIntegrationOutbox{}
	m := newSyncIntegrationManager(t, out)
	l := newSynchronizationLifecycle(m, nil)
	if err := m.RegisterBy(ctx, "f", 1, "p", []byte("tag"), nil); err != nil {
		t.Fatal(err)
	}
	out.setFail(true)
	if err := l.Joined(ctx, "f", 3); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatal(err)
	}
	codec := synchronizationCheckpoint{Manager: m}
	data, err := codec.Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	nextOut := &syncIntegrationOutbox{}
	next := newSyncIntegrationManager(t, nextOut)
	nextCodec := synchronizationCheckpoint{Manager: next}
	commit, err := nextCodec.PrepareRestore("f", data)
	if err != nil {
		t.Fatal(err)
	}
	if next.ExportState("f") != nil {
		t.Fatal("restore mutated before commit")
	}
	commit()
	beforeImage, afterImage := m.ExportState("f"), next.ExportState("f")
	afterImage.Pending = beforeImage.Pending
	if !reflect.DeepEqual(beforeImage, afterImage) {
		t.Fatal("checkpoint lost exact state")
	}
	restoredLifecycle := newSynchronizationLifecycle(next, nil)
	restoredLifecycle.Retry(ctx) // No join/resign error exists to schedule a restored image.
	if len(nextOut.events()) != len(beforeImage.Obligations) {
		t.Fatal("restored pending announce not delivered")
	}
	out.setFail(false)
	l.Retry(ctx)
	if len(m.ExportState("f").Pending) != 0 {
		t.Fatal("lifecycle retry lost wakeup")
	}
	if len(l.pending) != 0 {
		t.Fatal("completed retry still scheduled")
	}
	before := next.ExportState("f")
	if _, err := nextCodec.PrepareRestore("f", append(data, []byte("{}")...)); err == nil {
		t.Fatal("trailing checkpoint data accepted")
	}
	if !reflect.DeepEqual(before, next.ExportState("f")) {
		t.Fatal("corrupt checkpoint mutated state")
	}
}

func TestSynchronizationStrictCheckpointRejectsLegacyActiveImage(t *testing.T) {
	legacy, err := syncpkg.New(syncpkg.Options{Outbox: &syncIntegrationOutbox{}, Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} }})
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Register(context.Background(), "f", "p", nil, nil); err != nil {
		t.Fatal(err)
	}
	data, err := (savebundle.Synchronization{Manager: legacy}).Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	strict := newSyncIntegrationManager(t, &syncIntegrationOutbox{})
	if _, err := (synchronizationCheckpoint{Manager: strict}).PrepareRestore("f", data); err == nil {
		t.Fatal("legacy image fabricated strict history")
	}
	if strict.ExportState("f") != nil {
		t.Fatal("rejected legacy restore mutated state")
	}
}
