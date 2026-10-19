package time

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func newLookaheadManager(t *testing.T, out core.Outbox) *Manager {
	t.Helper()
	m, err := New(Options{Clock: core.NewManualClock(zeroTime()), Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func requireActualLookahead(t *testing.T, m *Manager, want core.LogicalTime) {
	t.Helper()
	got := m.Metrics("fed", 1).Lookahead
	if got == nil {
		t.Fatal("actual lookahead is unavailable")
	}
	if *got != want {
		t.Fatalf("actual lookahead = %v, want %v", *got, want)
	}
}

func TestIEEE2010LookaheadPendingUsesProjectedActual(t *testing.T) {
	ctx := context.Background()
	m := newLookaheadManager(t, nopOutbox{})
	for h, lookahead := range map[core.FederateHandle]core.LogicalTime{1: 5, 2: 1} {
		if err := m.EnableRegulation(ctx, "fed", h, lookahead); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.ModifyLookahead(ctx, "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	if !m.Metrics("fed", 1).Advancing {
		t.Fatal("expected pending TAR")
	}
	requireActualLookahead(t, m, 5)
	if err := m.ValidateOutgoingTSO("fed", 1, 5); err != nil {
		t.Fatalf("projected floor = 2 + max(2, 5-2) = 5: %v", err)
	}
	if err := m.ValidateOutgoingTSO("fed", 1, core.LogicalTime(math.Nextafter(5, 0))); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("below projected floor: %v", err)
	}
	for _, peer := range m.regulatingSnapshot("fed") {
		if peer.Handle == 1 && (peer.Time != 2 || peer.Lookahead != 3) {
			t.Fatalf("LBTS contribution is not projected: %+v", peer)
		}
	}
}

func TestIEEE2010ModifyLookaheadRejectsPendingAdvance(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA, ModeTAR, ModeTARA} {
		t.Run(mode.String(), func(t *testing.T) {
			m := newLookaheadManager(t, nopOutbox{})
			for h, lookahead := range map[core.FederateHandle]core.LogicalTime{1: 5, 2: 1} {
				if err := m.EnableRegulation(ctx, "fed", h, lookahead); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
				t.Fatal(err)
			}
			if err := m.dispatchAdvance(ctx, "fed", 1, 10, mode); err != nil {
				t.Fatal(err)
			}
			if !m.Metrics("fed", 1).Advancing {
				t.Fatal("test requires a genuinely pending request")
			}
			before, err := m.ExportState("fed", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.ModifyLookahead(ctx, "fed", 1, 2); !errors.Is(err, core.ErrTimeAdvancingState) {
				t.Fatalf("modify while advancing = %v, want TimeAdvancingState", err)
			}
			after, err := m.ExportState("fed", nil)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected modification mutated state: %v", err)
			}
		})
	}
}

func TestIEEE2010LookaheadDecreaseTracksGrantedTime(t *testing.T) {
	ctx := context.Background()
	m := newLookaheadManager(t, nopOutbox{})
	if err := m.EnableRegulation(ctx, "fed", 1, 5); err != nil {
		t.Fatal(err)
	}
	if err := m.ModifyLookahead(ctx, "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	requireActualLookahead(t, m, 5)
	if err := m.ValidateOutgoingTSO("fed", 1, 4); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("decrease prematurely relaxed outgoing bound: %v", err)
	}
	for _, step := range []struct{ grant, actual core.LogicalTime }{{0, 5}, {1, 4}, {2, 3}, {3, 2}, {8, 2}} {
		if err := m.TimeAdvanceRequest(ctx, "fed", 1, step.grant); err != nil {
			t.Fatal(err)
		}
		requireActualLookahead(t, m, step.actual)
	}
}

func TestIEEE2010LookaheadRetargetAndRestore(t *testing.T) {
	ctx := context.Background()
	m := newLookaheadManager(t, nopOutbox{})
	if err := m.EnableRegulation(ctx, "fed", 1, 5); err != nil {
		t.Fatal(err)
	}
	for _, target := range []core.LogicalTime{2, 4} {
		if err := m.ModifyLookahead(ctx, "fed", 1, target); err != nil {
			t.Fatal(err)
		}
		requireActualLookahead(t, m, 5)
	}
	saved, err := m.ExportState("fed", nil)
	if err != nil {
		t.Fatal(err)
	}
	restored := newLookaheadManager(t, nopOutbox{})
	if err := restored.ImportState("fed", saved, nil); err != nil {
		t.Fatal(err)
	}
	if err := restored.TimeAdvanceRequest(ctx, "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	requireActualLookahead(t, restored, 4)
	if err := restored.ModifyLookahead(ctx, "fed", 1, 7); err != nil {
		t.Fatal(err)
	}
	if err := restored.TimeAdvanceRequest(ctx, "fed", 1, 3); err != nil {
		t.Fatal(err)
	}
	requireActualLookahead(t, restored, 7)
}

func TestIEEE2010LookaheadDecreaseIndependentOfGrantPartition(t *testing.T) {
	ctx := context.Background()
	for _, restore := range []bool{false, true} {
		t.Run(map[bool]string{false: "continuous", true: "restored"}[restore], func(t *testing.T) {
			m := newLookaheadManager(t, nopOutbox{})
			if err := m.EnableRegulation(ctx, "fed", 1, 1); err != nil {
				t.Fatal(err)
			}
			if err := m.ModifyLookahead(ctx, "fed", 1, 0.5); err != nil {
				t.Fatal(err)
			}
			delta := core.LogicalTime(math.Ldexp(1, -54))
			for i := 1; i <= 4; i++ {
				if err := m.TimeAdvanceRequest(ctx, "fed", 1, core.LogicalTime(i)*delta); err != nil {
					t.Fatal(err)
				}
				if restore && i == 1 {
					state, err := m.ExportState("fed", nil)
					if err != nil {
						t.Fatal(err)
					}
					m = newLookaheadManager(t, nopOutbox{})
					if err := m.ImportState("fed", state, nil); err != nil {
						t.Fatal(err)
					}
				}
			}
			requireActualLookahead(t, m, 1-4*delta)
		})
	}
}

func TestIEEE2010OutgoingTSORejectsRepresentableBelowFloor(t *testing.T) {
	ctx := context.Background()
	for _, floor := range []float64{1e-6, 1, 100} {
		m := newLookaheadManager(t, nopOutbox{})
		if err := m.EnableRegulation(ctx, "fed", 1, core.LogicalTime(floor)); err != nil {
			t.Fatal(err)
		}
		if err := m.ValidateOutgoingTSO("fed", 1, core.LogicalTime(math.Nextafter(floor, 0))); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
			t.Errorf("timestamp below %g accepted: %v", floor, err)
		}
		if err := m.ValidateOutgoingTSO("fed", 1, core.LogicalTime(floor)); err != nil {
			t.Errorf("exact floor %g rejected: %v", floor, err)
		}
	}
}

type lookaheadFailOutbox struct{ fail bool }

func (o *lookaheadFailOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, e core.OutboundEvent) error {
	if isEnableCallback(e) {
		return nil
	}
	if o.fail {
		return errors.New("grant admission failed")
	}
	return nil
}

type lookaheadFailReservable struct{ *lookaheadFailOutbox }
type lookaheadGrantReservation struct{ out *lookaheadFailOutbox }

func (o lookaheadFailReservable) Reserve(_ context.Context, _ core.FederationName, ds []core.OutboxDelivery) (core.OutboxReservation, error) {
	if r, ok := enableFixtureAdmission(ds); ok {
		return r, nil
	}
	return &lookaheadGrantReservation{out: o.lookaheadFailOutbox}, nil
}

func (r *lookaheadGrantReservation) Commit() error {
	return r.out.Send(context.Background(), "fed", 1, &TimeAdvanceGrant{})
}
func (*lookaheadGrantReservation) Release() {}

func TestIEEE2010LookaheadDecreaseRollsBackFailedGrant(t *testing.T) {
	for _, reserved := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "reserved"}[reserved], func(t *testing.T) {
			ctx := context.Background()
			out := &lookaheadFailOutbox{}
			var port core.Outbox = out
			if reserved {
				port = lookaheadFailReservable{out}
			}
			m := newLookaheadManager(t, port)
			if err := m.EnableRegulation(ctx, "fed", 1, 5); err != nil {
				t.Fatal(err)
			}
			if err := m.ModifyLookahead(ctx, "fed", 1, 2); err != nil {
				t.Fatal(err)
			}
			out.fail = true
			if err := m.TimeAdvanceRequest(ctx, "fed", 1, 2); err == nil {
				t.Fatal("expected grant admission failure")
			}
			requireActualLookahead(t, m, 5)
			if got := m.Metrics("fed", 1); got.CurrentTime != 0 || !got.Advancing {
				t.Fatalf("failed grant changed time state: %+v", got)
			}
			out.fail = false
			if err := m.tryGrantPending(ctx, "fed"); err != nil {
				t.Fatal(err)
			}
			requireActualLookahead(t, m, 3)
		})
	}
}

func TestIEEE2010LookaheadSameInfiniteProductSentinel(t *testing.T) {
	ctx := context.Background()
	m := newLookaheadManager(t, nopOutbox{})
	if err := m.EnableRegulation(ctx, "fed", 1, 5); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequestAvailable(ctx, "fed", 1, core.PositiveInfinity); err != nil {
		t.Fatal(err)
	}
	if err := m.ModifyLookahead(ctx, "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequestAvailable(ctx, "fed", 1, core.PositiveInfinity); err != nil {
		t.Fatal(err)
	}
	requireActualLookahead(t, m, 5)
}
