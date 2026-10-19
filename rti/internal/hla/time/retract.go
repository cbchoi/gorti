package time

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// RequestRetraction is the §8.22 requestRetraction outbound event.
//
//revive:disable-next-line:exported
type RequestRetraction struct {
	seq              uint64 //nolint:unused // reserved for future stream-seq wiring; see grant.go.
	Sender           core.FederateHandle
	RetractionHandle uint64
}

// Seq satisfies core.OutboundEvent.
func (r *RequestRetraction) Seq() uint64 {
	if r == nil {
		return 0
	}
	return r.seq
}

// RetractMessageNotify retains the old count-only entry point. It uses the
// same strict transaction as RetractWithError; zero also represents rejection.
// New service callers use RetractWithError to preserve exception information.
func (m *ManagerOf[T]) RetractMessageNotify(
	ctx context.Context,
	fed core.FederationName,
	sender core.FederateHandle,
	retractionHandle uint64,
) int {
	ext := extOfOf[T](m)
	ext.mu.Lock()
	removed := 0
	for key, ns := range ext.states {
		if key.fed != fed {
			continue
		}
		for _, b := range ns.tsoBuffer {
			if b.sender == sender && b.retractionHandle == retractionHandle {
				removed++
			}
		}
	}
	ext.mu.Unlock()
	if err := m.RetractWithError(ctx, fed, sender, retractionHandle); err != nil {
		return 0
	}
	return removed
}
