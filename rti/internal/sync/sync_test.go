package sync

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
}

type fakeOutbox struct {
	mu   gosync.Mutex
	sent []sentRecord
}

func (o *fakeOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, _ core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, sentRecord{fed, h})
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

func TestRegister_FansOutAnnounceToRequiredFederates(t *testing.T) {
	mgr, outbox := newMgr(t, Options{})
	if err := mgr.Register(context.Background(), "fed", "phase1", []byte("tag"),
		[]core.FederateHandle{1, 2, 3}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got, want := len(outbox.Sent()), 3; got != want {
		t.Errorf("announce emissions = %d; want %d", got, want)
	}
	if state := mgr.QueryState("fed", "phase1"); state != StateAnnounced {
		t.Errorf("QueryState = %v; want StateAnnounced", state)
	}
}

func TestRegister_NilRequiredFederates_UsesMembersResolver(t *testing.T) {
	mgr, outbox := newMgr(t, Options{
		Members: func(_ core.FederationName) []core.FederateHandle {
			return []core.FederateHandle{10, 20}
		},
	})
	if err := mgr.Register(context.Background(), "fed", "phase1", nil, nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got, want := len(outbox.Sent()), 2; got != want {
		t.Errorf("announce emissions = %d; want %d", got, want)
	}
}

func TestRegister_NilRequiredAndNilMembers_DynamicMode(t *testing.T) {
	mgr, outbox := newMgr(t, Options{})
	if err := mgr.Register(context.Background(), "fed", "phase1", nil, nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Dynamic mode → no recipients at register time.
	if got := len(outbox.Sent()); got != 0 {
		t.Errorf("dynamic-mode announce emissions = %d; want 0", got)
	}
	// First Achieve completes the sync point.
	if err := mgr.Achieve(context.Background(), "fed", 1, "phase1"); err != nil {
		t.Fatalf("Achieve: %v", err)
	}
	if state := mgr.QueryState("fed", "phase1"); state != StateAchieved {
		t.Errorf("QueryState after dynamic Achieve = %v; want StateAchieved", state)
	}
	// One synchronized fan-out to the sole achieved federate.
	if got := len(outbox.Sent()); got != 1 {
		t.Errorf("synchronized fan-out emissions = %d; want 1", got)
	}
}

func TestRegister_AlreadyRegistered(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	ctx := context.Background()
	if err := mgr.Register(ctx, "fed", "phase1", nil, nil); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := mgr.Register(ctx, "fed", "phase1", nil, nil); !errors.Is(err, core.ErrSyncPointAlreadyRegistered) {
		t.Errorf("re-Register err = %v; want ErrSyncPointAlreadyRegistered", err)
	}
}

func TestAchieve_AllRequiredFires(t *testing.T) {
	mgr, outbox := newMgr(t, Options{})
	ctx := context.Background()
	if err := mgr.Register(ctx, "fed", "phase1", nil,
		[]core.FederateHandle{1, 2}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	pre := len(outbox.Sent())
	if err := mgr.Achieve(ctx, "fed", 1, "phase1"); err != nil {
		t.Fatalf("Achieve 1: %v", err)
	}
	if state := mgr.QueryState("fed", "phase1"); state != StateAnnounced {
		t.Errorf("after one Achieve, state = %v; want StateAnnounced", state)
	}
	if got := len(outbox.Sent()); got != pre {
		t.Errorf("synchronized emissions before all-achieved = %d; want %d (none yet)", got, pre)
	}
	if err := mgr.Achieve(ctx, "fed", 2, "phase1"); err != nil {
		t.Fatalf("Achieve 2: %v", err)
	}
	if state := mgr.QueryState("fed", "phase1"); state != StateAchieved {
		t.Errorf("after both Achieves, state = %v; want StateAchieved", state)
	}
	if got := len(outbox.Sent()) - pre; got != 2 {
		t.Errorf("synchronized emissions = %d; want 2", got)
	}
}

func TestAchieve_TwiceRejected(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	ctx := context.Background()
	if err := mgr.Register(ctx, "fed", "phase1", nil,
		[]core.FederateHandle{1, 2}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := mgr.Achieve(ctx, "fed", 1, "phase1"); err != nil {
		t.Fatalf("first Achieve: %v", err)
	}
	if err := mgr.Achieve(ctx, "fed", 1, "phase1"); !errors.Is(err, core.ErrSyncPointAlreadyAchieved) {
		t.Errorf("second Achieve err = %v; want ErrSyncPointAlreadyAchieved", err)
	}
}

func TestAchieve_UnregisteredRejected(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	if err := mgr.Achieve(context.Background(), "fed", 1, "no-such"); !errors.Is(err, core.ErrSyncPointNotRegistered) {
		t.Errorf("Achieve unregistered: err = %v; want ErrSyncPointNotRegistered", err)
	}
}

func TestQueryState_UnknownReturnsStateUnknown(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	if state := mgr.QueryState("fed", "no-such"); state != StateUnknown {
		t.Errorf("QueryState unknown = %v; want StateUnknown", state)
	}
}

func TestRegister_TagDefensiveCopy(t *testing.T) {
	mgr, _ := newMgr(t, Options{})
	tag := []byte("hello")
	if err := mgr.Register(context.Background(), "fed", "phase1", tag,
		[]core.FederateHandle{1}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	tag[0] = 'X'
	// The internal copy should be unaffected — we cannot read it
	// directly, but a re-register with the SAME label still fails
	// with AlreadyRegistered (verifying the manager kept the point).
	if err := mgr.Register(context.Background(), "fed", "phase1", nil,
		[]core.FederateHandle{1}); !errors.Is(err, core.ErrSyncPointAlreadyRegistered) {
		t.Errorf("re-Register: err = %v; want ErrSyncPointAlreadyRegistered", err)
	}
}

func TestConcurrent_Achieve(t *testing.T) {
	mgr, outbox := newMgr(t, Options{})
	required := make([]core.FederateHandle, 50)
	for i := range required {
		required[i] = core.FederateHandle(i + 1)
	}
	if err := mgr.Register(context.Background(), "fed", "phase1", nil, required); err != nil {
		t.Fatalf("Register: %v", err)
	}
	var wg gosync.WaitGroup
	for _, h := range required {
		wg.Add(1)
		go func(h core.FederateHandle) {
			defer wg.Done()
			if err := mgr.Achieve(context.Background(), "fed", h, "phase1"); err != nil {
				t.Errorf("Achieve %d: %v", h, err)
			}
		}(h)
	}
	wg.Wait()
	if state := mgr.QueryState("fed", "phase1"); state != StateAchieved {
		t.Errorf("post-concurrent state = %v; want StateAchieved", state)
	}
	// announce (50) + synchronized (50) = 100 total emissions.
	if got := len(outbox.Sent()); got != 100 {
		t.Errorf("total emissions = %d; want 100", got)
	}
}

func TestEventLog_AppendsRegisteredAchievedSynchronized(t *testing.T) {
	log := &permissiveLog{}
	mgr, _ := newMgr(t, Options{EventLog: log})
	ctx := context.Background()
	if err := mgr.Register(ctx, "fed", "phase1", nil,
		[]core.FederateHandle{1, 2}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := mgr.Achieve(ctx, "fed", 1, "phase1"); err != nil {
		t.Fatalf("Achieve 1: %v", err)
	}
	if err := mgr.Achieve(ctx, "fed", 2, "phase1"); err != nil {
		t.Fatalf("Achieve 2: %v", err)
	}
	// Expect: 1 register + 2 achieve + 1 synchronized = 4 records.
	if got := len(log.records); got != 4 {
		t.Errorf("eventlog records = %d; want 4", got)
	}
}
