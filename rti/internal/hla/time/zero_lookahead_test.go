package time

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func zeroBoundManager(t *testing.T, lookahead core.LogicalTime) *Manager {
	t.Helper()
	m := newLookaheadManager(t, nopOutbox{})
	for _, h := range []core.FederateHandle{1, 2} {
		la := core.LogicalTime(0)
		if h == 1 {
			la = lookahead
		}
		if err := m.EnableRegulation(context.Background(), "fed", h, la); err != nil {
			t.Fatal(err)
		}
		if err := m.EnableConstrained(context.Background(), "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func requireZeroBound(t *testing.T, m *Manager, base core.LogicalTime, strict bool) {
	t.Helper()
	for _, check := range []struct {
		ts    core.LogicalTime
		valid bool
	}{
		{core.LogicalTime(math.Nextafter(float64(base), math.Inf(-1))), false},
		{base, !strict},
		{core.LogicalTime(math.Nextafter(float64(base), math.Inf(1))), true},
	} {
		err := m.ValidateOutgoingTSO("fed", 1, check.ts)
		if check.valid && err != nil || !check.valid && !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
			t.Errorf("timestamp %.17g, bound %.17g strict=%v: error=%v, want valid=%v", check.ts, base, strict, err, check.valid)
		}
	}
}

// IEEE 1516.1-2010 8.1.4 and Table 3 (PDF pp.184-188).
func TestIEEE2010ZeroLookaheadPendingAndGranted(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeNER, ModeTARA, ModeNMRA, ModeFQR} {
		t.Run(mode.String(), func(t *testing.T) {
			m := zeroBoundManager(t, 0)
			requireZeroBound(t, m, 0, false)
			// Observe the admitted state before the immediate FQR evaluator.
			if err := m.recordAdvance("fed", 1, 10, mode); err != nil {
				t.Fatal(err)
			}
			if got := m.Metrics("fed", 1); !got.Advancing || got.CurrentTime != 0 {
				t.Fatalf("peer must hold request pending: %+v", got)
			}
			strict := mode == ModeTAR || mode == ModeNER
			requireZeroBound(t, m, 10, strict)
			if err := m.ModifyLookahead(context.Background(), "fed", 2, 20); err != nil {
				t.Fatal(err)
			}
			if got := m.Metrics("fed", 1); got.Advancing || got.CurrentTime != 10 {
				t.Fatalf("expected full grant to 10: %+v", got)
			}
			requireZeroBound(t, m, 10, strict)
		})
	}
}

func TestIEEE2010ZeroLookaheadAllZeroStrictProgress(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeNER} {
		t.Run(mode.String(), func(t *testing.T) {
			m := zeroBoundManager(t, 0)
			for _, h := range []core.FederateHandle{1, 2} {
				if err := m.dispatchAdvance(context.Background(), "fed", h, 10, mode); err != nil {
					t.Fatal(err)
				}
			}
			for _, h := range []core.FederateHandle{1, 2} {
				if got := m.Metrics("fed", h); got.Advancing || got.CurrentTime != 10 {
					t.Errorf("all-zero %v must progress, federate %d: %+v", mode, h, got)
				}
			}
			if got := LBTS(m.regulatingSnapshot("fed")); got != core.LogicalTime(math.Nextafter(10, math.Inf(1))) {
				t.Errorf("exclusive numeric adapter: LBTS=%v", got)
			}
		})
	}
}

func TestIEEE2010ZeroLookaheadSameTimeHistoryAndRestore(t *testing.T) {
	for _, strictMode := range []AdvanceMode{ModeTAR, ModeNER} {
		for _, available := range []AdvanceMode{ModeTARA, ModeNMRA, ModeFQR} {
			t.Run(strictMode.String()+"/"+available.String(), func(t *testing.T) {
				ctx := context.Background()
				m := zeroBoundManager(t, 0)
				if err := m.ModifyLookahead(ctx, "fed", 2, 20); err != nil {
					t.Fatal(err)
				}
				if err := m.dispatchAdvance(ctx, "fed", 1, 10, strictMode); err != nil {
					t.Fatal(err)
				}
				if got := m.Metrics("fed", 1); got.Advancing || got.CurrentTime != 10 {
					t.Fatalf("strict grant missing: %+v", got)
				}
				for i := 0; i < 2; i++ {
					if err := m.dispatchAdvance(ctx, "fed", 1, 10, available); err != nil {
						t.Fatal(err)
					}
					requireZeroBound(t, m, 10, true)
					state, err := m.ExportState("fed", nil)
					if err != nil {
						t.Fatal(err)
					}
					m = newLookaheadManager(t, nopOutbox{})
					if err := m.ImportState("fed", state, nil); err != nil {
						t.Fatal(err)
					}
					requireZeroBound(t, m, 10, true)
				}
				if err := m.dispatchAdvance(ctx, "fed", 1, 11, available); err != nil {
					t.Fatal(err)
				}
				requireZeroBound(t, m, 11, false)
			})
		}
	}
}

func TestIEEE2010ZeroLookaheadProjectedActual(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeNER, ModeTARA, ModeNMRA} {
		for _, target := range []core.LogicalTime{2, 5} {
			t.Run(mode.String()+"/"+map[core.LogicalTime]string{2: "still-positive", 5: "reaches-zero"}[target], func(t *testing.T) {
				m := zeroBoundManager(t, 5)
				if err := m.ModifyLookahead(context.Background(), "fed", 1, 0); err != nil {
					t.Fatal(err)
				}
				if err := m.dispatchAdvance(context.Background(), "fed", 1, target, mode); err != nil {
					t.Fatal(err)
				}
				requireActualLookahead(t, m, 5)
				requireZeroBound(t, m, 5, target == 5 && (mode == ModeTAR || mode == ModeNER))
			})
		}
	}
}

func TestIEEE2010ZeroLookaheadEarlyNextMessageGrant(t *testing.T) {
	m := zeroBoundManager(t, 0)
	ctx := context.Background()
	if err := m.BufferTSO(ctx, "fed", 1, 3, &fakeRetractEvent{}); err != nil {
		t.Fatal(err)
	}
	if err := m.NextMessageRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	requireZeroBound(t, m, 10, true)
	if err := m.ModifyLookahead(ctx, "fed", 2, 20); err != nil {
		t.Fatal(err)
	}
	if got := m.Metrics("fed", 1); got.Advancing || got.CurrentTime != 3 {
		t.Fatalf("early grant: %+v", got)
	}
	requireZeroBound(t, m, 3, true)
}

func TestIEEE2010ZeroLookaheadEnableResetsHistory(t *testing.T) {
	m := zeroBoundManager(t, 0)
	ctx := context.Background()
	if err := m.ModifyLookahead(ctx, "fed", 2, 20); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if got := m.Metrics("fed", 1); got.Advancing {
		t.Fatal("strict grant missing")
	}
	if err := m.EnableRegulation(ctx, "fed", 1, 0); !errors.Is(err, core.ErrTimeAlreadyRegulating) {
		t.Fatalf("duplicate enable: %v", err)
	}
	requireZeroBound(t, m, 10, true)
	if err := m.DisableRegulation(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableRegulation(ctx, "fed", 1, 0); err != nil {
		t.Fatal(err)
	}
	requireZeroBound(t, m, 10, false)
}

func TestIEEE2010ZeroLookaheadValidationUnderEvaluatorLease(t *testing.T) {
	m := zeroBoundManager(t, 0)
	lease := extOf(m).evaluatorLock("fed")
	lease.Lock()
	defer lease.Unlock()
	requireZeroBound(t, m, 0, false)
}

func TestIEEE2010ZeroLookaheadPendingSameTimeHistory(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeNER, ModeTARA, ModeNMRA, ModeFQR} {
		for _, requested := range []core.LogicalTime{10, 11} {
			fs := &federateState{regulating: true}
			ns := &nerState{currentTime: 10, strictZeroLookahead: true, pendingNER: true, requestedTime: requested, mode: mode}
			base, la, exclusive := outgoingTSOBound(fs, ns)
			wantExclusive := requested == 10 || mode == ModeTAR || mode == ModeNER
			if base != requested || la != 0 || exclusive != wantExclusive {
				t.Errorf("%v request %v: base=%v lookahead=%v exclusive=%v", mode, requested, base, la, exclusive)
			}
		}
	}
}

func TestIEEE2010ZeroLookaheadSnapshotUsesSameBound(t *testing.T) {
	m := zeroBoundManager(t, 0)
	ctx := context.Background()
	for _, h := range []core.FederateHandle{1, 2} {
		if err := m.TimeAdvanceRequest(ctx, "fed", h, 10); err != nil {
			t.Fatal(err)
		}
	}
	if snapshot := m.Snapshot("fed"); snapshot.LBTS != LBTS(m.regulatingSnapshot("fed")) {
		t.Fatalf("public snapshot and evaluator disagree: %+v", snapshot)
	}
}

func TestIEEE2010ZeroLookaheadFailedGrantRollsBackHistory(t *testing.T) {
	for _, reserved := range []bool{false, true} {
		for _, priorStrict := range []bool{false, true} {
			t.Run(map[bool]string{false: "send", true: "reserved"}[reserved]+"/"+map[bool]string{false: "set", true: "clear"}[priorStrict], func(t *testing.T) {
				ctx := context.Background()
				out := &lookaheadFailOutbox{}
				var port core.Outbox = out
				if reserved {
					port = lookaheadFailReservable{out}
				}
				m := newLookaheadManager(t, port)
				if err := m.EnableRegulation(ctx, "fed", 1, 0); err != nil {
					t.Fatal(err)
				}
				mode := ModeTAR
				current := core.LogicalTime(0)
				if priorStrict {
					if err := m.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
						t.Fatal(err)
					}
					mode, current = ModeTARA, 10
				}
				out.fail = true
				if err := m.dispatchAdvance(ctx, "fed", 1, 11, mode); err == nil {
					t.Fatal("expected grant failure")
				}
				state, err := m.ExportState("fed", nil)
				if err != nil {
					t.Fatal(err)
				}
				entry := state.Federates[0]
				if entry.StrictZeroLookahead != priorStrict || entry.CurrentTime != current || !entry.PendingRequest {
					t.Fatalf("failed grant changed history: %+v", entry)
				}
				out.fail = false
				if err := m.tryGrantPending(ctx, "fed"); err != nil {
					t.Fatal(err)
				}
				requireZeroBound(t, m, 11, !priorStrict)
			})
		}
	}
}

func TestIEEE2010ZeroLookaheadPreservesProductInfinity(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeNER, ModeTARA, ModeNMRA} {
		for _, la := range []core.LogicalTime{0, 2} {
			m := zeroBoundManager(t, la)
			if err := m.dispatchAdvance(context.Background(), "fed", 1, core.PositiveInfinity, mode); err != nil {
				t.Fatal(err)
			}
			if !m.Metrics("fed", 1).Advancing {
				t.Fatal("peer must keep final-time request pending")
			}
			if err := m.ValidateOutgoingTSO("fed", 1, core.PositiveInfinity); err != nil {
				t.Errorf("%v lookahead=%v: product final-time sentinel rejected: %v", mode, la, err)
			}
			if err := m.ValidateOutgoingTSO("fed", 1, core.LogicalTime(math.MaxFloat64)); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
				t.Errorf("%v lookahead=%v: accepted finite timestamp below final-time request: %v", mode, la, err)
			}
		}
	}
}
