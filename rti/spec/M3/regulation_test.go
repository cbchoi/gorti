package m3spec

import (
	"context"
	"errors"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// newTestTimeManager builds a time.Manager with FakeClock + permissive
// fixtures. Returns the manager (or nil if New stub still returns
// ErrNotImplemented), the outbox (for grant/halt assertions), and the
// log (for event-record assertions).
func newTestTimeManager(t *testing.T) (*timepkg.Manager, *fakeOutbox, *permissiveEventLog) {
	t.Helper()
	outbox := newFakeOutbox()
	log := newPermissiveEventLog()
	mgr, err := timepkg.New(timepkg.Options{
		Clock:        core.NewFakeClock(stdtime.Unix(0, 0)),
		Outbox:       outbox,
		EventLog:     log,
		StallTimeout: 60 * stdtime.Second,
	})
	if err != nil {
		t.Logf("time.New returned: %v (expected during M3 RED phase)", err)
	}
	return mgr, outbox, log
}

// TestSpec_M3_EnableRegulation_Happy: a fresh federate becomes
// regulating with the supplied lookahead.
//
// Implements: FR-TM-1.
func TestSpec_M3_EnableRegulation_Happy(t *testing.T) {
	mgr, _, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	if err := mgr.EnableRegulation(context.Background(), "fed", 1, core.LogicalTime(1.0)); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
}

// TestSpec_M3_EnableRegulation_Twice: re-enabling on an already-
// regulating federate returns core.ErrTimeAlreadyRegulating.
//
// Implements: FR-TM-1.
func TestSpec_M3_EnableRegulation_Twice(t *testing.T) {
	mgr, _, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	if err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0)); err != nil {
		t.Fatalf("first enable: %v", err)
	}
	err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0))
	if !errors.Is(err, core.ErrTimeAlreadyRegulating) {
		t.Errorf("re-enable: err = %v, want ErrTimeAlreadyRegulating", err)
	}
}

// TestSpec_M3_EnableRegulation_RejectsInvalidLookahead: lookahead < 0
// or NaN is rejected with core.ErrTimeInvalidLookahead.
//
// Implements: FR-TM-1.
func TestSpec_M3_EnableRegulation_RejectsInvalidLookahead(t *testing.T) {
	mgr, _, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()

	// Negative lookahead
	err := mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(-0.5))
	if !errors.Is(err, core.ErrTimeInvalidLookahead) {
		t.Errorf("negative lookahead: err = %v, want ErrTimeInvalidLookahead", err)
	}

	// NaN lookahead
	nanLookahead := core.LogicalTime(0)
	*(*float64)((*float64)(&nanLookahead)) = nan()
	err = mgr.EnableRegulation(ctx, "fed", 2, nanLookahead)
	if !errors.Is(err, core.ErrTimeInvalidLookahead) {
		t.Errorf("NaN lookahead: err = %v, want ErrTimeInvalidLookahead", err)
	}
}

// nan returns a NaN float64 without importing math (keeps fixtures.go
// dependency-light).
func nan() float64 {
	var z float64 = 0
	return z / z
}

// TestSpec_M3_DisableRegulation_Happy: a regulating federate can be
// disabled.
//
// Implements: FR-TM-1.
func TestSpec_M3_DisableRegulation_Happy(t *testing.T) {
	mgr, _, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1.0))
	if err := mgr.DisableRegulation(ctx, "fed", 1); err != nil {
		t.Errorf("DisableRegulation: %v", err)
	}
}

// TestSpec_M3_DisableRegulation_NotRegulating: disabling a federate
// that isn't regulating returns core.ErrTimeNotRegulating.
//
// Implements: FR-TM-1.
func TestSpec_M3_DisableRegulation_NotRegulating(t *testing.T) {
	mgr, _, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	err := mgr.DisableRegulation(context.Background(), "fed", 99)
	if !errors.Is(err, core.ErrTimeNotRegulating) {
		t.Errorf("disable non-regulating: err = %v, want ErrTimeNotRegulating", err)
	}
}

// TestSpec_M3_EnableConstrained_Happy + Twice + DisableConstrained:
// symmetric to regulating; stable per-federate state machine.
//
// Implements: FR-TM-1.
func TestSpec_M3_EnableConstrained_Happy(t *testing.T) {
	mgr, _, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	if err := mgr.EnableConstrained(context.Background(), "fed", 1); err != nil {
		t.Errorf("EnableConstrained: %v", err)
	}
}

func TestSpec_M3_EnableConstrained_Twice(t *testing.T) {
	mgr, _, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	_ = mgr.EnableConstrained(ctx, "fed", 1)
	err := mgr.EnableConstrained(ctx, "fed", 1)
	if !errors.Is(err, core.ErrTimeAlreadyConstrained) {
		t.Errorf("re-enable: err = %v, want ErrTimeAlreadyConstrained", err)
	}
}

func TestSpec_M3_DisableConstrained_NotConstrained(t *testing.T) {
	mgr, _, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	err := mgr.DisableConstrained(context.Background(), "fed", 99)
	if !errors.Is(err, core.ErrTimeNotConstrained) {
		t.Errorf("disable non-constrained: err = %v, want ErrTimeNotConstrained", err)
	}
}

// TestSpec_M3_PerFederationIsolation: regulating in fedA does not leak
// into fedB.
//
// Implements: FR-TM-1.
func TestSpec_M3_PerFederationIsolation(t *testing.T) {
	mgr, _, _ := newTestTimeManager(t)
	if mgr == nil {
		t.Skip("time.Manager not yet wired")
	}
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fedA", 1, core.LogicalTime(1.0))

	// fedB sees federate 1 as not-regulating.
	err := mgr.DisableRegulation(ctx, "fedB", 1)
	if !errors.Is(err, core.ErrTimeNotRegulating) {
		t.Errorf("fedB.DisableRegulation: err = %v, want ErrTimeNotRegulating", err)
	}
}
