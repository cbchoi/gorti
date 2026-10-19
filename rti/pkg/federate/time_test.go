// TASK-207 (M21) — Go SDK time-method test suite.
// See docs/M21_DISPATCH_PLAN.md §6.

package federate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	grpcwire "google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

// timeFixture wires the bufconn rtid + a *grpc.Server with the
// time service composed, so the SDK's time RPCs hit a real handler.
// Mirrors newTestRtid in federate_test.go but adds Options.Time.
func timeFixture(t *testing.T) (*testRtid, *Connection, *Federate) {
	t.Helper()
	r := newTimeAwareTestRtid(t)
	conn := r.connect(t)
	t.Cleanup(func() { _ = conn.Close() })
	fed, err := conn.JoinFederation(context.Background(), FederationSpec{Name: "test-fed"}, "alpha")
	if err != nil {
		t.Fatalf("JoinFederation: %v", err)
	}
	t.Cleanup(func() { _ = fed.Resign(context.Background()) })
	return r, conn, fed
}

// 207.1 — EnableTimeRegulation; QueryLookahead returns the same value.
func TestEnableTimeRegulationLookaheadRoundTrip(t *testing.T) {
	_, _, fed := timeFixture(t)
	ctx := context.Background()
	if err := fed.EnableTimeRegulation(ctx, 1.5); err != nil {
		t.Fatalf("EnableTimeRegulation: %v", err)
	}
	la, err := fed.QueryLookahead(ctx)
	if err != nil {
		t.Fatalf("QueryLookahead: %v", err)
	}
	if la != 1.5 {
		t.Errorf("QueryLookahead = %v, want 1.5", la)
	}
}

// 207.2 — EnableTimeRegulation twice → errors.Is(err, ErrTimeRegulationAlreadyEnabled).
func TestEnableTimeRegulationTwiceTypedError(t *testing.T) {
	_, _, fed := timeFixture(t)
	ctx := context.Background()
	if err := fed.EnableTimeRegulation(ctx, 1.0); err != nil {
		t.Fatalf("first EnableTimeRegulation: %v", err)
	}
	err := fed.EnableTimeRegulation(ctx, 2.0)
	if !errors.Is(err, ErrTimeRegulationAlreadyEnabled) {
		t.Errorf("err = %v, want ErrTimeRegulationAlreadyEnabled", err)
	}
}

// A joined federate may advance without enabling either time mode.
func TestNERWithoutTimeEngagementGrants(t *testing.T) {
	_, _, fed := timeFixture(t)
	ctx := context.Background()
	if err := fed.NextMessageRequest(ctx, 5.0); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-fed.Events():
		grant, ok := event.(TimeAdvanceGrant)
		if !ok || grant.Time != 5 {
			t.Fatalf("callback = %#v, want grant 5", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("unengaged federate received no grant")
	}
	if current, err := fed.QueryLogicalTime(ctx); err != nil || current != 5 {
		t.Fatalf("logical time = %v, %v, want 5", current, err)
	}
}

// 207.4 — Two federates both regulating+constrained NER → both Events()
// channels deliver TimeAdvanceGrant{Time} within deadline.
//
// Setup uses lookahead=0 + 2 regulators so the LBTS-promotion via
// pending requests raises LBTS to the requested time and full grants
// fire (cf. TASK-203.7 trace).
func TestTwoFederatesNERGrantArrival(t *testing.T) {
	r := newTimeAwareTestRtid(t)
	conn := r.connect(t)
	t.Cleanup(func() { _ = conn.Close() })
	ctx := context.Background()
	a, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "alpha")
	if err != nil {
		t.Fatalf("alpha join: %v", err)
	}
	t.Cleanup(func() { _ = a.Resign(ctx) })
	b, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "beta")
	if err != nil {
		t.Fatalf("beta join: %v", err)
	}
	t.Cleanup(func() { _ = b.Resign(ctx) })

	// lookahead=0.5 so both pending federates' floor-promotions raise
	// LBTS to 10.5, which strictly exceeds the requested 10 → full grant.
	if err := a.EnableTimeRegulation(ctx, 0.5); err != nil {
		t.Fatalf("a.EnableTimeRegulation: %v", err)
	}
	if err := b.EnableTimeRegulation(ctx, 0.5); err != nil {
		t.Fatalf("b.EnableTimeRegulation: %v", err)
	}
	expectTimeEnable(t, a, true, 0)
	expectTimeEnable(t, b, true, 0)
	// Issue NERs concurrently. Both pending → LBTS-promotion → full grants.
	if err := a.NextMessageRequest(ctx, 10); err != nil {
		t.Fatalf("a.NER: %v", err)
	}
	if err := b.NextMessageRequest(ctx, 10); err != nil {
		t.Fatalf("b.NER: %v", err)
	}
	// Wait for grants on both event channels. The exact grant time
	// depends on the cut-1 forced-grant path (sole-pending fires at
	// LBTS even when LBTS < requested); we just assert SOME grant
	// arrives and is bounded by the requested time. The boundary
	// semantics are pinned at the manager level in TASK-203.
	for _, fed := range []*Federate{a, b} {
		select {
		case ev := <-fed.Events():
			grant, ok := ev.(TimeAdvanceGrant)
			if !ok {
				t.Errorf("got %T, want TimeAdvanceGrant", ev)
				continue
			}
			if grant.Time > 10 {
				t.Errorf("grant time = %v, exceeds requested 10", grant.Time)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("federate %q: no grant within 5s", fed.Name())
		}
	}
}

// 207.5 — Each of TAR / TARA / NMRA / FQR — issue + grant arrives.
// Sub-tests share the same setup pattern as 207.4 but vary the primitive.
func TestPerPrimitiveBoundaries(t *testing.T) {
	cases := map[string]func(*Federate, context.Context) error{
		"NMRA": func(f *Federate, ctx context.Context) error { return f.NextMessageRequestAvailable(ctx, 10) },
		"TAR":  func(f *Federate, ctx context.Context) error { return f.TimeAdvanceRequest(ctx, 10) },
		"TARA": func(f *Federate, ctx context.Context) error { return f.TimeAdvanceRequestAvailable(ctx, 10) },
		"FQR":  func(f *Federate, ctx context.Context) error { return f.FlushQueueRequest(ctx, 10) },
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			r := newTimeAwareTestRtid(t)
			conn := r.connect(t)
			t.Cleanup(func() { _ = conn.Close() })
			ctx := context.Background()
			a, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "alpha")
			if err != nil {
				t.Fatalf("alpha join: %v", err)
			}
			t.Cleanup(func() { _ = a.Resign(ctx) })
			b, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "beta")
			if err != nil {
				t.Fatalf("beta join: %v", err)
			}
			t.Cleanup(func() { _ = b.Resign(ctx) })
			// Positive lookahead makes LBTS strictly exceed the requested
			// time once both federates are pending, including ordinary TAR.
			if err := a.EnableTimeRegulation(ctx, 1); err != nil {
				t.Fatalf("a.EnableTimeRegulation: %v", err)
			}
			if err := b.EnableTimeRegulation(ctx, 1); err != nil {
				t.Fatalf("b.EnableTimeRegulation: %v", err)
			}
			expectTimeEnable(t, a, true, 0)
			expectTimeEnable(t, b, true, 0)
			if err := fn(a, ctx); err != nil {
				t.Fatalf("a primitive %s: %v", name, err)
			}
			if err := fn(b, ctx); err != nil {
				t.Fatalf("b primitive %s: %v", name, err)
			}
			// Both should receive a grant within deadline; the boundary
			// (strict vs inclusive) is exercised at the manager level
			// in TASK-203 and at the example level in TASK-211.
			for _, fed := range []*Federate{a, b} {
				event := nextEnableTestEvent(t, fed)
				grant, ok := event.(TimeAdvanceGrant)
				if !ok || grant.Time != 10 {
					t.Fatalf("%s callback=%#v, want grant 10", name, event)
				}
			}
		})
	}
}

// 207.6 — Federate Resigns mid-NER → no goroutine leak.
// (The Resign-during-pending is exercised at the manager level in
// TASK-205; here we just confirm the SDK side completes cleanly.)
func TestResignMidNERLeakFree(t *testing.T) {
	_, conn, fed := timeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	peer, err := conn.JoinFederation(ctx, FederationSpec{Name: "test-fed"}, "beta")
	if err != nil {
		t.Fatalf("peer join: %v", err)
	}
	t.Cleanup(func() { _ = peer.Resign(context.Background()) })
	// Keep the peer idle so the request cannot receive a grant before resign.
	if err := peer.EnableTimeRegulation(ctx, 0); err != nil {
		t.Fatalf("peer.EnableTimeRegulation: %v", err)
	}
	if err := fed.EnableTimeRegulation(ctx, 0); err != nil {
		t.Fatalf("EnableTimeRegulation: %v", err)
	}
	if err := fed.EnableTimeConstrained(ctx); err != nil {
		t.Fatal(err)
	}
	expectTimeEnable(t, peer, true, 0)
	expectTimeEnable(t, fed, true, 0)
	expectTimeEnable(t, fed, false, 0)
	if err := fed.NextMessageRequest(ctx, 10); err != nil {
		t.Fatalf("NER: %v", err)
	}
	if err := fed.NextMessageRequest(ctx, 11); !errors.Is(err, ErrTimeAdvancingState) {
		t.Fatalf("request before resign = %v, want ErrTimeAdvancingState proving NER is pending", err)
	}
	// Resign mid-pending. The cleanup is wired via cmd/rtid's resign
	// hook — unit-tested at TASK-205; here we just want the SDK call
	// to succeed and the events channel to close.
	if err := fed.Resign(ctx); err != nil {
		t.Errorf("Resign: %v", err)
	}
	select {
	case _, ok := <-fed.Events():
		if ok {
			t.Errorf("Events() still open after Resign")
		}
	case <-time.After(2 * time.Second):
		t.Errorf("Events() didn't close within 2s")
	}
}

// A zero-lookahead peer holds the four ordinary advances pending. FQR instead
// completes at current time; resignation must clean up either state.
func TestResignMidPendingAllPrimitives(t *testing.T) {
	for _, tc := range []struct {
		name    string
		advance func(*Federate, context.Context, float64) error
	}{
		{"NER", (*Federate).NextMessageRequest},
		{"NMRA", (*Federate).NextMessageRequestAvailable},
		{"TAR", (*Federate).TimeAdvanceRequest},
		{"TARA", (*Federate).TimeAdvanceRequestAvailable},
		{"FQR", (*Federate).FlushQueueRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, tm := newPendingResignTestRtid(t)
			conn := r.connect(t)
			t.Cleanup(func() { _ = conn.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			a, err := conn.JoinFederation(ctx, FederationSpec{Name: "pending"}, "alpha")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.Resign(context.Background()) })
			b, err := conn.JoinFederation(ctx, FederationSpec{Name: "pending"}, "beta")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = b.Resign(context.Background()) })
			for _, fed := range []*Federate{a, b} {
				if err := fed.EnableTimeRegulation(ctx, 0); err != nil {
					t.Fatal(err)
				}
				expectTimeEnable(t, fed, true, 0)
			}
			if err := a.EnableTimeConstrained(ctx); err != nil {
				t.Fatal(err)
			}
			expectTimeEnable(t, a, false, 0)
			if err := tc.advance(a, ctx, 10); err != nil {
				t.Fatal(err)
			}
			pending := false
			for _, state := range tm.Snapshot("pending").Federates {
				if uint64(state.Handle) == a.Handle() {
					pending = state.HasPendingRequest && state.PendingRequestTime == 10 && state.CurrentTime == 0
				}
			}
			if tc.name == "FQR" {
				if pending {
					t.Fatal("FQR remained pending instead of granting current time")
				}
				event := nextEnableTestEvent(t, a)
				grant, ok := event.(TimeAdvanceGrant)
				if !ok || grant.Time != 0 {
					t.Fatalf("FQR callback=%#v, want grant 0", event)
				}
			} else {
				if !pending {
					t.Fatal("resign fixture did not establish a pending request at time 10")
				}
				if err := tc.advance(a, ctx, 11); !errors.Is(err, ErrTimeAdvancingState) {
					t.Fatalf("second request: %v, want ErrTimeAdvancingState", err)
				}
			}
			if err := a.Resign(ctx); err != nil {
				t.Fatal(err)
			}
			for _, state := range tm.Snapshot("pending").Federates {
				if uint64(state.Handle) == a.Handle() {
					t.Fatalf("resigned federate retained time state: %+v", state)
				}
			}
			select {
			case event, open := <-a.Events():
				if open {
					t.Fatalf("callback after pending resignation: %#v", event)
				}
			case <-ctx.Done():
				t.Fatal("callback channel did not close after resignation")
			}
			if err := b.ModifyLookahead(ctx, 1); err != nil {
				t.Fatal(err)
			}
			if err := b.TimeAdvanceRequest(ctx, 7); err != nil {
				t.Fatal(err)
			}
			select {
			case event := <-b.Events():
				grant, ok := event.(TimeAdvanceGrant)
				if !ok || grant.Time != 7 {
					t.Fatalf("survivor callback=%#v, want grant 7", event)
				}
			case <-ctx.Done():
				t.Fatal("resigned regulator still blocked the survivor")
			}
		})
	}
}

// The older SDK fixture omits lifecycle hooks. This fixture composes the real
// resignation hook so a closed SDK channel cannot hide retained server state.
func newPendingResignTestRtid(t *testing.T) (*testRtid, *timepkg.Manager) {
	t.Helper()
	clock := core.NewManualClock(time.Unix(0, 0))
	outbox := newTestOutbox()
	tm, err := timepkg.New(timepkg.Options{Clock: clock, Outbox: outbox})
	if err != nil {
		t.Fatal(err)
	}
	decl := declaration.New()
	objects, err := object.New(object.Options{Clock: clock, Outbox: outbox, Declarations: decl, FOMs: &nopFOMRepo{}})
	if err != nil {
		t.Fatal(err)
	}
	fed, err := federation.New(federation.Options{Clock: clock, FOMs: &nopFOMRepo{}, OnFederateResigned: tm.OnFederateResign})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := grpc.NewServer(grpc.Options{Federations: fed, Declarations: decl, Objects: objects, Outbox: outbox, Time: tm})
	if err != nil {
		t.Fatal(err)
	}
	server := grpcwire.NewServer()
	if err := svc.Register(server); err != nil {
		t.Fatal(err)
	}
	r := &testRtid{listener: bufconn.Listen(1 << 20), server: server}
	go func() { _ = server.Serve(r.listener) }()
	t.Cleanup(r.stop)
	return r, tm
}

// 207.8 — QueryLBTS with no regulators returns (_, false, nil).
func TestQueryLBTSEmpty(t *testing.T) {
	_, _, fed := timeFixture(t)
	_, finite, err := fed.QueryLBTS(context.Background())
	if err != nil {
		t.Fatalf("QueryLBTS: %v", err)
	}
	if finite {
		t.Errorf("Finite = true, want false (no regulators)")
	}
}

// 207.9 — ModifyLookahead → QueryLookahead reflects new value.
func TestModifyLookaheadRoundTrip(t *testing.T) {
	_, _, fed := timeFixture(t)
	ctx := context.Background()
	if err := fed.EnableTimeRegulation(ctx, 1.0); err != nil {
		t.Fatalf("EnableTimeRegulation: %v", err)
	}
	if err := fed.ModifyLookahead(ctx, 2.5); err != nil {
		t.Fatalf("ModifyLookahead: %v", err)
	}
	la, err := fed.QueryLookahead(ctx)
	if err != nil {
		t.Fatalf("QueryLookahead: %v", err)
	}
	if la != 2.5 {
		t.Errorf("post-ModifyLookahead = %v, want 2.5", la)
	}
}

// 207.10 — After DisableRegulation, QueryLookahead returns the typed error.
func TestQueryLookaheadAfterDisable(t *testing.T) {
	_, _, fed := timeFixture(t)
	ctx := context.Background()
	if err := fed.EnableTimeRegulation(ctx, 1.0); err != nil {
		t.Fatalf("EnableTimeRegulation: %v", err)
	}
	if err := fed.DisableTimeRegulation(ctx); err != nil {
		t.Fatalf("DisableTimeRegulation: %v", err)
	}
	_, err := fed.QueryLookahead(ctx)
	if !errors.Is(err, ErrTimeRegulationNotEnabled) {
		t.Errorf("post-disable QueryLookahead err = %v, want ErrTimeRegulationNotEnabled", err)
	}
}

// newTimeAwareTestRtid extends newTestRtid with the time service composed.
// The base fixture passes Options{} without Time, which means timeService
// is nil and the gRPC dispatch returns Unimplemented. Time-mgmt tests
// need a real *time.Manager wired in.
func newTimeAwareTestRtid(t *testing.T) *testRtid {
	return newTestRtidWithTime(t)
}

// (newTestRtidWithTime is defined alongside newTestRtid in
// federate_test.go's _test package; declared here to keep the time
// fixture declaration close to its callers.)
var _ = grpc.NewServer // keep grpc import tested through this declaration
