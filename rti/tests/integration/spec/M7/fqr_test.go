package m7spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

// FQR grants unconstrained requesters without waiting for a regulating peer.
//
// Implements: FR-TM-2 (M7 scope).
func TestSpec_M7_FQR_Unconstrained_GrantsDespitePeer(t *testing.T) {
	assertUnconstrainedAdvance(t, (*timepkg.Manager).FlushQueueRequest)
}

// TestSpec_M7_FQR_DrainsQueueAndGrants: per IEEE 1516.1-2010 §8.13,
// FlushQueueRequest drains the federate's TSO queue up to t and emits
// a grant when complete. Without pending messages, the grant should
// fire promptly (at min(t, LBTS) like NER).
//
// SCAFFOLD.
//
// Implements: FR-TM-2.
func TestSpec_M7_FQR_DrainsQueueAndGrants(t *testing.T) {
	mgr, outbox, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0)); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	err := mgr.FlushQueueRequest(ctx, "fed", 1, core.LogicalTime(5.0))
	if errors.Is(err, timepkg.ErrNotImplemented) {
		t.Skip("FQR not yet implemented (M7 RED state)")
	}
	if err != nil {
		t.Fatalf("FQR: %v", err)
	}
	sent := outbox.SentTo("fed", 1)
	if len(sent) == 0 {
		t.Fatal("FQR with empty queue: expected immediate grant; got 0 sends")
	}
}
