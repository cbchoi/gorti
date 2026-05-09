// TASK-205½ (M21) — Federate SDK foundation tests.
//
// Uses bufconn to bring up a real grpcsvc.Server with the cut-1
// service set, then dials it via the SDK's Connect/JoinFederation
// path. Verifies the lifecycle invariants the plan §2.7.0 + §6
// pinned (TASK-205½.1 ... 205½.6).

package federate

import (
	"context"
	"errors"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	"github.com/cbchoi/gorti/rti/internal/federation"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/transport/grpc"
)

// ----------------------------------------------------------------------------
// Bufconn fixture
// ----------------------------------------------------------------------------

type testRtid struct {
	listener *bufconn.Listener
	server   *grpc.Server
	stopOnce sync.Once
}

func newTestRtid(t *testing.T) *testRtid {
	t.Helper()
	return newTestRtidShared(t, false)
}

// newTestRtidWithTime is the time-aware variant: composes a real
// *time.Manager so the gRPC TimeService handlers don't return Unimplemented.
// W3A's time-method tests use this path; the base foundation tests use
// newTestRtid which leaves timeService nil.
func newTestRtidWithTime(t *testing.T) *testRtid {
	t.Helper()
	return newTestRtidShared(t, true)
}

func newTestRtidShared(t *testing.T, withTime bool) *testRtid {
	t.Helper()
	clock := core.NewFakeClock(time.Unix(0, 0))
	declMgr := declaration.New()
	outbox := newTestOutbox()
	objReg, err := object.New(object.Options{
		Clock:        clock,
		Outbox:       outbox,
		Declarations: declMgr,
		FOMs:         &nopFOMRepo{},
	})
	if err != nil {
		t.Fatalf("object.New: %v", err)
	}
	fedMgr, err := federation.New(federation.Options{
		Clock:    clock,
		EventLog: nil,
		FOMs:     &nopFOMRepo{},
	})
	if err != nil {
		t.Fatalf("federation.New: %v", err)
	}
	opts := grpcsvc.Options{
		Federations:  fedMgr,
		Declarations: declMgr,
		Objects:      objReg,
		Outbox:       outbox,
	}
	if withTime {
		mgr, mErr := timepkg.New(timepkg.Options{Clock: clock, Outbox: outbox})
		if mErr != nil {
			t.Fatalf("time.New: %v", mErr)
		}
		opts.Time = mgr
	}
	srv, err := grpcsvc.NewServer(opts)
	if err != nil {
		t.Fatalf("grpcsvc.NewServer: %v", err)
	}
	gs := grpc.NewServer()
	if err := srv.Register(gs); err != nil {
		t.Fatalf("Register: %v", err)
	}
	lis := bufconn.Listen(1 << 20)
	rtid := &testRtid{listener: lis, server: gs}
	go func() {
		_ = gs.Serve(lis)
	}()
	t.Cleanup(rtid.stop)
	return rtid
}

func (r *testRtid) stop() {
	r.stopOnce.Do(func() {
		r.server.GracefulStop()
		_ = r.listener.Close()
	})
}

func (r *testRtid) connect(t *testing.T) *Connection {
	t.Helper()
	cc, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return r.listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	return &Connection{
		cc:     cc,
		fed:    rtiv1.NewFederationServiceClient(cc),
		decl:   rtiv1.NewDeclarationServiceClient(cc),
		obj:    rtiv1.NewObjectServiceClient(cc),
		stream: rtiv1.NewStreamServiceClient(cc),
		tm:     rtiv1.NewTimeServiceClient(cc),
	}
}

// ----------------------------------------------------------------------------
// Test stubs
// ----------------------------------------------------------------------------

// testOutbox is the smallest core.Outbox + SubscribableOutbox impl
// needed for the streamService to bind. Records nothing.
type testOutbox struct {
	mu   sync.Mutex
	subs map[subKey]chan []core.OutboundEvent
}

type subKey struct {
	fed core.FederationName
	h   core.FederateHandle
}

func newTestOutbox() *testOutbox {
	return &testOutbox{subs: map[subKey]chan []core.OutboundEvent{}}
}

func (o *testOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	ch, ok := o.subs[subKey{fed, h}]
	o.mu.Unlock()
	if !ok {
		return nil
	}
	select {
	case ch <- []core.OutboundEvent{evt}:
	default:
	}
	return nil
}

func (o *testOutbox) Subscribe(ctx context.Context, fed core.FederationName, h core.FederateHandle) (<-chan []core.OutboundEvent, func() error, error) {
	ch := make(chan []core.OutboundEvent, 64)
	key := subKey{fed, h}
	o.mu.Lock()
	o.subs[key] = ch
	o.mu.Unlock()
	cancel := func() error {
		o.mu.Lock()
		delete(o.subs, key)
		o.mu.Unlock()
		return nil
	}
	go func() {
		<-ctx.Done()
		_ = cancel()
		close(ch)
	}()
	return ch, cancel, nil
}

// nopFOMRepo satisfies core.FOMRepository for the bufconn fixture.
// Returns an empty FOM handle for any module set; tests that need
// real handles supply their own FOM XML to JoinFederation and rely
// on the SDK's local fomTables for client-side resolution.
type nopFOMRepo struct{}

func (n *nopFOMRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	return emptyFOMHandle{}, nil
}
func (n *nopFOMRepo) Get(_ context.Context, _ core.FederationName) (core.FOMHandle, error) {
	return emptyFOMHandle{}, nil
}
func (n *nopFOMRepo) RememberFor(_ core.FederationName, _ core.FOMHandle) {}
func (n *nopFOMRepo) Forget(_ core.FederationName)                        {}

type emptyFOMHandle struct{}

func (emptyFOMHandle) IsValid() bool { return true }
func (emptyFOMHandle) LookupInteractionClass(_ string) (core.InteractionClassHandle, bool) {
	return core.InvalidInteractionClassHandle, false
}
func (emptyFOMHandle) LookupObjectClass(_ string) (core.ObjectClassHandle, bool) {
	return core.InvalidObjectClassHandle, false
}
func (emptyFOMHandle) LookupAttribute(_ core.ObjectClassHandle, _ string) (core.AttributeHandle, bool) {
	return core.InvalidAttributeHandle, false
}
func (emptyFOMHandle) LookupParameter(_ core.InteractionClassHandle, _ string) (core.ParameterHandle, bool) {
	return core.InvalidParameterHandle, false
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

// 205½.1 — Connect + Close cycle; no goroutine leak across -count=10.
func TestConnectCloseLeakFree(t *testing.T) {
	rtid := newTestRtid(t)
	baseline := runtime.NumGoroutine()
	for i := 0; i < 10; i++ {
		conn := rtid.connect(t)
		if err := conn.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}
	time.Sleep(20 * time.Millisecond)
	final := runtime.NumGoroutine()
	if final > baseline+3 {
		t.Errorf("goroutine leak: baseline=%d final=%d", baseline, final)
	}
}

// 205½.2 — JoinFederation on a fresh rtid.
func TestJoinFederationFresh(t *testing.T) {
	rtid := newTestRtid(t)
	conn := rtid.connect(t)
	t.Cleanup(func() { _ = conn.Close() })

	ctx := context.Background()
	fed, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "alpha")
	if err != nil {
		t.Fatalf("JoinFederation: %v", err)
	}
	if fed.Handle() == 0 {
		t.Errorf("Handle = 0, want non-zero")
	}
	if fed.Name() != "alpha" {
		t.Errorf("Name = %q, want %q", fed.Name(), "alpha")
	}
	if fed.Events() == nil {
		t.Errorf("Events() returned nil channel")
	}
	if err := fed.Resign(ctx); err != nil {
		t.Errorf("Resign: %v", err)
	}
}

// 205½.3 — JoinFederation when the federation already exists (idempotent).
func TestJoinFederationIdempotent(t *testing.T) {
	rtid := newTestRtid(t)
	conn := rtid.connect(t)
	t.Cleanup(func() { _ = conn.Close() })

	ctx := context.Background()
	fed1, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "alpha")
	if err != nil {
		t.Fatalf("first JoinFederation: %v", err)
	}
	fed2, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "beta")
	if err != nil {
		t.Fatalf("second JoinFederation (already-exists path): %v", err)
	}
	if fed1.Handle() == fed2.Handle() {
		t.Errorf("federate handles should be distinct: both %d", fed1.Handle())
	}
	_ = fed1.Resign(ctx)
	_ = fed2.Resign(ctx)
}

// 205½.4 — Resign closes the events channel within reasonable time.
func TestResignClosesEvents(t *testing.T) {
	rtid := newTestRtid(t)
	conn := rtid.connect(t)
	t.Cleanup(func() { _ = conn.Close() })

	ctx := context.Background()
	fed, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "alpha")
	if err != nil {
		t.Fatalf("JoinFederation: %v", err)
	}
	if err := fed.Resign(ctx); err != nil {
		t.Errorf("Resign: %v", err)
	}
	// Channel must be closed after Resign returns.
	select {
	case _, ok := <-fed.Events():
		if ok {
			t.Errorf("Events() returned a value after Resign; want closed channel")
		}
	case <-time.After(2 * time.Second):
		t.Errorf("Events() channel not closed within 2s of Resign")
	}
}

// 205½.5 — events-drain goroutine leak-free across many join/resign cycles.
func TestEventsGoroutineLeakFree(t *testing.T) {
	rtid := newTestRtid(t)
	conn := rtid.connect(t)
	t.Cleanup(func() { _ = conn.Close() })

	ctx := context.Background()
	// Warm-up cycle so any one-time initialization happens.
	fed, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "alpha")
	if err != nil {
		t.Fatalf("warm-up join: %v", err)
	}
	_ = fed.Resign(ctx)
	time.Sleep(20 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		fed, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "f")
		if err != nil {
			t.Fatalf("join cycle %d: %v", i, err)
		}
		_ = fed.Resign(ctx)
	}
	time.Sleep(50 * time.Millisecond)
	final := runtime.NumGoroutine()
	if final > baseline+5 {
		t.Errorf("goroutine leak: baseline=%d final=%d", baseline, final)
	}
}

// 205½.6 — Resign is idempotent.
func TestResignIdempotent(t *testing.T) {
	rtid := newTestRtid(t)
	conn := rtid.connect(t)
	t.Cleanup(func() { _ = conn.Close() })

	ctx := context.Background()
	fed, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "alpha")
	if err != nil {
		t.Fatalf("JoinFederation: %v", err)
	}
	if err := fed.Resign(ctx); err != nil {
		t.Errorf("first Resign: %v", err)
	}
	if err := fed.Resign(ctx); err != nil {
		t.Errorf("second Resign should be no-op, got: %v", err)
	}
}

// Compile-time guard: errors.Is unwraps the typed errors correctly.
var _ = errors.Is
