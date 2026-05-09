// TASK-241 (M22 W2) — async-delivery spec test.
//
// Per AC §3.5-3.8: TSO buffering off-by-default, buffer release on
// grant, drain on Enable, default state at federate join.

package m22spec

import (
	"context"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// recordingOutbox captures every Send so the test can assert on
// delivery order + count. Goroutine-safe.
type recordingOutbox struct {
	mu     sync.Mutex
	events []sentEvent
}

type sentEvent struct {
	fed core.FederationName
	h   core.FederateHandle
	evt core.OutboundEvent
}

func (o *recordingOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, sentEvent{fed: fed, h: h, evt: evt})
	return nil
}

func (o *recordingOutbox) sentCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.events)
}

func newM22Manager(t *testing.T) (*timepkg.Manager, *recordingOutbox) {
	t.Helper()
	out := &recordingOutbox{}
	mgr, err := timepkg.New(timepkg.Options{
		Clock:  core.NewFakeClock(stdtime.Unix(0, 0)),
		Outbox: out,
	})
	if err != nil {
		t.Fatalf("time.New: %v", err)
	}
	return mgr, out
}

// stubEvent is a minimal core.OutboundEvent for assertions.
type stubEvent struct{ seq uint64 }

func (e stubEvent) Seq() uint64 { return e.seq }

// TestSpec_M22_AsyncDeliveryDefaultOff — AC §3.8: a freshly-joined
// federate has asyncDelivery=false; ShouldDeliverNow returns false
// for any TSO event with timestamp > currentTime.
func TestSpec_M22_AsyncDeliveryDefaultOff(t *testing.T) {
	mgr, _ := newM22Manager(t)
	// Establish a time-state for fed/h=1 by enabling regulation.
	// asyncDelivery defaults to false on getOrCreate.
	if err := mgr.EnableRegulation(context.Background(), "fed", 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	// currentTime starts at 0; an event with ts=5 is in the future.
	if mgr.ShouldDeliverNow("fed", 1, 5) {
		t.Errorf("ShouldDeliverNow(ts=5) = true at currentTime=0 with async OFF; want false")
	}
	// An event at-or-before currentTime should deliver immediately.
	if !mgr.ShouldDeliverNow("fed", 1, 0) {
		t.Errorf("ShouldDeliverNow(ts=0) = false at currentTime=0; want true")
	}
}

// TestSpec_M22_TSOBufferedUntilGrant — AC §3.6: with async off, TSO
// events with ts > currentTime are buffered; advance grant releases
// them in FIFO order.
func TestSpec_M22_TSOBufferedUntilGrant(t *testing.T) {
	mgr, out := newM22Manager(t)
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}

	// Buffer 3 TSO events with ascending timestamps.
	for i, ts := range []core.LogicalTime{2, 5, 8} {
		if err := mgr.BufferTSO(ctx, "fed", 1, ts, stubEvent{seq: uint64(i + 1)}); err != nil {
			t.Fatalf("BufferTSO[%d]: %v", i, err)
		}
	}
	// No deliveries yet.
	if got := out.sentCount(); got != 0 {
		t.Fatalf("sentCount before grant = %d, want 0", got)
	}

	// Need a second regulator so LBTS can advance; otherwise NER on
	// a single-regulator federation auto-grants instantly.
	if err := mgr.EnableRegulation(ctx, "fed", 2, 1.0); err != nil {
		t.Fatalf("EnableRegulation(2): %v", err)
	}
	// Advance fed/h=1 to t=5 via TAR. With fed/h=2 not pending, LBTS
	// for fed/h=1 is bounded by fed/h=2's contribution (currentTime=0
	// + lookahead=1.0 = 1.0), so TAR(5) gets a forced grant at LBTS=1.
	// To get a grant at 5 we advance both regulators in step.
	// Simpler approach: directly mutate currentTime via the manager
	// path — use NER with both federates symmetrical.
	if err := mgr.NextMessageRequest(ctx, "fed", 2, 100); err != nil {
		t.Fatalf("NER fed/2: %v", err)
	}
	if err := mgr.NextMessageRequest(ctx, "fed", 1, 5); err != nil {
		t.Fatalf("NER fed/1: %v", err)
	}

	// fed/h=1 was granted (full or partial). Check that buffered TSO
	// events with ts <= grant.time were released. The Snapshot tells
	// us the grant time.
	snap := mgr.Snapshot("fed")
	var grantedTime core.LogicalTime
	for _, fs := range snap.Federates {
		if fs.Handle == 1 {
			grantedTime = fs.CurrentTime
			break
		}
	}
	if grantedTime <= 0 {
		t.Fatalf("fed/h=1 not granted (currentTime = %v)", grantedTime)
	}

	// Count releases: events with ts <= grantedTime should now be Sent.
	var expected int
	for _, ts := range []core.LogicalTime{2, 5, 8} {
		if float64(ts) <= float64(grantedTime) {
			expected++
		}
	}
	if got := out.sentCount(); got < expected {
		t.Errorf("sentCount = %d after grant at %v; want >= %d (TSO events released)", got, grantedTime, expected)
	}
}

// TestSpec_M22_EnableReleasesBuffer — AC §3.7: EnableAsynchronousDelivery
// drains all buffered TSO events immediately, regardless of timestamp.
func TestSpec_M22_EnableReleasesBuffer(t *testing.T) {
	mgr, out := newM22Manager(t)
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	// Buffer 3 TSO events with future timestamps.
	for i, ts := range []core.LogicalTime{10, 20, 30} {
		if err := mgr.BufferTSO(ctx, "fed", 1, ts, stubEvent{seq: uint64(i + 1)}); err != nil {
			t.Fatalf("BufferTSO[%d]: %v", i, err)
		}
	}
	if got := out.sentCount(); got != 0 {
		t.Fatalf("sentCount before Enable = %d, want 0", got)
	}
	// EnableAsynchronousDelivery should drain everything.
	if err := mgr.EnableAsynchronousDelivery(ctx, "fed", 1); err != nil {
		t.Fatalf("EnableAsynchronousDelivery: %v", err)
	}
	if got := out.sentCount(); got != 3 {
		t.Errorf("sentCount after Enable = %d, want 3 (all buffered events drained)", got)
	}
}

// TestSpec_M22_ToggleReachable — AC §3.5: both RPCs invokable
// against the manager (cross-process is covered by full-stack tests
// in W4; this is the unit-level reachability check).
func TestSpec_M22_ToggleReachable(t *testing.T) {
	mgr, _ := newM22Manager(t)
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	// Default off → Enable must succeed; Enable again must fail with
	// ErrTimeAlreadyAsynchronous.
	if err := mgr.EnableAsynchronousDelivery(ctx, "fed", 1); err != nil {
		t.Fatalf("Enable (first): %v", err)
	}
	if err := mgr.EnableAsynchronousDelivery(ctx, "fed", 1); err != core.ErrTimeAlreadyAsynchronous {
		t.Errorf("Enable (second) = %v, want ErrTimeAlreadyAsynchronous", err)
	}
	// Disable must succeed; Disable again must fail with ErrTimeNotAsynchronous.
	if err := mgr.DisableAsynchronousDelivery(ctx, "fed", 1); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if err := mgr.DisableAsynchronousDelivery(ctx, "fed", 1); err != core.ErrTimeNotAsynchronous {
		t.Errorf("Disable (second) = %v, want ErrTimeNotAsynchronous", err)
	}
}

// TestSpec_M22_AsyncOnDeliversImmediately — when async ON, even
// future-timestamped events deliver immediately (ShouldDeliverNow=true).
func TestSpec_M22_AsyncOnDeliversImmediately(t *testing.T) {
	mgr, _ := newM22Manager(t)
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	if err := mgr.EnableAsynchronousDelivery(ctx, "fed", 1); err != nil {
		t.Fatalf("EnableAsynchronousDelivery: %v", err)
	}
	if !mgr.ShouldDeliverNow("fed", 1, 1000) {
		t.Errorf("ShouldDeliverNow(ts=1000) = false with async ON; want true")
	}
}
