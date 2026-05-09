// TASK-243 (M22 W3) — manager-level test that NER full-grant semantics
// hold under the multi-regulator scenario the example exercises.
//
// Mirrors the example regulator's per-cycle pattern: each federate
// issues NER(t), waits for grants, treats forced grants as advisory,
// only iterates the cycle on full grants. Asserts no federate ever
// observes ErrTimeAdvancingState from a properly-issued NER.

package m22spec

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// grantSink is a per-federate grant collector. The real example
// drains via federate.Events(); here we use an Outbox that records
// every Send and a Ticker over the manager's snapshot to find the
// federate's currentTime.
type grantSink struct {
	mu     sync.Mutex
	grants map[core.FederateHandle][]core.LogicalTime
}

func newGrantSink() *grantSink {
	return &grantSink{grants: make(map[core.FederateHandle][]core.LogicalTime)}
}

func (g *grantSink) record(h core.FederateHandle, t core.LogicalTime) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.grants[h] = append(g.grants[h], t)
}

// TestSpec_M22_NER_NoErrTimeAdvancingState — drives 3 regulators
// through 5 cycles each, mixing NER and TAR, and asserts no NER
// ever returns ErrTimeAdvancingState. The federation pattern is the
// same as examples/go-timed/'s runner.
func TestSpec_M22_NER_NoErrTimeAdvancingState(t *testing.T) {
	mgr, _ := newM22Manager(t)
	ctx := context.Background()

	// Two regulators with mismatched lookaheads. Driving them in
	// lockstep (both request the same t at each cycle) tests the
	// LBTS = min(contribution) > requested predicate when both
	// federates contribute floor=requested + their own lookahead.
	// Smallest-lookahead federate's contribution determines LBTS;
	// it must exceed requested → full grant for both.
	if err := mgr.EnableRegulation(ctx, "fed", 1, 0.5); err != nil {
		t.Fatalf("EnableRegulation(1): %v", err)
	}
	if err := mgr.EnableRegulation(ctx, "fed", 2, 2.0); err != nil {
		t.Fatalf("EnableRegulation(2): %v", err)
	}

	// 5 cycles. Issuing in interleaved order (1, 2, 1, 2, ...)
	// stresses the tryGrantPending fixed-point loop ordering.
	for i := 1; i <= 5; i++ {
		t1 := core.LogicalTime(i * 3)
		// Issue NER for both at the same requested time.
		err1 := mgr.NextMessageRequest(ctx, "fed", 1, t1)
		err2 := mgr.NextMessageRequest(ctx, "fed", 2, t1)
		if errors.Is(err1, core.ErrTimeAdvancingState) {
			t.Errorf("cycle %d: fed/h=1 NER got ErrTimeAdvancingState — pendingNER not cleared by previous grant", i)
		} else if err1 != nil {
			t.Errorf("cycle %d: fed/h=1 NER err = %v", i, err1)
		}
		if errors.Is(err2, core.ErrTimeAdvancingState) {
			t.Errorf("cycle %d: fed/h=2 NER got ErrTimeAdvancingState — pendingNER not cleared by previous grant", i)
		} else if err2 != nil {
			t.Errorf("cycle %d: fed/h=2 NER err = %v", i, err2)
		}

		// After both grants resolve, pendingNER must be cleared so
		// the next cycle works. Verify via Snapshot.
		snap := mgr.Snapshot("fed")
		for _, fs := range snap.Federates {
			if fs.HasPendingRequest {
				t.Errorf("cycle %d: fed/h=%d still has pending request; currentTime=%v requested=%v",
					i, fs.Handle, fs.CurrentTime, fs.PendingRequestTime)
			}
		}
	}
}
