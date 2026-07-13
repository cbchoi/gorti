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
	d := decideGrant(ModeNER, 0, 5, 6, false, 0, false)
	if !d.fire || d.time != core.LogicalTime(5) || !d.clearPending {
		t.Errorf("NER LBTS>req: %+v, want fire@5 clear", d)
	}
	d = decideGrant(ModeNER, 0, 5, 5, false, 0, false)
	if d.fire {
		t.Errorf("NER LBTS==req (not solo): fired %+v, want hold (strict >)", d)
	}
}

// TestDecideGrant_NMRA_FullGrant_InclusiveGE: NMRA predicate fires at
// equality — the M7 distinguishing semantic.
func TestDecideGrant_NMRA_FullGrant_InclusiveGE(t *testing.T) {
	d := decideGrant(ModeNMRA, 0, 5, 5, false, 0, false)
	if !d.fire || d.time != core.LogicalTime(5) || !d.clearPending {
		t.Errorf("NMRA LBTS==req: %+v, want fire@5 clear", d)
	}
	d = decideGrant(ModeNMRA, 0, 5, 4, false, 0, false)
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
	d := decideGrant(ModeTAR, 0, 5, 2, false, 0, false)
	if d.fire {
		t.Errorf("TAR LBTS<req: fired %+v, want hold (§8.10 grant only at requested time)", d)
	}
	// LBTS == req: TAR holds because a regulating peer at the boundary
	// may still send a timestamp-equal message. TARA is inclusive.
	d = decideGrant(ModeTAR, 0, 5, 5, false, 0, false)
	if d.fire {
		t.Errorf("TAR LBTS==req: %+v, want hold (strict boundary)", d)
	}
}

// TestDecideGrant_TARA_FullGrantAtEqualLBTS: TARA's full-grant predicate
// IS inclusive, so LBTS == req fires the full path (clearPending=true).
func TestDecideGrant_TARA_FullGrantAtEqualLBTS(t *testing.T) {
	d := decideGrant(ModeTARA, 0, 5, 5, false, 0, false)
	if !d.fire || d.time != core.LogicalTime(5) || !d.clearPending {
		t.Errorf("TARA LBTS==req: %+v, want fire@5 clear", d)
	}
}

// TestDecideGrant_FQR_BehavesLikeTAR: cut-2 simplification. FQR uses
// the inclusive predicate (like TARA) and the incremental path (like
// TAR). Document this here so a cut-3 follow-up that diverges has an
// explicit baseline.
func TestDecideGrant_FQR_BehavesLikeTAR(t *testing.T) {
	d := decideGrant(ModeFQR, 0, 5, 5, false, 0, false)
	if !d.fire || d.time != core.LogicalTime(5) || !d.clearPending {
		t.Errorf("FQR LBTS==req: %+v, want fire@5 clear (cut-2 inclusive)", d)
	}
	d = decideGrant(ModeFQR, 0, 5, 2, false, 0, false)
	if !d.fire || d.time != core.LogicalTime(2) || !d.clearPending {
		t.Errorf("FQR LBTS<req: %+v, want fire@2 clear (cut-2 incremental)", d)
	}
}

// TestDecideGrant_NER_SolePending_NoTSO_Holds: M38 GA — IEEE
// 1516.1-2010 §8.8 defines no interim grant: a sole-pending NER with
// LBTS < requested and NO queued TSO message simply HOLDS. (The M3 W2
// "forced grant at LBTS keeps pending" escape hatch this replaced was
// spec-invisible extra chatter — the tm_tso_ordering extra GRANT and
// the IVCT tc010 NER xfail.)
func TestDecideGrant_NER_SolePending_NoTSO_Holds(t *testing.T) {
	d := decideGrant(ModeNER, 0, 5, 2, true, 0, false)
	if d.fire {
		t.Errorf("NER solo LBTS<req no TSO: fired %+v, want hold (§8.8 — no interim grant)", d)
	}
}

// TestDecideGrant_NMRA_SolePending_NoTSO_Holds: NMRA (§8.9) holds the
// same way. Only the queued-message path (below) or LBTS >= requested
// completes the request.
func TestDecideGrant_NMRA_SolePending_NoTSO_Holds(t *testing.T) {
	d := decideGrant(ModeNMRA, 0, 5, 2, true, 0, false)
	if d.fire {
		t.Errorf("NMRA solo LBTS<req no TSO: fired %+v, want hold (§8.9 — no interim grant)", d)
	}
}

// TestDecideGrant_NER_NextTSOTarget: M38 GA — §8.8: with a TSO message
// queued below the requested time, the grant target is the message
// time. NER needs LBTS strictly above the target (a same-time message
// could still arrive at the boundary) and the grant COMPLETES the
// request (clearPending).
func TestDecideGrant_NER_NextTSOTarget(t *testing.T) {
	// TSO at 1, LBTS 2 > 1 → grant at the message time, clear.
	d := decideGrant(ModeNER, 0, 5, 2, false, 1, true)
	if !d.fire || d.time != core.LogicalTime(1) || !d.clearPending {
		t.Errorf("NER TSO@1 LBTS=2: %+v, want fire@1 clear (§8.8 min(requested, next TSO))", d)
	}
	// TSO at 2 == LBTS → hold (strict boundary).
	d = decideGrant(ModeNER, 0, 5, 2, false, 2, true)
	if d.fire {
		t.Errorf("NER TSO@2 LBTS==2: fired %+v, want hold (strict >)", d)
	}
	// TSO above requested → target stays requested; LBTS 6 > 5 → @5.
	d = decideGrant(ModeNER, 0, 5, 6, false, 9, true)
	if !d.fire || d.time != core.LogicalTime(5) || !d.clearPending {
		t.Errorf("NER TSO@9 req=5 LBTS=6: %+v, want fire@5 clear", d)
	}
	// TSO at-or-before currentTime is already releasable and must not
	// drag the target into the past.
	d = decideGrant(ModeNER, 3, 5, 2, false, 3, true)
	if d.fire {
		t.Errorf("NER TSO@currentTime LBTS<req: fired %+v, want hold", d)
	}
}

// TestDecideGrant_NMRA_NextTSOTarget_Inclusive: §8.9 — the Available
// variant grants at the message time when LBTS EQUALS it.
func TestDecideGrant_NMRA_NextTSOTarget_Inclusive(t *testing.T) {
	d := decideGrant(ModeNMRA, 0, 5, 2, false, 2, true)
	if !d.fire || d.time != core.LogicalTime(2) || !d.clearPending {
		t.Errorf("NMRA TSO@2 LBTS==2: %+v, want fire@2 clear (inclusive)", d)
	}
}

// TestDecideGrant_TAR_IgnoresQueuedTSO: §8.10 — TAR grants at EXACTLY
// the requested time; a queued TSO below it drains on emission
// (§8.14) but never becomes the grant target.
func TestDecideGrant_TAR_IgnoresQueuedTSO(t *testing.T) {
	d := decideGrant(ModeTAR, 0, 5, 6, false, 1, true)
	if !d.fire || d.time != core.LogicalTime(5) || !d.clearPending {
		t.Errorf("TAR TSO@1 LBTS=6: %+v, want fire@5 clear (TSO does not retarget TAR)", d)
	}
	d = decideGrant(ModeTAR, 0, 5, 2, false, 1, true)
	if d.fire {
		t.Errorf("TAR TSO@1 LBTS=2<req: fired %+v, want hold (§8.10)", d)
	}
}

// TestDecideGrant_TAR_NoForcedGrant_HoldsWhenSole: M37 EB-5 — TAR has
// neither the NER/NMRA forced-grant escape hatch nor the removed
// incremental path: sole-pending with LBTS < req simply HOLDS until
// LBTS reaches the requested time (§8.10).
func TestDecideGrant_TAR_NoForcedGrant_HoldsWhenSole(t *testing.T) {
	d := decideGrant(ModeTAR, 0, 5, 2, true, 0, false)
	if d.fire {
		t.Errorf("TAR solo LBTS<req: fired %+v, want hold (§8.10; no escape hatch, no incremental)", d)
	}
}

// TestDecideGrant_NER_NoProgress_Holds: when LBTS == currentTime, any
// grant would produce zero progress. Hold (no emission).
func TestDecideGrant_NER_NoProgress_Holds(t *testing.T) {
	d := decideGrant(ModeNER, 5, 7, 5, true, 0, false)
	if d.fire {
		t.Errorf("NER LBTS==current: fired %+v, want hold (no progress)", d)
	}
}

// TestDecideGrant_TAR_NoProgress_Holds: same no-progress invariant for
// the TAR family.
func TestDecideGrant_TAR_NoProgress_Holds(t *testing.T) {
	d := decideGrant(ModeTAR, 5, 7, 5, false, 0, false)
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
	// fed1 NER — pending; LBTS=min(5+1, 0+1)=1 < 5 and no queued TSO →
	// the request HOLDS (M38 GA — §8.8 no interim grant) and stays
	// outstanding.
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
