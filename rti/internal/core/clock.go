package core

import (
	"sync"
	"time"
)

// Clock isolates wall-time access. The core code path MUST NOT call time.Now()
// directly — see CODING_CONVENTIONS.md D-1. Production wires RealClock; tests
// inject FakeClock for deterministic harnesses.
type Clock interface {
	Now() time.Time
}

// RealClock is the production Clock. Construct with NewRealClock.
type RealClock struct{}

// NewRealClock returns a Clock backed by time.Now.
func NewRealClock() Clock { return RealClock{} }

// Now returns the current wall time.
func (RealClock) Now() time.Time { return time.Now() }

// FakeClock is a manually-advanced Clock for tests. Safe for concurrent use.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock returns a FakeClock initialized to t.
func NewFakeClock(t time.Time) *FakeClock { return &FakeClock{now: t} }

// Now returns the current fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the fake clock forward by d.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Set sets the fake clock to t.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}
