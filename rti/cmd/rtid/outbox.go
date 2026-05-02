package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// multiOutbox is the production implementation of both core.Outbox and
// grpc.SubscribableOutbox. It maintains one bounded channel per
// (federation, federate) pair; Send pushes to that channel, returning
// core.ErrFederateOverflow when the channel is full (matching the cut-1
// crash-on-overflow contract). Subscribe registers the channel and
// returns the read side + a cancel func that unregisters and closes.
//
// Concurrency: a single RWMutex guards the subscriber table. Send takes
// a read lock + does a non-blocking channel send; Subscribe/cancel take
// the write lock. The per-federate channel is goroutine-safe by Go's
// channel semantics.
type multiOutbox struct {
	mu          sync.RWMutex
	subscribers map[fedHandleKey]chan core.OutboundEvent
	bufferSize  int
}

type fedHandleKey struct {
	fed core.FederationName
	h   core.FederateHandle
}

// newMultiOutbox constructs an outbox where each per-federate channel is
// bounded at bufferSize. bufferSize <= 0 is normalized to 1 (a degenerate
// but legal value used by tests that exercise the overflow path).
func newMultiOutbox(bufferSize int) *multiOutbox {
	if bufferSize < 1 {
		bufferSize = 1
	}
	return &multiOutbox{
		subscribers: map[fedHandleKey]chan core.OutboundEvent{},
		bufferSize:  bufferSize,
	}
}

// Send implements core.Outbox. The federate's channel is bounded; on a
// full channel Send returns core.ErrFederateOverflow per the cut-1
// contract (federation manager treats the federate as crashed).
//
// "No subscriber" is silently dropped — the federate may not have
// established its outbound stream yet, which is a normal startup window
// rather than a crash condition.
func (m *multiOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	m.mu.RLock()
	ch, ok := m.subscribers[fedHandleKey{fed: fed, h: h}]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	select {
	case ch <- evt:
		return nil
	default:
		return fmt.Errorf("%w: federation %q federate %d", core.ErrFederateOverflow, fed, h)
	}
}

// Subscribe implements grpc.SubscribableOutbox. Returns the read-side
// channel and a cancel func that unregisters the subscriber and closes
// the channel.
//
// A second Subscribe for the same (fed, h) pair is rejected — the
// federate already owns the stream, and a duplicate subscribe would
// silently drop events on one of the two readers.
//
// The ctx parameter is preserved for symmetry with the gRPC handler;
// cancellation of the subscription is via the returned cancel func, not
// ctx, because the lifetime is owned by the streamService loop, not by
// the request context.
func (m *multiOutbox) Subscribe(_ context.Context, fed core.FederationName, h core.FederateHandle) (<-chan core.OutboundEvent, func() error, error) {
	key := fedHandleKey{fed: fed, h: h}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.subscribers[key]; dup {
		return nil, nil, fmt.Errorf("rtid: subscriber already registered for federation %q federate %d", fed, h)
	}
	ch := make(chan core.OutboundEvent, m.bufferSize)
	m.subscribers[key] = ch

	var cancelOnce sync.Once
	cancel := func() error {
		cancelOnce.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if existing, ok := m.subscribers[key]; ok && existing == ch {
				delete(m.subscribers, key)
				close(ch)
			}
		})
		return nil
	}
	return ch, cancel, nil
}

// Compile-time assertion that multiOutbox implements core.Outbox. The
// SubscribableOutbox assertion is performed at runtime by streamService
// (see rti/internal/transport/grpc/stream.go); we cannot assert it here
// without an import cycle (cmd/rtid imports transport/grpc to wire the
// composed Server, and SubscribableOutbox lives in that package).
var _ core.Outbox = (*multiOutbox)(nil)
