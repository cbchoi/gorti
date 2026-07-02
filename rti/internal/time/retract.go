// §8.22 requestRetraction (M37 Agent EA).
//
// RetractMessageNotify extends the M20.2 RetractMessage with the §8.22
// notification: every federate whose per-federate TSO buffer held a
// matching entry (i.e. WOULD have received the message on a later
// grant) is sent a RequestRetraction event identifying the retracted
// message by its (sender, retraction handle) pair.
//
// Layering (M21 TASK-204b): the time package must not import the
// generated proto, so RequestRetraction is a plain struct translated
// in transport/grpc/stream.go's toFederateEvent type-switch — the same
// path TimeAdvanceGrant and FederationHalted take.

package time

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/core"
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

// RetractMessageNotify removes every buffered TSO event matching
// (sender, retractionHandle) — exactly like RetractMessage — and then
// emits RequestRetraction to each affected recipient (§8.22). Returns
// the count removed. The frozen core.TSODeliveryGate.RetractMessage
// keeps its no-notify shape; callers (grpc handler → object.Registry)
// duck-type for this richer method.
func (m *Manager) RetractMessageNotify(
	ctx context.Context,
	fed core.FederationName,
	sender core.FederateHandle,
	retractionHandle uint64,
) int {
	if retractionHandle == 0 {
		return 0
	}
	ext := extOf(m)
	ext.mu.Lock()
	removed := 0
	var affected []core.FederateHandle
	for key, ns := range ext.states {
		if key.fed != fed {
			continue
		}
		filtered := ns.tsoBuffer[:0]
		hit := false
		for _, b := range ns.tsoBuffer {
			if b.sender == sender && b.retractionHandle == retractionHandle {
				removed++
				hit = true
				continue
			}
			filtered = append(filtered, b)
		}
		if hit {
			affected = append(affected, key.h)
			ns.tsoBuffer = append([]bufferedTSOEvent(nil), filtered...)
		}
	}
	ext.mu.Unlock()

	slices.Sort(affected)
	for _, h := range affected {
		_ = m.opts.Outbox.Send(ctx, fed, h, &RequestRetraction{
			Sender:           sender,
			RetractionHandle: retractionHandle,
		})
	}
	return removed
}
