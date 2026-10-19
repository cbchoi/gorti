package m3spec

import (
	"context"
	"errors"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

// newStallTestManager builds a Manager configured with a short stall
// timeout (5 fake seconds) so tests can advance the ManualClock past it
// quickly. Returns the manager, ManualClock (for explicit Advance), the
// outbox (FederationHalted detection), and the event log (record
// detection).
func newStallTestManager(t *testing.T, stallTimeout stdtime.Duration) (*timepkg.Manager, *core.ManualClock, *fakeOutbox, *permissiveEventLog) {
	t.Helper()
	clk := core.NewManualClock(stdtime.Unix(0, 0))
	outbox := newFakeOutbox()
	log := newPermissiveEventLog()
	mgr, err := timepkg.New(timepkg.Options{
		Clock:        clk,
		Outbox:       outbox,
		EventLog:     log,
		StallTimeout: stallTimeout,
	})
	if err != nil {
		t.Logf("time.New returned: %v (expected during M3 RED phase)", err)
	}
	return mgr, clk, outbox, log
}

// TestSpec_M3_Stall_NoFederate_NoHalt: CheckStalls on an empty manager
// is a clean no-op (no halt, no panic).
//
// Implements: FR-TM-6.
func TestSpec_M3_Stall_NoFederate_NoHalt(t *testing.T) {
	mgr, clk, outbox, _ := newStallTestManager(t, 5*stdtime.Second)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	clk.Advance(60 * stdtime.Second) // long after timeout
	halted := mgr.CheckStalls(context.Background())
	if halted != 0 {
		t.Errorf("CheckStalls on empty manager: halted = %d, want 0", halted)
	}
	if got := len(outbox.Sent()); got != 0 {
		t.Errorf("outbox emissions on empty manager: got %d, want 0", got)
	}
}

// TestSpec_M3_Stall_BeforeTimeout_NoHalt: a federate that NER'd recently
// (within the timeout window) is NOT halted. Specifically: NER at fake
// time t=0, advance by half the timeout, CheckStalls should report 0.
//
// Implements: FR-TM-6.
func TestSpec_M3_Stall_BeforeTimeout_NoHalt(t *testing.T) {
	mgr, clk, outbox, _ := newStallTestManager(t, 10*stdtime.Second)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0)); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	if err := mgr.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnableRegulation(ctx, "fed", 2, 1); err != nil {
		t.Fatal(err)
	}
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(5.0)); err != nil {
		t.Fatalf("NER: %v", err)
	}
	if state := mgr.Metrics("fed", 1); !state.Advancing || state.CurrentTime != 0 {
		t.Fatalf("NER must be pending before checking timeout: %+v", state)
	}

	clk.Advance(4 * stdtime.Second) // 4 < 10 → no stall
	halted := mgr.CheckStalls(ctx)
	if halted != 0 {
		t.Errorf("CheckStalls within timeout window: halted = %d, want 0", halted)
	}
	if sent := outbox.Sent(); len(sent) != 0 {
		t.Fatalf("pending request emitted a grant or halt before timeout: %+v", sent)
	}
}

// TestSpec_M3_Stall_PastTimeout_HaltsFederation: NER at t=0, advance
// past timeout, CheckStalls returns 1, FederationHalted event emitted
// to the outbox naming federate 1, EventLog records the halt.
//
// Implements: FR-TM-6, NFR-PERF-3.
func TestSpec_M3_Stall_PastTimeout_HaltsFederation(t *testing.T) {
	mgr, clk, outbox, log := newStallTestManager(t, 5*stdtime.Second)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0))
	_ = mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(1.0))
	if err := mgr.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	// fed1 NER's; fed2 never NER's, blocking fed1's grant.
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(10.0)); err != nil {
		t.Fatal(err)
	}
	if state := mgr.Metrics("fed", 1); !state.Advancing || state.CurrentTime != 0 || len(outbox.Sent()) != 0 {
		t.Fatalf("NER must remain pending without a grant before timeout: %+v", state)
	}
	appendedBefore := len(log.Appended())

	// Advance well past the stall timeout.
	clk.Advance(10 * stdtime.Second)
	halted := mgr.CheckStalls(ctx)
	if halted != 1 {
		t.Errorf("CheckStalls past timeout: halted = %d, want 1", halted)
	}

	sent := outbox.Sent()
	if len(sent) != 2 {
		t.Fatalf("outbox after halt: got %d sends, want one per federate", len(sent))
	}
	for i, s := range sent {
		halt, ok := s.Event.(*timepkg.FederationHalted)
		if !ok || halt.Cause != timepkg.HaltCauseStall || halt.StalledFederate != 1 || s.Federate != core.FederateHandle(i+1) {
			t.Errorf("halt callback[%d] = %+v (%+v)", i, s, s.Event)
		}
	}

	// EventLog must record the halt.
	if got := len(log.Appended()) - appendedBefore; got != 1 {
		t.Errorf("EventLog after halt: got %d new appends, want 1 halt record", got)
	}
}

// TestSpec_M3_Stall_HaltedFederation_RejectsFurtherCalls: once a
// federation is halted, subsequent EnableRegulation / NER calls return
// core.ErrFederationHalted (terminal state).
//
// Implements: FR-TM-6.
func TestSpec_M3_Stall_HaltedFederation_RejectsFurtherCalls(t *testing.T) {
	mgr, clk, _, _ := newStallTestManager(t, 5*stdtime.Second)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0))
	_ = mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(1.0))
	if err := mgr.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(10.0)); err != nil {
		t.Fatal(err)
	}

	clk.Advance(10 * stdtime.Second)
	if halted := mgr.CheckStalls(ctx); halted != 1 {
		t.Fatalf("CheckStalls = %d, want 1", halted)
	}

	// Now both EnableRegulation and NER on the halted federation must
	// be rejected with ErrFederationHalted (or a related terminal
	// error code).
	err := mgr.EnableRegulation(ctx, "fed", 3, core.LogicalTime(1.0))
	if !errors.Is(err, core.ErrFederationHalted) {
		t.Errorf("EnableRegulation after halt: got %v, want ErrFederationHalted", err)
	}
	err = mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(11.0))
	if !errors.Is(err, core.ErrFederationHalted) {
		t.Errorf("NER after halt: got %v, want ErrFederationHalted", err)
	}
}

// TestSpec_M3_Stall_PerFederationIsolation: a halt in fedA does NOT
// affect fedB. fedB continues to accept EnableRegulation + NER.
//
// Implements: FR-TM-1, FR-TM-6.
func TestSpec_M3_Stall_PerFederationIsolation(t *testing.T) {
	mgr, clk, _, _ := newStallTestManager(t, 5*stdtime.Second)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	// Halt fedA.
	_ = mgr.EnableRegulation(ctx, "fedA", 1, core.LogicalTime(1.0))
	_ = mgr.EnableRegulation(ctx, "fedA", 2, core.LogicalTime(1.0))
	if err := mgr.EnableConstrained(ctx, "fedA", 1); err != nil {
		t.Fatal(err)
	}
	if err := mgr.NextMessageRequest(ctx, "fedA", 1, core.LogicalTime(10.0)); err != nil {
		t.Fatal(err)
	}
	clk.Advance(10 * stdtime.Second)
	if halted := mgr.CheckStalls(ctx); halted != 1 {
		t.Fatalf("CheckStalls = %d, want fedA halted", halted)
	}

	// fedB should still be operational.
	if err := mgr.EnableRegulation(ctx, "fedB", 1, core.LogicalTime(1.0)); err != nil {
		t.Errorf("fedB.EnableRegulation after fedA halt: %v (want nil — isolation)", err)
	}
	if err := mgr.NextMessageRequest(ctx, "fedB", 1, 5); err != nil {
		t.Fatal(err)
	}
	if state := mgr.Metrics("fedB", 1); state.CurrentTime != 5 || state.Advancing {
		t.Fatalf("fedB did not advance after fedA halt: %+v", state)
	}
}

// TestSpec_M3_Stall_DefaultTimeout_Is60Seconds: per docs/srs.md M3 exit
// criterion the default stall timeout when Options.StallTimeout == 0
// is 60 seconds. The constant is exported (timepkg.DefaultStallTimeout)
// so this test asserts the documented value.
//
// Implements: FR-TM-6.
func TestSpec_M3_Stall_DefaultTimeout_Is60Seconds(t *testing.T) {
	if timepkg.DefaultStallTimeout != 60*stdtime.Second {
		t.Errorf("DefaultStallTimeout = %v, want 60s (per srs.md M3 exit criterion)",
			timepkg.DefaultStallTimeout)
	}
}
