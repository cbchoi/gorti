// TASK-205 (M21) — see docs/M21_DISPATCH_PLAN.md §6.
//
// Verifies the W2B wiring: rtid composes timeMgr (TASK-204), stream
// conversion handles time events (TASK-204b — tested in
// rti/internal/transport/grpc/stream_test.go), and the OnFederateResign
// hook drops pending state (TASK-204c).
//
// The "grant arrives on the wire" end-to-end cases (205.1-205.5,
// 205.9, 205.10) are deferred to the W4A go-timed runner test
// (TASK-211) — those need a full bufconn-rtid + federate-loop
// fixture that's better expressed at the example level than at
// cmd/rtid's package-private layer.

package main

import (
	"context"
	"io"
	"log/slog"
	"runtime"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// 205.composes — newRTID composes a non-nil time manager and threads
// it through both the gRPC server and the AdminService chain.
func TestRTIDComposesTimeManager(t *testing.T) {
	srv, err := newRTID(rtidConfig{
		ListenAddr:        ":0",
		MetricsListenAddr: ":0",
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("newRTID: %v", err)
	}
	if srv.timeMgr == nil {
		t.Fatalf("rtid composed without a time manager (M21 TASK-204 regression)")
	}
}

// 205.6 — OnFederateResign drops pending NER state.
func TestOnFederateResign_ClearsPendingNER(t *testing.T) {
	mgr := newTimeMgrForTest(t)
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, 0); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	if err := mgr.EnableRegulation(ctx, "fed", 2, 0); err != nil {
		t.Fatalf("EnableRegulation 2: %v", err)
	}
	// Two regulators with lookahead=0 → fed 1's NER stays pending
	// (no sole-pending forced grant; LBTS = 0 = currentTime).
	if err := mgr.NextMessageRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatalf("NER: %v", err)
	}
	// Verify pending state is recorded.
	snap := mgr.Snapshot("fed")
	var preFed1Pending bool
	for _, fst := range snap.Federates {
		if fst.Handle == 1 && fst.HasPendingRequest {
			preFed1Pending = true
		}
	}
	if !preFed1Pending {
		t.Fatalf("expected fed 1 to have pending request before resign; snapshot=%+v", snap.Federates)
	}
	// Resign fed 1.
	mgr.OnFederateResign(ctx, "fed", 1)
	snap = mgr.Snapshot("fed")
	for _, fst := range snap.Federates {
		if fst.Handle == 1 {
			t.Errorf("fed 1 still in snapshot after OnFederateResign: %+v", fst)
		}
	}
}

// 205.7 — OnFederateResign drops pending TAR state.
func TestOnFederateResign_ClearsPendingTAR(t *testing.T) {
	mgr := newTimeMgrForTest(t)
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fed", 1, 0)
	_ = mgr.EnableRegulation(ctx, "fed", 2, 0)
	if err := mgr.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatalf("TAR: %v", err)
	}
	mgr.OnFederateResign(ctx, "fed", 1)
	snap := mgr.Snapshot("fed")
	for _, fst := range snap.Federates {
		if fst.Handle == 1 {
			t.Errorf("fed 1 still in snapshot after OnFederateResign: %+v", fst)
		}
	}
}

// 205.8 — OnFederateResign drops pending NMRA, TARA, FQR — sub-tests.
func TestOnFederateResign_ClearsPendingOtherPrimitives(t *testing.T) {
	cases := map[string]func(*timepkg.Manager) error{
		"NMRA": func(m *timepkg.Manager) error {
			return m.NextMessageRequestAvailable(context.Background(), "fed", 1, 10)
		},
		"TARA": func(m *timepkg.Manager) error {
			return m.TimeAdvanceRequestAvailable(context.Background(), "fed", 1, 10)
		},
		"FQR": func(m *timepkg.Manager) error {
			return m.FlushQueueRequest(context.Background(), "fed", 1, 10)
		},
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			mgr := newTimeMgrForTest(t)
			ctx := context.Background()
			_ = mgr.EnableRegulation(ctx, "fed", 1, 0)
			_ = mgr.EnableRegulation(ctx, "fed", 2, 0)
			if err := fn(mgr); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			mgr.OnFederateResign(ctx, "fed", 1)
			snap := mgr.Snapshot("fed")
			for _, fst := range snap.Federates {
				if fst.Handle == 1 {
					t.Errorf("[%s] fed 1 still in snapshot after OnFederateResign: %+v", name, fst)
				}
			}
		})
	}
}

// 205.leak — Repeated NER + OnFederateResign cycles do not leak goroutines.
// Rough leak detector: snapshot goroutine count, run 100 cycles, snapshot
// again. The manager's stall ticker is a known constant background
// goroutine; otherwise the count should be stable.
func TestOnFederateResign_NoGoroutineLeak(t *testing.T) {
	mgr := newTimeMgrForTest(t)
	ctx := context.Background()
	// Establish baseline after a single round-trip so any lazy goroutine
	// (event-log writer etc.) is already spawned.
	_ = mgr.EnableRegulation(ctx, "fed", 1, 0)
	_ = mgr.EnableRegulation(ctx, "fed", 2, 0)
	_ = mgr.NextMessageRequest(ctx, "fed", 1, 10)
	mgr.OnFederateResign(ctx, "fed", 1)
	stdtime.Sleep(20 * stdtime.Millisecond) // let any deferred work settle
	baseline := runtime.NumGoroutine()

	for i := 0; i < 100; i++ {
		_ = mgr.EnableRegulation(ctx, "fed", core.FederateHandle(100+i), 0)
		_ = mgr.NextMessageRequest(ctx, "fed", core.FederateHandle(100+i), 10)
		mgr.OnFederateResign(ctx, "fed", core.FederateHandle(100+i))
	}
	stdtime.Sleep(20 * stdtime.Millisecond)
	final := runtime.NumGoroutine()

	// Allow ±2 slack — runtime.NumGoroutine includes test infra
	// goroutines that may flicker.
	if final > baseline+2 {
		t.Errorf("goroutine leak suspected: baseline=%d final=%d delta=%d",
			baseline, final, final-baseline)
	}
}

// newTimeMgrForTest builds a real *time.Manager with a fake clock and
// no-op outbox. Mirrors the regulation_test fixture pattern.
func newTimeMgrForTest(t *testing.T) *timepkg.Manager {
	t.Helper()
	mgr, err := timepkg.New(timepkg.Options{
		Clock:  core.NewFakeClock(stdtime.Unix(0, 0)),
		Outbox: nopOutbox{},
	})
	if err != nil {
		t.Fatalf("time.New: %v", err)
	}
	return mgr
}

// nopOutbox satisfies core.Outbox without recording.
type nopOutbox struct{}

func (nopOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	return nil
}
