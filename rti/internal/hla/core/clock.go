package core

import (
	"sync"
	"time"
)

// Clock isolates wall-time access. The core code path MUST NOT call time.Now()
// directly — see CODING_CONVENTIONS.md D-1. Production wires RealClock; tests
// inject ManualClock for deterministic harnesses.
type Clock interface {
	Now() time.Time
}

// RealClock is the production Clock. Construct with NewRealClock.
type RealClock struct{}

// NewRealClock returns a Clock backed by time.Now.
func NewRealClock() Clock { return RealClock{} }

// Now returns the current wall time.
func (RealClock) Now() time.Time { return time.Now() }

// ManualClock is a manually-advanced Clock for tests. Safe for concurrent use.
type ManualClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewManualClock returns a ManualClock initialized to t.
func NewManualClock(t time.Time) *ManualClock { return &ManualClock{now: t} }

// Now returns the current fake time.
func (c *ManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the fake clock forward by d.
func (c *ManualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Set sets the fake clock to t.
func (c *ManualClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}
