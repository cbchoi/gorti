package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// multiOutbox is the production implementation of both core.Outbox and
// grpc.SubscribableOutbox. It maintains one bounded channel per
// (federation, federate) pair; Send pushes to that channel, returning
// core.ErrFederateOverflow when the channel is full (matching the cut-1
// crash-on-overflow contract). Subscribe registers the channel and
// returns the read side + a cancel func that unregisters and closes.
//
// Concurrency: the subscriber table is held in an atomic.Pointer so
// the hot Send path is a single atomic load + map lookup with no
// mutex acquire. Subscribe / cancel serialize on writeMu, build a
// fresh map (copy-on-write) with the mutation applied, and Store the
// new pointer. Concurrent readers see either the pre- or post-write
// snapshot atomically. Tuned for the production read-mostly profile
// (one Subscribe per federate at join, N Sends per fanout). The
// per-federate channel itself is goroutine-safe by Go's channel
// semantics.
type multiOutbox struct {
	subs       atomic.Pointer[map[fedHandleKey]chan core.OutboundEvent]
	writeMu    sync.Mutex
	bufferSize int
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
	m := &multiOutbox{bufferSize: bufferSize}
	empty := map[fedHandleKey]chan core.OutboundEvent{}
	m.subs.Store(&empty)
	return m
}

// Send implements core.Outbox. The federate's channel is bounded; on a
// full channel Send returns core.ErrFederateOverflow per the cut-1
// contract (federation manager treats the federate as crashed).
//
// "No subscriber" is silently dropped — the federate may not have
// established its outbound stream yet, which is a normal startup window
// rather than a crash condition.
func (m *multiOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	subs := *m.subs.Load()
	ch, ok := subs[fedHandleKey{fed: fed, h: h}]
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
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	current := *m.subs.Load()
	if _, dup := current[key]; dup {
		return nil, nil, fmt.Errorf("rtid: subscriber already registered for federation %q federate %d", fed, h)
	}
	ch := make(chan core.OutboundEvent, m.bufferSize)
	next := make(map[fedHandleKey]chan core.OutboundEvent, len(current)+1)
	for k, v := range current {
		next[k] = v
	}
	next[key] = ch
	m.subs.Store(&next)

	var cancelOnce sync.Once
	cancel := func() error {
		cancelOnce.Do(func() {
			m.writeMu.Lock()
			defer m.writeMu.Unlock()
			cur := *m.subs.Load()
			existing, ok := cur[key]
			if !ok || existing != ch {
				return
			}
			next := make(map[fedHandleKey]chan core.OutboundEvent, len(cur)-1)
			for k, v := range cur {
				if k != key {
					next[k] = v
				}
			}
			m.subs.Store(&next)
			close(ch)
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
