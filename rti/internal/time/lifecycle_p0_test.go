package time

import (
	"context"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

func TestP0OnFederationDestroyedClearsAllTimeState(t *testing.T) {
	m, err := New(Options{
		Clock:  core.NewFakeClock(stdtime.Unix(0, 0)),
		Outbox: &recordingOutbox{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.EnableRegulation(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.TimeAdvanceRequest(ctx, "fed", 1, 2); err != nil {
		t.Fatal(err)
	}
	ext := extOf(m)
	ext.haltedMu.Lock()
	ext.halted["fed"] = struct{}{}
	ext.haltedMu.Unlock()
	_ = ext.evaluatorLock("fed")

	m.OnFederationDestroyed("fed")
	m.states.mu.Lock()
	stateCount := len(m.states.states)
	m.states.mu.Unlock()
	ext.mu.Lock()
	advanceCount := len(ext.states)
	ext.mu.Unlock()
	ext.haltedMu.Lock()
	_, halted := ext.halted["fed"]
	ext.haltedMu.Unlock()
	ext.evalMu.Lock()
	_, evalLock := ext.evalLocks["fed"]
	ext.evalMu.Unlock()
	if stateCount != 0 || advanceCount != 0 || halted || evalLock {
		t.Fatalf("time state remains: states=%d advance=%d halted=%v eval=%v", stateCount, advanceCount, halted, evalLock)
	}
}
