// M25 Phase A — TSOGate contract for non-time-engaged federates.
//
// Regression for the M5 verbose-mode TSO drop: a federate that has
// not enabled time regulation or constrained has no logical time
// progression, so TSO ordering does not apply and events must
// deliver immediately. The M22 implementation defaulted to buffering
// for any federate with no nerState, which silently dropped TSO
// events for non-time-aware subscribers.

package m25spec

import (
	"context"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

type recordingOutbox struct {
	mu     sync.Mutex
	events int
}

func (o *recordingOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events++
	return nil
}

func newMgr(t *testing.T) *timepkg.Manager {
	t.Helper()
	mgr, err := timepkg.New(timepkg.Options{
		Clock:  core.NewFakeClock(stdtime.Unix(0, 0)),
		Outbox: &recordingOutbox{},
	})
	if err != nil {
		t.Fatalf("time.New: %v", err)
	}
	return mgr
}

// TestSpec_M25_NonTimeEngaged_DeliversImmediately — a federate that
// has not enabled regulation or constrained delivers TSO immediately.
func TestSpec_M25_NonTimeEngaged_DeliversImmediately(t *testing.T) {
	mgr := newMgr(t)
	if !mgr.ShouldDeliverNow("fed", 1, 42) {
		t.Errorf("non-time-engaged federate: ShouldDeliverNow(ts=42) = false; want true")
	}
	if !mgr.ShouldDeliverNow("fed", 1, 1e9) {
		t.Errorf("non-time-engaged federate: ShouldDeliverNow(ts=1e9) = false; want true")
	}
}

// TestSpec_M25_RegulatingFederate_StillBuffered — a regulating
// federate keeps the M22 buffering contract.
func TestSpec_M25_RegulatingFederate_StillBuffered(t *testing.T) {
	mgr := newMgr(t)
	if err := mgr.EnableRegulation(context.Background(), "fed", 1, 1.0); err != nil {
		t.Fatalf("EnableRegulation: %v", err)
	}
	if mgr.ShouldDeliverNow("fed", 1, 42) {
		t.Errorf("regulating federate at currentTime=0: ShouldDeliverNow(ts=42) = true; want false (M22 buffering)")
	}
}

// TestSpec_M25_ConstrainedFederate_StillBuffered — a constrained
// federate keeps the M22 buffering contract.
func TestSpec_M25_ConstrainedFederate_StillBuffered(t *testing.T) {
	mgr := newMgr(t)
	if err := mgr.EnableConstrained(context.Background(), "fed", 1); err != nil {
		t.Fatalf("EnableConstrained: %v", err)
	}
	if mgr.ShouldDeliverNow("fed", 1, 42) {
		t.Errorf("constrained federate at currentTime=0: ShouldDeliverNow(ts=42) = true; want false (M22 buffering)")
	}
}
