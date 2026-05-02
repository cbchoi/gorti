package object

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// ===========================================================================
// Test fakes — minimal in-package doubles for core dependencies.
//
// These are deliberately separate from rti/spec/M2/fixtures.go so that the
// unit tests under rti/internal/object stand alone (no cross-package test
// helpers) and can exercise paths the spec tests don't (e.g. EventLog
// failure rollback, nil-options validation).
// ===========================================================================

type recordingOutbox struct {
	mu   sync.Mutex
	sent []outboxRecord
}

type outboxRecord struct {
	Federation core.FederationName
	Federate   core.FederateHandle
	Event      core.OutboundEvent
}

func (o *recordingOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, outboxRecord{fed, h, evt})
	return nil
}

func (o *recordingOutbox) Records() []outboxRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]outboxRecord, len(o.sent))
	copy(out, o.sent)
	return out
}

// recordingEventLog accepts any federation, assigns a monotonic seq, and
// keeps the appended records for inspection. Optionally fails the next
// Append to exercise rollback.
type recordingEventLog struct {
	mu       sync.Mutex
	nextSeq  uint64
	appends  []recordedAppend
	failNext error
}

type recordedAppend struct {
	Federation core.FederationName
	Seq        uint64
	Event      core.EventRecord
}

func (l *recordingEventLog) Append(_ context.Context, fed core.FederationName, evt core.EventRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failNext != nil {
		err := l.failNext
		l.failNext = nil
		return err
	}
	l.nextSeq++
	// The production registry wraps *rtiv1.Event in an adapter that keeps
	// the proto reachable via an Inner accessor. Tests don't need to know
	// the adapter type — they extract the proto by interface.
	if er, ok := evt.(eventCarrier); ok {
		er.SetSeq(l.nextSeq)
	}
	l.appends = append(l.appends, recordedAppend{fed, l.nextSeq, evt})
	return nil
}

// eventCarrier is satisfied by the adapter the registry uses to wrap
// *rtiv1.Event. The unit test's recording log uses it to inject the seq
// without depending on the adapter's concrete type.
type eventCarrier interface {
	core.EventRecord
	SetSeq(uint64)
	Inner() *rtiv1.Event
}

func (*recordingEventLog) Sync(_ context.Context, _ core.FederationName) error { return nil }

func (*recordingEventLog) OpenReader(_ context.Context, _ string) (core.EventLogReader, error) {
	return nil, errors.New("recordingEventLog: OpenReader not supported")
}

func (l *recordingEventLog) Records() []recordedAppend {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]recordedAppend, len(l.appends))
	copy(out, l.appends)
	return out
}

// stubFOMHandle and stubFOMRepo mirror the spec-test fakes for unit-test
// convenience. They are intentionally lenient so registry tests focus on
// routing + write-ahead rather than FOM resolution.
type stubFOMHandle struct{}

func (*stubFOMHandle) IsValid() bool                                                       { return true }
func (*stubFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool)             { return 1, true }
func (*stubFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool)   { return 1, true }
func (*stubFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (*stubFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

type stubFOMRepo struct{}

func (*stubFOMRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	return &stubFOMHandle{}, nil
}
func (*stubFOMRepo) Get(_ context.Context, _ core.FederationName) (core.FOMHandle, error) {
	return &stubFOMHandle{}, nil
}

func newTestRegistry(t *testing.T, log core.EventLog) (*Registry, *declaration.Manager, *recordingOutbox) {
	t.Helper()
	declMgr := declaration.New()
	outbox := &recordingOutbox{}
	reg, err := New(Options{
		EventLog:     log,
		Declarations: declMgr,
		Outbox:       outbox,
		FOMs:         &stubFOMRepo{},
		Clock:        core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return reg, declMgr, outbox
}

// ---------------------------------------------------------------------------
// TASK-030 — handle assignment + write-ahead
// ---------------------------------------------------------------------------

func TestNew_RejectsMissingDependencies(t *testing.T) {
	t.Parallel()
	declMgr := declaration.New()
	outbox := &recordingOutbox{}
	clock := core.NewFakeClock(time.Unix(0, 0))

	cases := []struct {
		name string
		opts Options
	}{
		{"missing Declarations", Options{Outbox: outbox, FOMs: &stubFOMRepo{}, Clock: clock}},
		{"missing Outbox", Options{Declarations: declMgr, FOMs: &stubFOMRepo{}, Clock: clock}},
		{"missing FOMs", Options{Declarations: declMgr, Outbox: outbox, Clock: clock}},
		{"missing Clock", Options{Declarations: declMgr, Outbox: outbox, FOMs: &stubFOMRepo{}}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(tc.opts); err == nil {
				t.Fatalf("New(%s): want error, got nil", tc.name)
			}
		})
	}
}

func TestNew_AcceptsNilEventLog(t *testing.T) {
	t.Parallel()
	// EventLog is optional in cut 1; mirrors the federation manager's
	// relaxation so an in-memory wiring (no log) still works for tests.
	reg, _, _ := newTestRegistry(t, nil)
	if reg == nil {
		t.Fatal("expected non-nil registry with nil EventLog")
	}
}

func TestRegister_AssignsMonotonicHandlesPerFederation(t *testing.T) {
	t.Parallel()
	reg, declMgr, _ := newTestRegistry(t, &recordingEventLog{})
	ctx := context.Background()

	const fed = core.FederationName("alpha")
	if err := declMgr.PublishObjectClassAttributes(ctx, fed, 1, 7, []core.AttributeHandle{1, 2, 3}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	for i := 1; i <= 5; i++ {
		h, name, err := reg.Register(ctx, fed, 1, 7, "")
		if err != nil {
			t.Fatalf("Register[%d]: %v", i, err)
		}
		if h != core.ObjectHandle(i) {
			t.Errorf("Register[%d] handle = %d, want %d", i, h, i)
		}
		if name == "" {
			t.Errorf("Register[%d] name should be auto-generated, got empty", i)
		}
	}
}

func TestRegister_HandleCountersAreIndependentPerFederation(t *testing.T) {
	t.Parallel()
	reg, declMgr, _ := newTestRegistry(t, &recordingEventLog{})
	ctx := context.Background()

	for _, fed := range []core.FederationName{"alpha", "beta"} {
		if err := declMgr.PublishObjectClassAttributes(ctx, fed, 1, 7, []core.AttributeHandle{1}); err != nil {
			t.Fatalf("Publish %s: %v", fed, err)
		}
		for i := 1; i <= 3; i++ {
			h, _, err := reg.Register(ctx, fed, 1, 7, "")
			if err != nil {
				t.Fatalf("Register %s[%d]: %v", fed, i, err)
			}
			if h != core.ObjectHandle(i) {
				t.Errorf("%s Register[%d] handle = %d, want %d", fed, i, h, i)
			}
		}
	}
}

func TestRegister_RejectsUnpublished(t *testing.T) {
	t.Parallel()
	reg, _, _ := newTestRegistry(t, &recordingEventLog{})
	_, _, err := reg.Register(context.Background(), "fed", 1, 7, "")
	if !errors.Is(err, core.ErrObjectClassNotPublished) {
		t.Errorf("Register without publish: err = %v, want ErrObjectClassNotPublished", err)
	}
}

func TestRegister_WriteAheadBeforeFanout(t *testing.T) {
	t.Parallel()
	log := &recordingEventLog{}
	reg, declMgr, outbox := newTestRegistry(t, log)
	ctx := context.Background()

	if err := declMgr.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := declMgr.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	h, _, err := reg.Register(ctx, "fed", 1, 7, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if h == 0 {
		t.Fatalf("expected non-zero handle")
	}

	appends := log.Records()
	if len(appends) != 1 {
		t.Fatalf("expected 1 eventlog append, got %d", len(appends))
	}
	carrier, ok := appends[0].Event.(eventCarrier)
	if !ok {
		t.Fatalf("appended event %T does not satisfy eventCarrier", appends[0].Event)
	}
	ev := carrier.Inner()
	body := ev.GetObjRegistered()
	if body == nil {
		t.Fatalf("appended event has no ObjectRegistered body: %+v", ev)
	}
	if body.GetObjectHandle() != uint64(h) {
		t.Errorf("ObjectRegistered.ObjectHandle = %d, want %d", body.GetObjectHandle(), h)
	}
	if body.GetOwnerFederateHandle() != 1 {
		t.Errorf("ObjectRegistered.OwnerFederateHandle = %d, want 1", body.GetOwnerFederateHandle())
	}

	// Fanout must follow the eventlog write — there is one subscriber
	// (federate 2) so exactly one Discover Send is expected.
	sends := outbox.Records()
	if len(sends) != 1 || sends[0].Federate != 2 {
		t.Errorf("Discover fanout = %+v, want one send to federate 2", sends)
	}
}

func TestRegister_RollsBackHandleOnEventLogFailure(t *testing.T) {
	t.Parallel()
	log := &recordingEventLog{failNext: errors.New("disk full")}
	reg, declMgr, outbox := newTestRegistry(t, log)
	ctx := context.Background()
	if err := declMgr.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if _, _, err := reg.Register(ctx, "fed", 1, 7, ""); err == nil {
		t.Fatalf("expected error from Register when EventLog.Append fails")
	}
	if got := outbox.Records(); len(got) != 0 {
		t.Errorf("no fanout should occur on log failure; got %+v", got)
	}

	// Next successful Register must reuse the would-have-been handle 1
	// (the failed attempt rolled back the counter).
	h, _, err := reg.Register(ctx, "fed", 1, 7, "")
	if err != nil {
		t.Fatalf("second Register: %v", err)
	}
	if h != core.ObjectHandle(1) {
		t.Errorf("after rollback, next handle = %d, want 1", h)
	}
}

func TestRegister_AutoGeneratesUniqueNamesByDefault(t *testing.T) {
	t.Parallel()
	reg, declMgr, _ := newTestRegistry(t, &recordingEventLog{})
	ctx := context.Background()
	_ = declMgr.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{1})

	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		_, name, err := reg.Register(ctx, "fed", 1, 7, "")
		if err != nil {
			t.Fatalf("Register[%d]: %v", i, err)
		}
		if seen[name] {
			t.Errorf("duplicate auto-generated name %q at i=%d", name, i)
		}
		seen[name] = true
	}
}

func TestRegister_RejectsDuplicateExplicitName(t *testing.T) {
	t.Parallel()
	reg, declMgr, _ := newTestRegistry(t, &recordingEventLog{})
	ctx := context.Background()
	_ = declMgr.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{1})

	if _, _, err := reg.Register(ctx, "fed", 1, 7, "vehicle-1"); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	_, _, err := reg.Register(ctx, "fed", 1, 7, "vehicle-1")
	if err == nil {
		t.Errorf("duplicate name should be rejected")
	}
}
