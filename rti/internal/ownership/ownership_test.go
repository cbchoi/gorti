package ownership

import (
	"context"
	"errors"
	gosync "sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// fakeOutbox + permissiveLog mirror the spec/M8 fixtures so this
// package's tests stay independent of the specification fixtures.

type sentRecord struct {
	Federation core.FederationName
	Federate   core.FederateHandle
	Event      core.OutboundEvent
}

type fakeOutbox struct {
	mu   gosync.Mutex
	sent []sentRecord
}

func (o *fakeOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, sentRecord{fed, h, evt})
	return nil
}

func (o *fakeOutbox) Sent() []sentRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]sentRecord, len(o.sent))
	copy(out, o.sent)
	return out
}

type permissiveLog struct {
	mu      gosync.Mutex
	records []core.EventRecord
}

func (l *permissiveLog) Append(_ context.Context, _ core.FederationName, evt core.EventRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, evt)
	return nil
}
func (*permissiveLog) Sync(_ context.Context, _ core.FederationName) error { return nil }
func (*permissiveLog) OpenReader(_ context.Context, _ string) (core.EventLogReader, error) {
	return nil, errors.New("unsupported")
}

func newMgr(t *testing.T, opts Options) (*Manager, *fakeOutbox) {
	t.Helper()
	if opts.Outbox == nil {
		opts.Outbox = &fakeOutbox{}
	}
	mgr, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mgr, opts.Outbox.(*fakeOutbox)
}

func TestNew_RequiresOutbox(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Errorf("New with nil Outbox: err = nil; want non-nil")
	}
}

func TestRegisterInitialOwnership_QueryRoundtrip(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1, 2, 3})
	for _, attr := range []core.AttributeHandle{1, 2, 3} {
		owner, ok := mgr.QueryOwnership("fed", 100, attr)
		if !ok || owner != 7 {
			t.Errorf("QueryOwnership(100,%d) = (%d, %v); want (7, true)", attr, owner, ok)
		}
	}
	if !mgr.IsOwnedBy("fed", 7, 100, 1) {
		t.Errorf("IsOwnedBy(7,100,1) = false; want true")
	}
}

func TestRegisterInitialOwnership_RejectsInvalidHandles(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	mgr.RegisterInitialOwnership("fed", core.InvalidFederateHandle, 100, []core.AttributeHandle{1})
	if _, ok := mgr.QueryOwnership("fed", 100, 1); ok {
		t.Errorf("QueryOwnership after invalid-owner Register = ok; want unowned")
	}
	mgr.RegisterInitialOwnership("fed", 7, core.InvalidObjectHandle, []core.AttributeHandle{1})
	if _, ok := mgr.QueryOwnership("fed", core.InvalidObjectHandle, 1); ok {
		t.Errorf("QueryOwnership after invalid-obj Register = ok; want unowned")
	}
}

func TestUnconditionalDivest_Happy(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1, 2})
	if err := mgr.UnconditionalDivest(context.Background(), "fed", 7, 100, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatalf("UnconditionalDivest: %v", err)
	}
	for _, a := range []core.AttributeHandle{1, 2} {
		if _, ok := mgr.QueryOwnership("fed", 100, a); ok {
			t.Errorf("attr %d still owned after divest", a)
		}
	}
}

func TestUnconditionalDivest_NotOwner(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1})
	if err := mgr.UnconditionalDivest(context.Background(), "fed", 99, 100, []core.AttributeHandle{1}); !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Errorf("UnconditionalDivest by non-owner: err = %v; want ErrAttributeNotOwned", err)
	}
}

func TestNegotiatedDivest_Pending(t *testing.T) {
	mgr, outbox := newMgr(t, Options{
		Subscribers: func(_ context.Context, _ core.FederationName, _ core.ObjectHandle, _ []core.AttributeHandle) []core.FederateHandle {
			return []core.FederateHandle{2, 3, 7}
		},
	})
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1})
	if err := mgr.NegotiatedDivest(context.Background(), "fed", 7, 100, []core.AttributeHandle{1}, []byte("tag")); err != nil {
		t.Fatalf("NegotiatedDivest: %v", err)
	}
	// Subscribers {2, 3, 7} → {2, 3} after owner exclusion.
	got := map[core.FederateHandle]int{}
	for _, rec := range outbox.Sent() {
		got[rec.Federate]++
	}
	if got[7] != 0 {
		t.Errorf("owner federate 7 received an assumption envelope; want 0")
	}
	if got[2] != 1 || got[3] != 1 {
		t.Errorf("recipients = %v; want {2:1, 3:1}", got)
	}
	// Owner still nominally owns until acquirer completes.
	if owner, ok := mgr.QueryOwnership("fed", 100, 1); !ok || owner != 7 {
		t.Errorf("post-divest owner = (%d, %v); want (7, true)", owner, ok)
	}
}

func TestNegotiatedDivest_TwiceRejected(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1})
	if err := mgr.NegotiatedDivest(context.Background(), "fed", 7, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatalf("first NegotiatedDivest: %v", err)
	}
	if err := mgr.NegotiatedDivest(context.Background(), "fed", 7, 100, []core.AttributeHandle{1}, nil); !errors.Is(err, core.ErrOwnershipDivestPending) {
		t.Errorf("second NegotiatedDivest: err = %v; want ErrOwnershipDivestPending", err)
	}
}

func TestAcquire_AfterDivest_Transfers(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1})
	ctx := context.Background()
	if err := mgr.NegotiatedDivest(ctx, "fed", 7, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatalf("NegotiatedDivest: %v", err)
	}
	if err := mgr.Acquire(ctx, "fed", 9, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if owner, _ := mgr.QueryOwnership("fed", 100, 1); owner != 9 {
		t.Errorf("post-Acquire owner = %d; want 9", owner)
	}
}

func TestAcquire_BeforeDivest_QueuesThenTransfersOnDivest(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1})
	ctx := context.Background()
	// Acquire first — no pending divest, so this queues.
	if err := mgr.Acquire(ctx, "fed", 9, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if owner, _ := mgr.QueryOwnership("fed", 100, 1); owner != 7 {
		t.Errorf("after Acquire-before-divest, owner = %d; want 7", owner)
	}
	if err := mgr.NegotiatedDivest(ctx, "fed", 7, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatalf("NegotiatedDivest: %v", err)
	}
	if owner, _ := mgr.QueryOwnership("fed", 100, 1); owner != 9 {
		t.Errorf("post-divest-of-queued-acquire owner = %d; want 9", owner)
	}
}

func TestAcquire_DuplicatePending_Rejected(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1})
	ctx := context.Background()
	if err := mgr.Acquire(ctx, "fed", 9, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if err := mgr.Acquire(ctx, "fed", 9, 100, []core.AttributeHandle{1}, nil); !errors.Is(err, core.ErrOwnershipAcquirePending) {
		t.Errorf("dup Acquire: err = %v; want ErrOwnershipAcquirePending", err)
	}
}

func TestCancelDivest_HappyAndError(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1})
	ctx := context.Background()
	if err := mgr.CancelDivest(ctx, "fed", 7, 100, []core.AttributeHandle{1}); !errors.Is(err, core.ErrOwnershipNotInTransfer) {
		t.Errorf("CancelDivest with no pending: err = %v; want ErrOwnershipNotInTransfer", err)
	}
	if err := mgr.NegotiatedDivest(ctx, "fed", 7, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatalf("NegotiatedDivest: %v", err)
	}
	if err := mgr.CancelDivest(ctx, "fed", 7, 100, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("CancelDivest: %v", err)
	}
	// After cancel, NegotiatedDivest should succeed again.
	if err := mgr.NegotiatedDivest(ctx, "fed", 7, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Errorf("re-divest after cancel: %v", err)
	}
}

func TestCancelAcquire_HappyAndError(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1})
	ctx := context.Background()
	if err := mgr.CancelAcquire(ctx, "fed", 9, 100, []core.AttributeHandle{1}); !errors.Is(err, core.ErrOwnershipNotInTransfer) {
		t.Errorf("CancelAcquire with no pending: err = %v; want ErrOwnershipNotInTransfer", err)
	}
	if err := mgr.Acquire(ctx, "fed", 9, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := mgr.CancelAcquire(ctx, "fed", 9, 100, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("CancelAcquire: %v", err)
	}
	// Re-Acquire should succeed (no longer pending).
	if err := mgr.Acquire(ctx, "fed", 9, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Errorf("re-Acquire after cancel: %v", err)
	}
}

func TestDivestIfWanted_NoAcquirer_StaysWithOwner(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1})
	if err := mgr.DivestIfWanted(context.Background(), "fed", 7, 100, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("DivestIfWanted: %v", err)
	}
	if owner, _ := mgr.QueryOwnership("fed", 100, 1); owner != 7 {
		t.Errorf("DivestIfWanted with no acquirer changed owner = %d; want 7", owner)
	}
}

func TestDivestIfWanted_WithQueuedAcquirer_Transfers(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1})
	ctx := context.Background()
	if err := mgr.Acquire(ctx, "fed", 9, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := mgr.DivestIfWanted(ctx, "fed", 7, 100, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("DivestIfWanted: %v", err)
	}
	if owner, _ := mgr.QueryOwnership("fed", 100, 1); owner != 9 {
		t.Errorf("post-DivestIfWanted owner = %d; want 9", owner)
	}
}

func TestDivestIfWanted_NotOwner_Rejected(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1})
	if err := mgr.DivestIfWanted(context.Background(), "fed", 99, 100, []core.AttributeHandle{1}); !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Errorf("DivestIfWanted by non-owner: err = %v; want ErrAttributeNotOwned", err)
	}
}

func TestQueryOwnership_Unknown_ReturnsZeroFalse(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	owner, ok := mgr.QueryOwnership("fed", 999, 999)
	if owner != 0 || ok {
		t.Errorf("QueryOwnership(unknown) = (%d, %v); want (0, false)", owner, ok)
	}
}

func TestConcurrent_QueryAndDivest(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	for i := 1; i <= 50; i++ {
		mgr.RegisterInitialOwnership("fed", core.FederateHandle(i), core.ObjectHandle(i), []core.AttributeHandle{1})
	}
	var wg gosync.WaitGroup
	for i := 1; i <= 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_, _ = mgr.QueryOwnership("fed", core.ObjectHandle(i), 1)
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = mgr.UnconditionalDivest(context.Background(), "fed", core.FederateHandle(i), core.ObjectHandle(i), []core.AttributeHandle{1})
		}(i)
	}
	wg.Wait()
	for i := 1; i <= 50; i++ {
		if _, ok := mgr.QueryOwnership("fed", core.ObjectHandle(i), 1); ok {
			t.Errorf("obj %d still owned after concurrent divest", i)
		}
	}
}

func TestEventLog_AppendsTransitions(t *testing.T) {
	log := &permissiveLog{}
	mgr, _ := newMgr(t, Options{EventLog: log})
	ctx := context.Background()
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1})
	if err := mgr.NegotiatedDivest(ctx, "fed", 7, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatalf("NegotiatedDivest: %v", err)
	}
	if err := mgr.Acquire(ctx, "fed", 9, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// Expect at least: 1 negotiated-divest + 1 acquire + 1 transferred.
	if got := len(log.records); got < 3 {
		t.Errorf("eventlog records = %d; want >= 3", got)
	}
}
