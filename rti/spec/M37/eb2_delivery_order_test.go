// M37 EB-2 — §8.14 delivery order (IEEE 1516.1-2010).
//
// §8.14 (time advance grant): when the RTI grants an advance to logical
// time t, the joined federate must ALREADY have received every TSO
// message with timestamp <= t. The grant is the federate's guarantee
// that no further TSO message at-or-before t will arrive; delivering
// buffered TSO after the grant violates that guarantee (a federate that
// acts on the grant immediately would miss the messages).
//
// gorti pre-M37 emitted the grant BEFORE draining the TSO buffer
// (emitGrant: Send(grant) → releaseBufferedTSO). All five advance
// primitives (NER/NMRA/TAR/TARA/FQR), forced grants, and the M36
// membership-event grants funnel through emitGrant, so the corrected
// ordering is asserted at that single point.

package m37spec

import (
	"context"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	hlatime "github.com/cbchoi/gorti/rti/internal/time"
)

// fakeTSOEvent is a minimal core.OutboundEvent stand-in for a buffered
// TSO delivery.
type fakeTSOEvent struct{ seq uint64 }

func (e *fakeTSOEvent) Seq() uint64 { return e.seq }

func newTimeManager(t *testing.T) (*hlatime.Manager, *recordingOutbox) {
	t.Helper()
	out := &recordingOutbox{}
	mgr, err := hlatime.New(hlatime.Options{
		Clock:  core.NewFakeClock(stdtime.Unix(0, 0)),
		Outbox: out,
	})
	if err != nil {
		t.Fatalf("time.New: %v", err)
	}
	return mgr, out
}

// indexOfGrantAndTSO scans the outbox records for federate h and
// returns the index of the first TimeAdvanceGrant and the index of the
// buffered TSO event (-1 when absent).
func indexOfGrantAndTSO(recs []recordedEvent, h core.FederateHandle, tso core.OutboundEvent) (grantIdx, tsoIdx int) {
	grantIdx, tsoIdx = -1, -1
	i := 0
	for _, rec := range recs {
		if rec.h != h {
			continue
		}
		if _, ok := rec.evt.(*hlatime.TimeAdvanceGrant); ok && grantIdx == -1 {
			grantIdx = i
		}
		if rec.evt == tso && tsoIdx == -1 {
			tsoIdx = i
		}
		i++
	}
	return grantIdx, tsoIdx
}

func TestSpec_M37_EB2_BufferedTSOReleasedBeforeGrant_TAR(t *testing.T) {
	ctx := context.Background()
	mgr, out := newTimeManager(t)
	fed := core.FederationName("f")
	h := core.FederateHandle(2)

	if err := mgr.EnableConstrained(ctx, fed, h); err != nil {
		t.Fatalf("enable constrained: %v", err)
	}

	// A TSO event at t=3 is buffered (async delivery off, currentTime 0).
	tso := &fakeTSOEvent{seq: 101}
	if !mgr.ShouldDeliverNow(fed, h, 3) {
		if err := mgr.BufferTSO(ctx, fed, h, 3, tso); err != nil {
			t.Fatalf("buffer tso: %v", err)
		}
	} else {
		t.Fatalf("precondition failed: TSO at t=3 should buffer for constrained federate at t=0")
	}

	// TAR(5): no regulators → grant fires immediately at t=5, which
	// advances past the buffered event's timestamp.
	if err := mgr.TimeAdvanceRequest(ctx, fed, h, 5); err != nil {
		t.Fatalf("TAR: %v", err)
	}

	grantIdx, tsoIdx := indexOfGrantAndTSO(out.snapshot(), h, tso)
	if grantIdx == -1 {
		t.Fatalf("no TimeAdvanceGrant delivered")
	}
	if tsoIdx == -1 {
		t.Fatalf("buffered TSO event never released")
	}
	if tsoIdx > grantIdx {
		t.Fatalf("§8.14 violation: buffered TSO (idx %d) delivered AFTER the grant (idx %d); federate must hold all TSO <= grant-time before the grant fires", tsoIdx, grantIdx)
	}
}

func TestSpec_M37_EB2_BufferedTSOReleasedBeforeGrant_NER(t *testing.T) {
	ctx := context.Background()
	mgr, out := newTimeManager(t)
	fed := core.FederationName("f")
	h := core.FederateHandle(2)

	if err := mgr.EnableConstrained(ctx, fed, h); err != nil {
		t.Fatalf("enable constrained: %v", err)
	}
	tso := &fakeTSOEvent{seq: 202}
	if err := mgr.BufferTSO(ctx, fed, h, 4, tso); err != nil {
		t.Fatalf("buffer tso: %v", err)
	}
	if err := mgr.NextMessageRequest(ctx, fed, h, 4); err != nil {
		t.Fatalf("NER: %v", err)
	}

	grantIdx, tsoIdx := indexOfGrantAndTSO(out.snapshot(), h, tso)
	if grantIdx == -1 || tsoIdx == -1 {
		t.Fatalf("grant idx %d / tso idx %d — both must be delivered", grantIdx, tsoIdx)
	}
	if tsoIdx > grantIdx {
		t.Fatalf("§8.14 violation: buffered TSO (idx %d) after grant (idx %d)", tsoIdx, grantIdx)
	}
}

// A buffered event BEYOND the grant time must stay buffered — the swap
// must not over-release.
func TestSpec_M37_EB2_TSOBeyondGrantTimeStaysBuffered(t *testing.T) {
	ctx := context.Background()
	mgr, out := newTimeManager(t)
	fed := core.FederationName("f")
	h := core.FederateHandle(2)

	if err := mgr.EnableConstrained(ctx, fed, h); err != nil {
		t.Fatalf("enable constrained: %v", err)
	}
	tso := &fakeTSOEvent{seq: 303}
	if err := mgr.BufferTSO(ctx, fed, h, 9, tso); err != nil {
		t.Fatalf("buffer tso: %v", err)
	}
	if err := mgr.TimeAdvanceRequest(ctx, fed, h, 5); err != nil {
		t.Fatalf("TAR: %v", err)
	}
	_, tsoIdx := indexOfGrantAndTSO(out.snapshot(), h, tso)
	if tsoIdx != -1 {
		t.Fatalf("TSO at t=9 released by a grant to t=5")
	}
}
