package m11spec

import (
	"context"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

type sentRecord struct {
	Federation core.FederationName
	Federate   core.FederateHandle
	Event      core.OutboundEvent
}

type fakeOutbox struct {
	mu   sync.Mutex
	sent []sentRecord
}

func newFakeOutbox() *fakeOutbox { return &fakeOutbox{} }

func (o *fakeOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, sentRecord{fed, h, evt})
	return nil
}

func (o *fakeOutbox) Sent() []sentRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]sentRecord, len(o.sent))
	copy(out, o.sent)
	return out
}
