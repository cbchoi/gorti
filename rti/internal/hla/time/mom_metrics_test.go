package time

import (
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"testing"
)

func TestMOMMetricsObserveActualStoresWithoutCreatingState(t *testing.T) {
	m, _ := New(Options{Clock: core.NewManualClock(zeroTime()), Outbox: nopOutbox{}})
	ext := extOf(m)
	m.states.mu.Lock()
	m.states.states[federateKey{"f", 1}] = &federateState{regulating: true, constrained: true, lookahead: 3}
	m.states.states[federateKey{"f", 2}] = &federateState{regulating: true, lookahead: 4}
	m.states.mu.Unlock()
	ext.mu.Lock()
	ns := ext.getOrCreateLocked("f", 1)
	ns.currentTime = 7
	ns.pendingNER = true
	ns.asyncDelivery = true
	other := ext.getOrCreateLocked("f", 2)
	other.currentTime = 12
	ext.mu.Unlock()
	s := m.Metrics("f", 1)
	if !s.Regulating || !s.Constrained || !s.AsynchronousDelivery || !s.Advancing || s.CurrentTime != 7 || s.Lookahead == nil || *s.Lookahead != 3 || s.GALT == nil || *s.GALT != 16 || s.LITS == nil || *s.LITS != 16 {
		t.Fatalf("metrics=%+v", s)
	}
	otherSnapshot := m.Metrics("other", 1)
	if otherSnapshot.Regulating || otherSnapshot.GALT != nil {
		t.Fatal("federations mixed")
	}
	m.states.mu.Lock()
	_, exists := m.states.states[federateKey{"other", 1}]
	m.states.mu.Unlock()
	if exists {
		t.Fatal("read created state")
	}
}
