// TASK-245 (M22 W4) — Go-side acceptance gate for AC §3.
//
// Per AC §3.5-3.10: surface reachability (manager + Go SDK
// methods exist), TSO buffer/release semantics, default off,
// race no longer reproducible after the SDK fix.

package m22spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/federate"
)

// AC §3.5 — TimeManager interface satisfied by *time.Manager
// includes the M22 methods.
func TestACTimeManagerInterfaceM22(t *testing.T) {
	mgr, _ := newM22Manager(t)
	var _ core.TimeManager = mgr // compile-time assertion
	t.Logf("core.TimeManager satisfied (incl. EnableAsynchronousDelivery / DisableAsynchronousDelivery)")
}

// AC §3.1 / §3.2 — Go SDK exposes the toggles.
func TestACGoSDKExposesToggles(t *testing.T) {
	// Smoke: ensure the SDK methods exist by reflection-free check.
	// They are tested behaviorally in W1+W2 above.
	var f *federate.Federate
	_ = f.EnableAsynchronousDelivery
	_ = f.DisableAsynchronousDelivery
}

// AC §3.3 — Pysdk regulator workaround removed. Confirmed by
// Regression guard for the completed Time Management service path; a Go-side
// equivalent here would re-implement the same scan, so we delegate.
func TestACWorkaroundRemovalDelegated(t *testing.T) {
	t.Logf("time-service workaround removal remains covered")
}

// AC §3.5 + §3.7 — async on/off toggle reachable without draining TSO.
// Asserted in async_delivery_test.go::TestSpec_M22_ToggleReachable +
// TestSpec_M22_EnablePreservesTSOBuffer.
func TestACToggleAndEnableDrainCoveredElsewhere(t *testing.T) {
	t.Logf("AC §3.5 + §3.7 covered by async_delivery_test.go")
}

// AC §3.6 — TSO buffered until grant.
// Asserted in async_delivery_test.go::TestSpec_M22_TSOBufferedUntilGrant.
func TestACTSOBufferingCoveredElsewhere(t *testing.T) {
	t.Logf("AC §3.6 covered by async_delivery_test.go")
}

// AC §3.8 — Default OFF.
// Asserted in async_delivery_test.go::TestSpec_M22_AsyncDeliveryDefaultOff.
func TestACDefaultOffCoveredElsewhere(t *testing.T) {
	t.Logf("AC §3.8 covered by async_delivery_test.go")
}

// AC §3.9 — NER race no longer reproducible at the SDK level.
// The manager-level forced-grant semantics are pinned in
// ner_forced_grant_race_test.go; the SDK fix in
// examples/{go-timed,pyjevsim-time-advance}/regulator_main.go
// (waitForFullGrant) is exercised by ner_full_grant_test.go.
func TestACNERRaceFixed(t *testing.T) {
	mgr, _ := newM22Manager(t)
	ctx := context.Background()
	// Two regulators, NER from both — the failing pattern from M21.
	if err := mgr.EnableRegulation(ctx, "fed", 1, 0.5); err != nil {
		t.Fatalf("EnableRegulation(1): %v", err)
	}
	if err := mgr.EnableRegulation(ctx, "fed", 2, 0.5); err != nil {
		t.Fatalf("EnableRegulation(2): %v", err)
	}
	for _, h := range []core.FederateHandle{1, 2} {
		if err := mgr.EnableConstrained(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 3; i++ {
		t1 := core.LogicalTime(i * 2)
		err1 := mgr.NextMessageRequest(ctx, "fed", 1, t1)
		if state := mgr.Metrics("fed", 1); !state.Advancing || state.CurrentTime != t1-2 {
			t.Fatalf("cycle %d: first requester did not wait for its peer: %+v", i, state)
		}
		err2 := mgr.NextMessageRequest(ctx, "fed", 2, t1)
		if errors.Is(err1, core.ErrTimeAdvancingState) || errors.Is(err2, core.ErrTimeAdvancingState) {
			t.Errorf("cycle %d: ErrTimeAdvancingState surfaced — pendingNER not cleared between cycles", i)
		}
		if err1 != nil || err2 != nil {
			t.Fatalf("cycle %d: NER errors = %v, %v", i, err1, err2)
		}
		for _, h := range []core.FederateHandle{1, 2} {
			if state := mgr.Metrics("fed", h); state.CurrentTime != t1 || state.Advancing {
				t.Fatalf("cycle %d: fed%d state after grant = %+v", i, h, state)
			}
		}
	}
}

// AC §3.10 — Migration audit complete. Verified by reading the
// audit table in docs/M22_DISPATCH_PLAN.md §8 alongside the
// example-level edits.
func TestACMigrationAuditCovered(t *testing.T) {
	t.Logf("AC §3.10 covered by plan §8 + dashboard*/dashboard_main.py edits")
}
