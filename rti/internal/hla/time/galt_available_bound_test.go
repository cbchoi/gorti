package time

import (
	"context"
	"errors"
	"math"
	"runtime"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func newGALTPendingPeer(t *testing.T, mode AdvanceMode, lookahead core.LogicalTime, queued ...core.LogicalTime) *Manager {
	t.Helper()
	m, _ := newIEEE2010DeliveryManager(t)
	t.Cleanup(func() { managerExt.Delete(m) })
	ctx := context.Background()
	// The querying regulator keeps the peer genuinely pending, but must not
	// contribute directly to its own GALT (1516.1-2010 8.1.5).
	for h, la := range map[core.FederateHandle]core.LogicalTime{1: 0, 2: lookahead} {
		if err := m.EnableRegulation(ctx, "fed", h, la); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	for _, ts := range queued {
		r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{{Recipient: 2, Timestamp: ts, Event: &scheduledOrderedEvent{}, Sender: 1}})
		r.Commit(ctx)
	}
	if err := m.dispatchAdvance(ctx, "fed", 2, 10, mode); err != nil {
		t.Fatal(err)
	}
	if peer := m.Metrics("fed", 2); !peer.Advancing || peer.CurrentTime != 0 {
		t.Fatalf("fixture must retain pending peer request: %+v", peer)
	}
	return m
}

func assertGALTBound(t *testing.T, m *Manager, h core.FederateHandle, want core.LogicalTime) {
	t.Helper()
	s := m.Metrics("fed", h)
	if s.GALT == nil {
		t.Fatalf("GALT is undefined, want independent bound %v", want)
	}
	if *s.GALT != want {
		t.Fatalf("GALT = %v, want independent bound %v", *s.GALT, want)
	}
	if got, defined := m.QueryGALT("fed", h); !defined || got != want {
		t.Fatalf("QueryGALT = (%v,%v), want (%v,true)", got, defined, want)
	}
}

func TestGALTAvailablePendingTARUsesRequestedHorizon(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeTARA} {
		t.Run(mode.String(), func(t *testing.T) {
			m := newGALTPendingPeer(t, mode, 2, 4)
			assertGALTBound(t, m, 1, 12)
			assertGALTBound(t, m, 2, 0)
			if s := m.Metrics("fed", 1); s.LITS == nil || *s.LITS != 12 {
				t.Fatalf("empty incoming queue must use corrected GALT: %+v", s)
			}
		})
	}
}

func TestGALTAvailableNMRAccountsForEarlyGrant(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA} {
		for _, tc := range []struct {
			name   string
			queued []core.LogicalTime
			want   core.LogicalTime
		}{
			{"no-message", nil, 12},
			{"early-message", []core.LogicalTime{8, 4, 6}, 6},
			{"after-target", []core.LogicalTime{15}, 12},
		} {
			t.Run(mode.String()+"/"+tc.name, func(t *testing.T) {
				m := newGALTPendingPeer(t, mode, 2, tc.queued...)
				assertGALTBound(t, m, 1, tc.want)
			})
		}
	}
}

func TestGALTAvailableUsesProjectedActualLookahead(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeNER} {
		t.Run(mode.String(), func(t *testing.T) {
			m, _ := newIEEE2010DeliveryManager(t)
			t.Cleanup(func() { managerExt.Delete(m) })
			ctx := context.Background()
			for h, la := range map[core.FederateHandle]core.LogicalTime{1: 0, 2: 8} {
				if err := m.EnableRegulation(ctx, "fed", h, la); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.EnableConstrained(ctx, "fed", 2); err != nil {
				t.Fatal(err)
			}
			if err := m.ModifyLookahead(ctx, "fed", 2, 1); err != nil {
				t.Fatal(err)
			}
			r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{{Recipient: 2, Timestamp: 4, Event: &scheduledOrderedEvent{}}})
			r.Commit(ctx)
			if err := m.dispatchAdvance(ctx, "fed", 2, 10, mode); err != nil {
				t.Fatal(err)
			}
			want := core.LogicalTime(11)
			if mode == ModeNER {
				want = 8 // Grant at 4 has actual lookahead 4, not requested 1.
			}
			assertGALTBound(t, m, 1, want)
		})
	}
}

func TestGALTAvailableZeroLookaheadMatchesFloat64SendBoundary(t *testing.T) {
	for _, mode := range []AdvanceMode{ModeTAR, ModeTARA, ModeNER, ModeNMRA} {
		t.Run(mode.String(), func(t *testing.T) {
			m := newGALTPendingPeer(t, mode, 0)
			want := core.LogicalTime(10)
			if strictAdvance(mode) {
				want = core.LogicalTime(math.Nextafter(10, math.Inf(1)))
				if err := m.ValidateOutgoingTSO("fed", 2, 10); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
					t.Fatalf("strict peer may not send at request boundary: %v", err)
				}
			}
			if err := m.ValidateOutgoingTSO("fed", 2, want); err != nil {
				t.Fatalf("lowest representable permitted timestamp rejected: %v", err)
			}
			assertGALTBound(t, m, 1, want)
		})
	}
}

func TestGALTAvailableQueuedMessageCapsLITSNotTARHorizon(t *testing.T) {
	m := newGALTPendingPeer(t, ModeTAR, 2)
	ctx := context.Background()
	if err := m.EnableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	r := m.ReserveTSO("fed", []core.TSOBufferedDelivery{{Recipient: 1, Timestamp: 4, Event: &scheduledOrderedEvent{}}})
	r.Commit(ctx)
	assertGALTBound(t, m, 1, 12)
	if lits, ok := m.QueryLITS("fed", 1); !ok || lits != 4 {
		t.Fatalf("LITS = (%v,%v), want (4,true)", lits, ok)
	}
}

func TestGALTAvailableResignRemovesOnlyOtherRegulator(t *testing.T) {
	m := newGALTPendingPeer(t, ModeTAR, 2)
	assertGALTBound(t, m, 1, 12)
	m.OnFederateResign(context.Background(), "fed", 2)
	if s := m.Metrics("fed", 1); s.GALT != nil || s.LITS != nil {
		t.Fatalf("sole regulator GALT/LITS must be undefined: %+v", s)
	}
	if s := m.Metrics("other", 1); s.GALT != nil || s.LITS != nil {
		t.Fatalf("unrelated federation GALT/LITS must be undefined: %+v", s)
	}
}

func TestGALTAvailableRetractionRaisesEarlyGrantBound(t *testing.T) {
	m := newGALTPendingPeer(t, ModeNER, 2)
	assertGALTBound(t, m, 1, 12)
	if err := m.BufferTSOWithRetraction(context.Background(), "fed", 2, 4, &fakeRetractEvent{}, 1, 42); err != nil {
		t.Fatal(err)
	}
	assertGALTBound(t, m, 1, 6)
	if got := m.RetractMessage("fed", 1, 42); got != 1 {
		t.Fatalf("retracted %d messages, want 1", got)
	}
	assertGALTBound(t, m, 1, 12)
}

func TestGALTAvailableFailedGrantPreservesEarlyGrantBound(t *testing.T) {
	m := newGALTPendingPeer(t, ModeNER, 2, 4)
	failed := &lookaheadFailOutbox{fail: true}
	m.opts.Outbox = lookaheadFailReservable{failed}
	assertGALTBound(t, m, 1, 6)
	if err := m.ModifyLookahead(context.Background(), "fed", 1, 20); err == nil {
		t.Fatal("expected callback admission failure")
	}
	if peer := m.Metrics("fed", 2); !peer.Advancing || peer.CurrentTime != 0 || peer.TSOQueued != 1 {
		t.Fatalf("failed grant must restore pending state and queue: %+v", peer)
	}
	assertGALTBound(t, m, 1, 6)
	failed.fail = false
	if err := m.tryGrantPending(context.Background(), "fed"); err != nil {
		t.Fatal(err)
	}
	if peer := m.Metrics("fed", 2); peer.Advancing || peer.CurrentTime != 4 || peer.TSOQueued != 0 {
		t.Fatalf("retry must grant to the early message: %+v", peer)
	}
	assertGALTBound(t, m, 1, 6)
}

func TestGALTAvailableFQRCannotAdvertiseOnlyRequestedHorizon(t *testing.T) {
	m, _ := newIEEE2010DeliveryManager(t)
	t.Cleanup(func() { managerExt.Delete(m) })
	ctx := context.Background()
	for h, la := range map[core.FederateHandle]core.LogicalTime{1: 5, 2: 1, 3: 3} {
		if err := m.EnableRegulation(ctx, "fed", h, la); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range []core.FederateHandle{2, 3} {
		if err := m.EnableConstrained(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	// Observe the legal interval between request recording and grant admission.
	for _, h := range []core.FederateHandle{2, 3} {
		if err := m.recordAdvance("fed", h, 10, ModeFQR); err != nil {
			t.Fatal(err)
		}
	}
	// Peer 2 can FQR-grant at 5 without receiving a message, then send at 6.
	// Peer 3's corresponding send bound is 8. Requested horizons 11/13 are unsafe.
	assertGALTBound(t, m, 1, 6)
}

func TestGALTAvailableQueryWaitsForOutgoingAdmission(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "release", true: "commit"}[commit], func(t *testing.T) {
			m := newGALTPendingPeer(t, ModeNER, 2)
			r, err := m.ReserveOutgoingTSO("fed", 1, 4, []core.TSOBufferedDelivery{{Recipient: 2, Timestamp: 4, Event: &fakeRetractEvent{}}})
			if err != nil {
				t.Fatal(err)
			}
			defer r.Release()
			m.states.mu.Lock()
			unlockState := sync.OnceFunc(m.states.mu.Unlock)
			defer unlockState()
			started := make(chan struct{})
			done := make(chan core.LogicalTime, 1)
			go func() {
				close(started)
				got, _ := m.QueryGALT("fed", 1)
				done <- got
			}()
			<-started
			completed := make(chan struct{})
			go func() {
				if commit {
					r.Commit(context.Background())
				} else {
					r.Release()
				}
				close(completed)
			}()
			lease := extOf(m).evaluatorLock("fed")
			deadline := stdtime.Now().Add(stdtime.Second)
			ownsLease := false
			for stdtime.Now().Before(deadline) {
				if !lease.TryLock() {
					ownsLease = true
					break
				}
				lease.Unlock()
				runtime.Gosched()
			}
			unlockState()
			select {
			case got := <-done:
				want := core.LogicalTime(12)
				if commit {
					want = 6
				}
				if got != want || !ownsLease {
					t.Fatalf("query = %v (owns evaluator=%v), want %v", got, ownsLease, want)
				}
			case <-stdtime.After(stdtime.Second):
				t.Fatal("query did not complete after admission")
			}
			select {
			case <-completed:
			case <-stdtime.After(stdtime.Second):
				t.Fatal("outgoing admission did not complete")
			}
		})
	}
}
