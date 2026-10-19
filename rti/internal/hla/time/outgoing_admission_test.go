package time

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type outgoingAdmissionMutation struct {
	name        string
	regulating  bool
	constrained bool
	apply       func(*Manager) error
	check       func(*testing.T, *Manager)
}

func outgoingAdmissionMutations() []outgoingAdmissionMutation {
	ctx := context.Background()
	mutations := []outgoingAdmissionMutation{
		{
			name: "modify-lookahead", regulating: true,
			apply: func(m *Manager) error { return m.ModifyLookahead(ctx, "fed", 1, 6) },
			check: func(t *testing.T, m *Manager) {
				t.Helper()
				if got := m.Metrics("fed", 1); got.Lookahead == nil || *got.Lookahead != 6 {
					t.Errorf("lookahead mutation missing: %+v", got)
				}
			},
		},
		{
			name:  "enable-regulation",
			apply: func(m *Manager) error { return m.EnableRegulation(ctx, "fed", 1, 6) },
			check: func(t *testing.T, m *Manager) {
				t.Helper()
				if got := m.Metrics("fed", 1); !got.Regulating || got.Lookahead == nil || *got.Lookahead != 6 {
					t.Errorf("regulation enable missing: %+v", got)
				}
			},
		},
		{
			name: "disable-regulation", regulating: true,
			apply: func(m *Manager) error { return m.DisableRegulation(ctx, "fed", 1) },
			check: func(t *testing.T, m *Manager) {
				t.Helper()
				if got := m.Metrics("fed", 1); got.Regulating || got.Lookahead != nil {
					t.Errorf("regulation disable missing: %+v", got)
				}
			},
		},
		{
			name: "resign", regulating: true,
			apply: func(m *Manager) error { m.OnFederateResign(ctx, "fed", 1); return nil },
			check: func(t *testing.T, m *Manager) {
				t.Helper()
				m.states.mu.Lock()
				exists := m.states.getLocked("fed", 1) != nil
				m.states.mu.Unlock()
				ext := extOf(m)
				ext.mu.Lock()
				exists = exists || ext.getLocked("fed", 1) != nil
				ext.mu.Unlock()
				if exists {
					t.Error("resigned sender still has time state")
				}
			},
		},
	}
	for _, mode := range []AdvanceMode{ModeNER, ModeNMRA, ModeTAR, ModeTARA, ModeFQR} {
		mode := mode
		mutations = append(mutations, outgoingAdmissionMutation{
			name: "record-" + mode.String(), regulating: true, constrained: true,
			apply: func(m *Manager) error { return m.recordAdvance("fed", 1, 10, mode) },
			check: func(t *testing.T, m *Manager) {
				t.Helper()
				ext := extOf(m)
				ext.mu.Lock()
				defer ext.mu.Unlock()
				ns := ext.getLocked("fed", 1)
				if ns == nil || !ns.pendingNER || ns.requestedTime != 10 || ns.mode != mode || ns.currentTime != 0 {
					t.Errorf("advance recording missing or unexpectedly granted: %+v", ns)
				}
			},
		})
	}
	return mutations
}

func newOutgoingAdmissionManager(t *testing.T, regulating bool) *Manager {
	t.Helper()
	m, err := New(Options{Clock: core.NewManualClock(zeroTime()), Outbox: nopOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { managerExt.Delete(m) })
	ctx := context.Background()
	if regulating {
		if err := m.EnableRegulation(ctx, "fed", 1, 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	// A zero-lookahead peer exercises constrained sender admission.
	if err := m.EnableRegulation(ctx, "fed", 3, 0); err != nil {
		t.Fatal(err)
	}
	return m
}

func outgoingAdmissionQueueLength(m *Manager) int {
	ext := extOf(m)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	if ns := ext.getLocked("fed", 2); ns != nil {
		return len(ns.tsoBuffer)
	}
	return 0
}

func assertOutgoingAdmissionUnlocked(t *testing.T, m *Manager) {
	t.Helper()
	ext := extOf(m)
	for _, item := range []struct {
		name string
		lock *sync.Mutex
	}{{"evaluator", ext.evaluatorLock("fed")}, {"regulation", &m.states.mu}, {"advance", &ext.mu}} {
		if !item.lock.TryLock() {
			t.Fatalf("%s lock leaked", item.name)
		}
		item.lock.Unlock()
	}
}

func TestOutgoingAdmissionSerializesBoundaryMutations(t *testing.T) {
	for _, mutation := range outgoingAdmissionMutations() {
		for _, commit := range []bool{false, true} {
			for _, buffered := range []bool{false, true} {
				name := mutation.name + "/" + map[bool]string{false: "release", true: "commit"}[commit] + "/" + map[bool]string{false: "empty", true: "buffered"}[buffered]
				t.Run(name, func(t *testing.T) {
					m := newOutgoingAdmissionManager(t, mutation.regulating)
					if mutation.constrained {
						if err := m.EnableConstrained(context.Background(), "fed", 1); err != nil {
							t.Fatal(err)
						}
					}
					var deliveries []core.TSOBufferedDelivery
					if buffered {
						deliveries = []core.TSOBufferedDelivery{{Recipient: 2, Timestamp: 5, Event: &fakeRetractEvent{}, Sender: 1}}
					}
					reservation, err := m.ReserveOutgoingTSO("fed", 1, 5, deliveries)
					if err != nil || reservation == nil {
						t.Fatalf("reserve: reservation=%v err=%v", reservation, err)
					}
					defer reservation.Release()
					lease := extOf(m).evaluatorLock("fed")
					if lease.TryLock() {
						lease.Unlock()
						t.Fatal("outgoing reservation does not retain evaluator")
					}
					if got := outgoingAdmissionQueueLength(m); got != 0 {
						t.Fatalf("reservation published %d buffered callbacks before commit", got)
					}

					// Pin the next lock in each mutator. After reservation completion,
					// TryLock can prove the mutator owns evaluator before touching state.
					m.states.mu.Lock()
					unlockState := sync.OnceFunc(m.states.mu.Unlock)
					defer unlockState()
					started := make(chan struct{})
					done := make(chan error, 1)
					go func() {
						close(started)
						done <- mutation.apply(m)
					}()
					<-started
					if commit {
						reservation.Commit(context.Background())
					} else {
						reservation.Release()
					}
					// Completion is idempotent even after the other operation takes over.
					reservation.Commit(context.Background())
					reservation.Release()
					deadline := stdtime.Now().Add(stdtime.Second)
					ownsEvaluator := false
					for stdtime.Now().Before(deadline) {
						if !lease.TryLock() {
							ownsEvaluator = true
							break
						}
						lease.Unlock()
						runtime.Gosched()
					}
					unlockState()
					select {
					case err := <-done:
						if err != nil {
							t.Fatalf("mutation after reservation completion: %v", err)
						}
					case <-stdtime.After(stdtime.Second):
						t.Fatal("mutation did not unblock after reservation completion")
					}
					if !ownsEvaluator {
						t.Error("boundary mutator reaches state without owning evaluator")
					}
					assertOutgoingAdmissionUnlocked(t, m)
					mutation.check(t, m)
					wantQueued := 0
					if commit && buffered {
						wantQueued = 1
					}
					if got := outgoingAdmissionQueueLength(m); got != wantQueued {
						t.Errorf("buffered callbacks = %d, want %d", got, wantQueued)
					}
				})
			}
		}
	}
}

func TestOutgoingAdmissionFailedValidationReleasesLease(t *testing.T) {
	for _, buffered := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "buffered"}[buffered], func(t *testing.T) {
			m := newOutgoingAdmissionManager(t, true)
			if err := m.ValidateOutgoingTSO("fed", 1, 5); err != nil {
				t.Fatalf("optimistic validation: %v", err)
			}
			if err := m.ModifyLookahead(context.Background(), "fed", 1, 6); err != nil {
				t.Fatal(err)
			}
			var deliveries []core.TSOBufferedDelivery
			if buffered {
				deliveries = []core.TSOBufferedDelivery{{Recipient: 2, Timestamp: 5, Event: &fakeRetractEvent{}, Sender: 1}}
			}
			reservation, err := m.ReserveOutgoingTSO("fed", 1, 5, deliveries)
			if reservation != nil {
				reservation.Release()
				t.Fatal("failed validation returned a reservation")
			}
			if !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
				t.Fatalf("stale timestamp error = %v, want ErrTimeInvalidLogicalTime", err)
			}
			assertOutgoingAdmissionUnlocked(t, m)
			if got := outgoingAdmissionQueueLength(m); got != 0 {
				t.Fatalf("rejected reservation published %d callbacks", got)
			}
			valid, err := m.ReserveOutgoingTSO("fed", 1, 6, nil)
			if err != nil || valid == nil {
				t.Fatalf("valid retry: reservation=%v err=%v", valid, err)
			}
			valid.Release()
			assertOutgoingAdmissionUnlocked(t, m)
		})
	}
}
