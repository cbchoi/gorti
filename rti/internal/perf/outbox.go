package perf

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// perfOutbox is the in-process outbox used by the perf harness. It
// satisfies core.Outbox AND the SubscribableOutbox shape (channel-per-
// federate). Size and Send semantics mirror cmd/rtid's multiOutbox so
// the harness measures the same fanout codepath production sees.
//
// On a full receiver channel Send drops the event silently rather than
// returning an error — the perf measurement explicitly accepts the
// bounded-overflow envelope (we report sent count + delivered samples
// separately in the result). This is different from cmd/rtid's
// crash-on-overflow contract, which is appropriate for production but
// would terminate the harness prematurely under sustained tight-loop
// load.
//
// Concurrency: the subscriber table is held in an atomic.Pointer so
// the hot Send path is a single atomic load + map lookup with no
// mutex acquire. Subscribe/cancel take writeMu, build a fresh map
// (copy-on-write) with the mutation applied, and Store the new
// pointer. Reads see either the pre-write or post-write snapshot
// atomically. Suited to read-mostly workloads (one Subscribe per
// federate at start, N Sends per fanout).
type perfOutbox struct {
	subs       atomic.Pointer[map[fedHandleKey]chan core.OutboundEvent]
	writeMu    sync.Mutex
	bufferSize int
}

type fedHandleKey struct {
	fed core.FederationName
	h   core.FederateHandle
}

func newPerfOutbox(bufferSize int) *perfOutbox {
	if bufferSize < 1 {
		bufferSize = 1
	}
	o := &perfOutbox{bufferSize: bufferSize}
	empty := map[fedHandleKey]chan core.OutboundEvent{}
	o.subs.Store(&empty)
	return o
}

// Send implements core.Outbox.
func (o *perfOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	subs := *o.subs.Load()
	ch, ok := subs[fedHandleKey{fed: fed, h: h}]
	if !ok {
		return nil
	}
	select {
	case ch <- evt:
		return nil
	default:
		// Drop on full — measurement-mode contract; see type doc.
		return nil
	}
}

// Subscribe registers a per-federate inbox.
func (o *perfOutbox) Subscribe(_ context.Context, fed core.FederationName, h core.FederateHandle) (<-chan core.OutboundEvent, func() error, error) {
	key := fedHandleKey{fed: fed, h: h}
	o.writeMu.Lock()
	defer o.writeMu.Unlock()
	current := *o.subs.Load()
	if _, dup := current[key]; dup {
		return nil, nil, fmt.Errorf("perf: subscriber already registered for federation %q federate %d", fed, h)
	}
	ch := make(chan core.OutboundEvent, o.bufferSize)
	next := make(map[fedHandleKey]chan core.OutboundEvent, len(current)+1)
	for k, v := range current {
		next[k] = v
	}
	next[key] = ch
	o.subs.Store(&next)
	var cancelOnce sync.Once
	cancel := func() error {
		cancelOnce.Do(func() {
			o.writeMu.Lock()
			defer o.writeMu.Unlock()
			cur := *o.subs.Load()
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
			o.subs.Store(&next)
			close(ch)
		})
		return nil
	}
	return ch, cancel, nil
}

// Compile-time assertion.
var _ core.Outbox = (*perfOutbox)(nil)
