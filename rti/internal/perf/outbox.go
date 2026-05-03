package perf

import (
	"context"
	"fmt"
	"sync"

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
type perfOutbox struct {
	mu          sync.RWMutex
	subscribers map[fedHandleKey]chan core.OutboundEvent
	bufferSize  int
}

type fedHandleKey struct {
	fed core.FederationName
	h   core.FederateHandle
}

func newPerfOutbox(bufferSize int) *perfOutbox {
	if bufferSize < 1 {
		bufferSize = 1
	}
	return &perfOutbox{
		subscribers: map[fedHandleKey]chan core.OutboundEvent{},
		bufferSize:  bufferSize,
	}
}

// Send implements core.Outbox.
func (o *perfOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.RLock()
	ch, ok := o.subscribers[fedHandleKey{fed: fed, h: h}]
	o.mu.RUnlock()
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
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, dup := o.subscribers[key]; dup {
		return nil, nil, fmt.Errorf("perf: subscriber already registered for federation %q federate %d", fed, h)
	}
	ch := make(chan core.OutboundEvent, o.bufferSize)
	o.subscribers[key] = ch
	var cancelOnce sync.Once
	cancel := func() error {
		cancelOnce.Do(func() {
			o.mu.Lock()
			defer o.mu.Unlock()
			if existing, ok := o.subscribers[key]; ok && existing == ch {
				delete(o.subscribers, key)
				close(ch)
			}
		})
		return nil
	}
	return ch, cancel, nil
}

// Compile-time assertion.
var _ core.Outbox = (*perfOutbox)(nil)
