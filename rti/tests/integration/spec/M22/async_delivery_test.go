// TASK-241 (M22 W2) — async-delivery spec test.
//
// IEEE 1516.1-2010 8.1.7: asynchronous delivery does not bypass TSO ordering.

package m22spec

import (
	"context"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
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
	// Observe message/grant ordering after activation completes.
	switch evt.(type) {
	case *timepkg.TimeRegulationEnabled, *timepkg.TimeConstrainedEnabled:
		return nil
	}
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

func (o *recordingOutbox) sentTo(fed core.FederationName, h core.FederateHandle) []core.OutboundEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	var events []core.OutboundEvent
	for _, sent := range o.events {
		if sent.fed == fed && sent.h == h {
			events = append(events, sent.evt)
		}
	}
	return events
}

func newM22Manager(t *testing.T) (*timepkg.Manager, *recordingOutbox) {
	t.Helper()
	out := &recordingOutbox{}
	mgr, err := timepkg.New(timepkg.Options{
		Clock:  core.NewManualClock(stdtime.Unix(0, 0)),
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
// constrained federate has asyncDelivery=false; even equal-current TSO waits
// for an advance and is delivered before the grant.
func TestSpec_M22_AsyncDeliveryDefaultOff(t *testing.T) {
	mgr, out := newM22Manager(t)
	ctx := context.Background()
	// Establish a constrained receiver.
	// asyncDelivery defaults to false on getOrCreate.
	if err := mgr.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatalf("EnableConstrained: %v", err)
	}
	for i, ts := range []core.LogicalTime{0, 5} {
		if mgr.ShouldDeliverNow("fed", 1, ts) {
			t.Errorf("ShouldDeliverNow(ts=%v) = true while time-granted; want false", ts)
		}
		if err := mgr.BufferTSO(ctx, "fed", 1, ts, stubEvent{seq: uint64(i + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	if state := mgr.Metrics("fed", 1); state.AsynchronousDelivery || state.CurrentTime != 0 || state.TSOQueued != 2 || out.sentCount() != 0 {
		t.Fatalf("default-off TSO escaped before advance: %+v", state)
	}
	for i, target := range []core.LogicalTime{0, 5} {
		if err := mgr.TimeAdvanceRequest(ctx, "fed", 1, target); err != nil {
			t.Fatalf("TAR(%v): %v", target, err)
		}
		events := out.sentTo("fed", 1)
		if len(events) != 2*(i+1) {
			t.Fatalf("TAR(%v) callbacks = %+v, want message then grant", target, events)
		}
		if event, ok := events[2*i].(stubEvent); !ok || event.seq != uint64(i+1) {
			t.Fatalf("callback[%d] = %+v, want TSO message %d", 2*i, events[2*i], i+1)
		}
		if grant, ok := events[2*i+1].(*timepkg.TimeAdvanceGrant); !ok || grant.Time != target {
			t.Fatalf("callback[%d] = %+v, want TAG(%v)", 2*i+1, events[2*i+1], target)
		}
		if state := mgr.Metrics("fed", 1); state.CurrentTime != target || state.Advancing || state.TSOQueued != uint64(1-i) || state.AsynchronousDelivery {
			t.Fatalf("state after TAR(%v) = %+v", target, state)
		}
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
	if err := mgr.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
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

	// An idle regulating peer holds the constrained receiver's TAR.
	if err := mgr.EnableRegulation(ctx, "fed", 2, 1.0); err != nil {
		t.Fatalf("EnableRegulation(2): %v", err)
	}
	if err := mgr.TimeAdvanceRequest(ctx, "fed", 1, 5); err != nil {
		t.Fatalf("TAR fed/1: %v", err)
	}
	if state := mgr.Metrics("fed", 1); !state.Advancing || state.CurrentTime != 0 || state.TSOQueued != 3 || out.sentCount() != 0 {
		t.Fatalf("held TAR released TSO or granted early: %+v", state)
	}
	// The regulating-only peer can advance without constraint, releasing TAR(5).
	if err := mgr.NextMessageRequest(ctx, "fed", 2, 100); err != nil {
		t.Fatalf("NER fed/2: %v", err)
	}
	events := out.sentTo("fed", 1)
	if len(events) != 3 {
		t.Fatalf("TAR(5) callbacks = %+v, want two messages then grant", events)
	}
	for i := 0; i < 2; i++ {
		if event, ok := events[i].(stubEvent); !ok || event.seq != uint64(i+1) {
			t.Fatalf("callback[%d] = %+v, want TSO message %d", i, events[i], i+1)
		}
	}
	if grant, ok := events[2].(*timepkg.TimeAdvanceGrant); !ok || grant.Time != 5 {
		t.Fatalf("callback[2] = %+v, want TAG(5)", events[2])
	}
	if state := mgr.Metrics("fed", 1); state.CurrentTime != 5 || state.Advancing || state.TSOQueued != 1 {
		t.Fatalf("TAR(5) did not retain the future message: %+v", state)
	}
	if err := mgr.TimeAdvanceRequest(ctx, "fed", 1, 8); err != nil {
		t.Fatal(err)
	}
	events = out.sentTo("fed", 1)
	if len(events) != 5 {
		t.Fatalf("TAR(8) callbacks = %+v, want final message then grant", events)
	}
	if event, ok := events[3].(stubEvent); !ok || event.seq != 3 {
		t.Fatalf("callback[3] = %+v, want final TSO message", events[3])
	}
	if grant, ok := events[4].(*timepkg.TimeAdvanceGrant); !ok || grant.Time != 8 {
		t.Fatalf("callback[4] = %+v, want TAG(8)", events[4])
	}
	if state := mgr.Metrics("fed", 1); state.CurrentTime != 8 || state.Advancing || state.TSOQueued != 0 {
		t.Fatalf("TAR(8) state = %+v", state)
	}
}

// Enabling asynchronous delivery must leave the constrained TSO queue intact.
func TestSpec_M22_EnablePreservesTSOBuffer(t *testing.T) {
	mgr, out := newM22Manager(t)
	ctx := context.Background()
	if err := mgr.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatalf("EnableConstrained: %v", err)
	}
	// Equal-current and future TSO must all survive the async toggle.
	for i, ts := range []core.LogicalTime{0, 20, 30} {
		if err := mgr.BufferTSO(ctx, "fed", 1, ts, stubEvent{seq: uint64(i + 1)}); err != nil {
			t.Fatalf("BufferTSO[%d]: %v", i, err)
		}
	}
	if got := out.sentCount(); got != 0 {
		t.Fatalf("sentCount before Enable = %d, want 0", got)
	}
	// The toggle does not grant permission to deliver future TSO messages.
	if err := mgr.EnableAsynchronousDelivery(ctx, "fed", 1); err != nil {
		t.Fatalf("EnableAsynchronousDelivery: %v", err)
	}
	if got := out.sentCount(); got != 0 {
		t.Errorf("sentCount after Enable = %d, want 0", got)
	}
	if err := mgr.TimeAdvanceRequest(ctx, "fed", 1, 30); err != nil {
		t.Fatal(err)
	}
	if got := out.sentCount(); got != 4 {
		t.Fatalf("sentCount after grant = %d, want three messages and TAG", got)
	}
	events := out.sentTo("fed", 1)
	for i := 0; i < 3; i++ {
		if event, ok := events[i].(stubEvent); !ok || event.seq != uint64(i+1) {
			t.Fatalf("callback[%d] = %+v, want TSO message %d before grant", i, events[i], i+1)
		}
	}
	if grant, ok := events[3].(*timepkg.TimeAdvanceGrant); !ok || grant.Time != 30 {
		t.Fatalf("callback[3] = %+v, want TAG(30)", events[3])
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

// A regulating-only recipient is not constrained, independently of async mode.
func TestSpec_M22_RegulatingOnlyDeliversInReceiveOrder(t *testing.T) {
	mgr, _ := newM22Manager(t)
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	if !mgr.ShouldDeliverNow("fed", 1, 1000) {
		t.Fatal("regulation must not constrain incoming messages with async OFF")
	}
	if err := mgr.EnableAsynchronousDelivery(ctx, "fed", 1); err != nil {
		t.Fatalf("EnableAsynchronousDelivery: %v", err)
	}
	if !mgr.ShouldDeliverNow("fed", 1, 1000) {
		t.Errorf("ShouldDeliverNow(ts=1000) = false with async ON; want true")
	}
}
