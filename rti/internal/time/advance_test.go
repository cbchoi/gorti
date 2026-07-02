package time

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestAdvanceMode_String_Stable: the stringification is part of the
// log/replay surface (cut-3 may parse it). Pin it explicitly so a
// future enum reorder is caught.
func TestAdvanceMode_String_Stable(t *testing.T) {
	cases := []struct {
		mode AdvanceMode
		want string
	}{
		{ModeNone, "none"},
		{ModeNER, "NER"},
		{ModeNMRA, "NMRA"},
		{ModeTAR, "TAR"},
		{ModeTARA, "TARA"},
		{ModeFQR, "FQR"},
	}
	for _, c := range cases {
		if got := c.mode.String(); got != c.want {
			t.Errorf("mode %d: String() = %q, want %q", c.mode, got, c.want)
		}
	}
}

// TestDecideGrant_NER_FullGrant_StrictGT: the M3 NER predicate fires
// when LBTS > requested (strict). Equality does not fire (forced-grant
// path is solo-only and gated separately).
func TestDecideGrant_NER_FullGrant_StrictGT(t *testing.T) {
	d := decideGrant(ModeNER, 0, 5, 6, false)
	if !d.fire || d.time != core.LogicalTime(5) || !d.clearPending {
		t.Errorf("NER LBTS>req: %+v, want fire@5 clear", d)
	}
	d = decideGrant(ModeNER, 0, 5, 5, false)
	if d.fire {
		t.Errorf("NER LBTS==req (not solo): fired %+v, want hold (strict >)", d)
	}
}

// TestDecideGrant_NMRA_FullGrant_InclusiveGE: NMRA predicate fires at
// equality — the M7 distinguishing semantic.
func TestDecideGrant_NMRA_FullGrant_InclusiveGE(t *testing.T) {
	d := decideGrant(ModeNMRA, 0, 5, 5, false)
	if !d.fire || d.time != core.LogicalTime(5) || !d.clearPending {
		t.Errorf("NMRA LBTS==req: %+v, want fire@5 clear", d)
	}
	d = decideGrant(ModeNMRA, 0, 5, 4, false)
	if d.fire {
		t.Errorf("NMRA LBTS<req (not solo): fired %+v, want hold", d)
	}
}

// TestDecideGrant_TAR_HoldsBelowRequested_GrantsAtRequested: M37 EB-5
// — IEEE 1516.1-2010 §8.10: a timeAdvanceRequest(t) is granted at
// EXACTLY t, once LBTS covers it; the RTI HOLDS the request while
// LBTS < t (delivering intervening TSO meanwhile). The pre-M37
// "incremental grant at LBTS" fired a full grant at LBTS < t
// (clearPending=true), silently parking the federate below its
// requested time — early/partial grants are FQR's contract (§8.12),
// not TAR's.
func TestDecideGrant_TAR_HoldsBelowRequested_GrantsAtRequested(t *testing.T) {
	// LBTS < req — TAR holds (pre-M37: fired @2).
	d := decideGrant(ModeTAR, 0, 5, 2, false)
	if d.fire {
		t.Errorf("TAR LBTS<req: fired %+v, want hold (§8.10 grant only at requested time)", d)
	}
	// LBTS == req: inclusive full grant at the requested time. The
	// inclusive boundary preserves the zero-lookahead peer lockstep
	// (two la=0 federates both TAR(t) → LBTS == t → both grant); see
	// AdvanceMode.inclusiveLBTS.
	d = decideGrant(ModeTAR, 0, 5, 5, false)
	if !d.fire || d.time != core.LogicalTime(5) || !d.clearPending {
		t.Errorf("TAR LBTS==req: %+v, want fire@5 clear (inclusive full grant)", d)
	}
}

// TestDecideGrant_TARA_FullGrantAtEqualLBTS: TARA's full-grant predicate
// IS inclusive, so LBTS == req fires the full path (clearPending=true).
func TestDecideGrant_TARA_FullGrantAtEqualLBTS(t *testing.T) {
	d := decideGrant(ModeTARA, 0, 5, 5, false)
	if !d.fire || d.time != core.LogicalTime(5) || !d.clearPending {
		t.Errorf("TARA LBTS==req: %+v, want fire@5 clear", d)
	}
}

// TestDecideGrant_FQR_BehavesLikeTAR: cut-2 simplification. FQR uses
// the inclusive predicate (like TARA) and the incremental path (like
// TAR). Document this here so a cut-3 follow-up that diverges has an
// explicit baseline.
func TestDecideGrant_FQR_BehavesLikeTAR(t *testing.T) {
	d := decideGrant(ModeFQR, 0, 5, 5, false)
	if !d.fire || d.time != core.LogicalTime(5) || !d.clearPending {
		t.Errorf("FQR LBTS==req: %+v, want fire@5 clear (cut-2 inclusive)", d)
	}
	d = decideGrant(ModeFQR, 0, 5, 2, false)
	if !d.fire || d.time != core.LogicalTime(2) || !d.clearPending {
		t.Errorf("FQR LBTS<req: %+v, want fire@2 clear (cut-2 incremental)", d)
	}
}

// TestDecideGrant_NER_SolePending_ForcedGrant_KeepsPending: the M3 W2
// escape hatch — sole-pending NER with LBTS<req emits a forced grant
// at LBTS and KEEPS pending so the duplicate-NER check still fires.
func TestDecideGrant_NER_SolePending_ForcedGrant_KeepsPending(t *testing.T) {
	d := decideGrant(ModeNER, 0, 5, 2, true)
	if !d.fire || d.time != core.LogicalTime(2) || d.clearPending {
		t.Errorf("NER solo LBTS<req: %+v, want fire@2 keep-pending", d)
	}
}

// TestDecideGrant_NMRA_SolePending_ForcedGrant_KeepsPending: NMRA
// inherits the forced-grant escape hatch. Only NER and NMRA do.
func TestDecideGrant_NMRA_SolePending_ForcedGrant_KeepsPending(t *testing.T) {
	d := decideGrant(ModeNMRA, 0, 5, 2, true)
	if !d.fire || d.time != core.LogicalTime(2) || d.clearPending {
		t.Errorf("NMRA solo LBTS<req: %+v, want fire@2 keep-pending", d)
	}
}

// TestDecideGrant_TAR_NoForcedGrant_HoldsWhenSole: M37 EB-5 — TAR has
// neither the NER/NMRA forced-grant escape hatch nor the removed
// incremental path: sole-pending with LBTS < req simply HOLDS until
// LBTS reaches the requested time (§8.10).
func TestDecideGrant_TAR_NoForcedGrant_HoldsWhenSole(t *testing.T) {
	d := decideGrant(ModeTAR, 0, 5, 2, true)
	if d.fire {
		t.Errorf("TAR solo LBTS<req: fired %+v, want hold (§8.10; no escape hatch, no incremental)", d)
	}
}

// TestDecideGrant_NER_NoProgress_Holds: when LBTS == currentTime, any
// grant would produce zero progress. Hold (no emission).
func TestDecideGrant_NER_NoProgress_Holds(t *testing.T) {
	d := decideGrant(ModeNER, 5, 7, 5, true)
	if d.fire {
		t.Errorf("NER LBTS==current: fired %+v, want hold (no progress)", d)
	}
}

// TestDecideGrant_TAR_NoProgress_Holds: same no-progress invariant for
// the TAR family.
func TestDecideGrant_TAR_NoProgress_Holds(t *testing.T) {
	d := decideGrant(ModeTAR, 5, 7, 5, false)
	if d.fire {
		t.Errorf("TAR LBTS==current: fired %+v, want hold (no progress)", d)
	}
}

// TestDispatchAdvance_HaltedFederation_RejectsAllModes: every mode must
// reject on a halted federation, mirroring NER's behaviour.
func TestDispatchAdvance_HaltedFederation_RejectsAllModes(t *testing.T) {
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ext := extOf(mgr)
	ext.markHalted("fed")
	ctx := context.Background()
	cases := []struct {
		name string
		fn   func() error
	}{
		{"NMRA", func() error { return mgr.NextMessageRequestAvailable(ctx, "fed", 1, 1) }},
		{"TAR", func() error { return mgr.TimeAdvanceRequest(ctx, "fed", 1, 1) }},
		{"TARA", func() error { return mgr.TimeAdvanceRequestAvailable(ctx, "fed", 1, 1) }},
		{"FQR", func() error { return mgr.FlushQueueRequest(ctx, "fed", 1, 1) }},
	}
	for _, c := range cases {
		if e := c.fn(); !errors.Is(e, core.ErrFederationHalted) {
			t.Errorf("%s halted: err = %v, want ErrFederationHalted", c.name, e)
		}
	}
}

// TestDispatchAdvance_DuplicatePending_BlocksAcrossModes: a federate
// with an outstanding NER cannot start a TAR (or any sibling) without
// first being granted. The duplicate-pending check is mode-agnostic.
func TestDispatchAdvance_DuplicatePending_BlocksAcrossModes(t *testing.T) {
	out := &recordingOutbox{}
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: out})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1))
	_ = mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(1))
	// fed1 NER — pending; LBTS=min(5+1, 0+1)=1 < 5, sole NER not solo
	// (fed2 is regulating but not pending, so pending count=1 → forced
	// grant at LBTS=1, KEEP pending).
	if e := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(5)); e != nil {
		t.Fatalf("first NER: %v", e)
	}
	// Now fed1 is mid-NER; a TAR call must be rejected as duplicate.
	if e := mgr.TimeAdvanceRequest(ctx, "fed", 1, core.LogicalTime(7)); !errors.Is(e, ErrDuplicateNER) {
		t.Errorf("TAR while NER pending: err = %v, want ErrDuplicateNER", e)
	}
	if e := mgr.NextMessageRequestAvailable(ctx, "fed", 1, core.LogicalTime(7)); !errors.Is(e, ErrDuplicateNER) {
		t.Errorf("NMRA while NER pending: err = %v, want ErrDuplicateNER", e)
	}
}

// TestTAR_SoleRegulator_GrantsAtRequest: with only one regulating
// federate, LBTS = req+lookahead > req for any positive lookahead, so
// the full-grant path fires.
func TestTAR_SoleRegulator_GrantsAtRequest(t *testing.T) {
	out := &recordingOutbox{}
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: out})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1))
	if e := mgr.TimeAdvanceRequest(ctx, "fed", 1, core.LogicalTime(5)); e != nil {
		t.Fatalf("TAR: %v", e)
	}
	got := out.snapshot()
	if len(got) != 1 || got[0].t != core.LogicalTime(5) {
		t.Errorf("TAR sole reg: grants = %+v, want one @5", got)
	}
}

// TestTARA_GrantAtLBTSEqualsT_BothLookaheadZero: the canonical TARA
// scenario from the M7 spec. LBTS = 0 with two lookahead-0 regulators;
// TARA(0) grants at 0 (TAR(0) would not — strict).
func TestTARA_GrantAtLBTSEqualsT_BothLookaheadZero(t *testing.T) {
	out := &recordingOutbox{}
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: out})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(0))
	_ = mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(0))
	if e := mgr.TimeAdvanceRequestAvailable(ctx, "fed", 1, core.LogicalTime(0)); e != nil {
		t.Fatalf("TARA: %v", e)
	}
	got := out.snapshot()
	if len(got) != 1 || got[0].t != core.LogicalTime(0) {
		t.Errorf("TARA inclusive: grants = %+v, want one @0", got)
	}
}

// TestNMRA_GrantAtLBTSEqualsT_BothLookaheadZero: NMRA mirror of the
// TARA test — same shape, different primitive.
func TestNMRA_GrantAtLBTSEqualsT_BothLookaheadZero(t *testing.T) {
	out := &recordingOutbox{}
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: out})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(0))
	_ = mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(0))
	if e := mgr.NextMessageRequestAvailable(ctx, "fed", 1, core.LogicalTime(0)); e != nil {
		t.Fatalf("NMRA: %v", e)
	}
	got := out.snapshot()
	if len(got) != 1 || got[0].t != core.LogicalTime(0) {
		t.Errorf("NMRA inclusive: grants = %+v, want one @0", got)
	}
}

// TestTAR_TwoRegulators_HoldsUntilPeerAdvances: M37 EB-5 — with a peer
// regulator idle at 0+2=2, fed1's TAR(5) HOLDS (pre-M37: incremental
// grant at LBTS=2). §8.10: the grant fires at EXACTLY the requested
// time once the peer's LBTS contribution covers it — here when fed2
// issues its own TAR(5), promoting its floor to 5+2=7.
func TestTAR_TwoRegulators_HoldsUntilPeerAdvances(t *testing.T) {
	out := &recordingOutbox{}
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: out})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1))
	_ = mgr.EnableRegulation(ctx, "fed", 2, core.LogicalTime(2))
	if e := mgr.TimeAdvanceRequest(ctx, "fed", 1, core.LogicalTime(5)); e != nil {
		t.Fatalf("TAR: %v", e)
	}
	if got := out.snapshot(); len(got) != 0 {
		t.Errorf("TAR below LBTS: grants = %+v, want none (hold until peer advances)", got)
	}
	// While held, the request stays pending — a duplicate TAR is
	// rejected.
	if e := mgr.TimeAdvanceRequest(ctx, "fed", 1, core.LogicalTime(6)); !errors.Is(e, core.ErrTimeAdvancingState) {
		t.Errorf("TAR while pending: err = %v, want ErrTimeAdvancingState", e)
	}
	// Peer TARs to 5 → its floor rises to 5+2=7 → both grants fire at
	// exactly the requested time 5.
	if e := mgr.TimeAdvanceRequest(ctx, "fed", 2, core.LogicalTime(5)); e != nil {
		t.Fatalf("peer TAR: %v", e)
	}
	got := out.snapshot()
	if len(got) != 2 {
		t.Fatalf("after peer TAR: grants = %+v, want two (both federates @5)", got)
	}
	for _, g := range got {
		if g.t != core.LogicalTime(5) {
			t.Errorf("grant = %+v, want time 5 (§8.10 exact requested time)", g)
		}
	}
	// Pending cleared — a fresh TAR succeeds.
	if e := mgr.TimeAdvanceRequest(ctx, "fed", 1, core.LogicalTime(6)); e != nil {
		t.Errorf("TAR after grant: err = %v, want nil (TAR clears pending)", e)
	}
}

// TestNER_AfterTAR_NoStaleMode: a federate that completed a TAR (mode
// cleared to None) can issue a fresh NER without inheriting TAR
// semantics. Ensures emitGrant resets ns.mode when clearing pending.
func TestNER_AfterTAR_NoStaleMode(t *testing.T) {
	out := &recordingOutbox{}
	mgr, err := New(Options{Clock: core.NewFakeClock(zeroTime()), Outbox: out})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	_ = mgr.EnableRegulation(ctx, "fed", 1, core.LogicalTime(1))
	if e := mgr.TimeAdvanceRequest(ctx, "fed", 1, core.LogicalTime(2)); e != nil {
		t.Fatalf("TAR: %v", e)
	}
	// mode should be ModeNone now.
	ext := extOf(mgr)
	ext.mu.Lock()
	ns := ext.getOrCreateLocked("fed", 1)
	mode := ns.mode
	pending := ns.pendingNER
	ext.mu.Unlock()
	if pending {
		t.Errorf("after TAR full-grant: pending=true, want false")
	}
	if mode != ModeNone {
		t.Errorf("after TAR full-grant: mode=%s, want none", mode)
	}
	// Fresh NER must be accepted.
	if e := mgr.NextMessageRequest(ctx, "fed", 1, core.LogicalTime(5)); e != nil {
		t.Errorf("NER after TAR: err = %v, want nil", e)
	}
}
