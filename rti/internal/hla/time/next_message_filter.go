package time

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// deliverNextMessageGroup runs only after the earliest timestamp is safe and
// permitted by a scheduled save. Subscription filtering happens once at actual
// delivery; a completely discarded group must not produce an empty early TAG.
// The caller holds the evaluator lease and reevaluates the target afterwards.
func (m *ManagerOf[T]) deliverNextMessageGroup(ctx context.Context, fed core.FederationName, h core.FederateHandle, at T) (bool, error) {
	if m.messageFilter == nil {
		return false, nil
	}
	ext := extOfOf[T](m)
	m.states.mu.Lock()
	ext.mu.Lock()
	fs, ns := m.states.getLocked(fed, h), ext.getLocked(fed, h)
	hasGroup := false
	if fs != nil && fs.constrained && ns != nil && ns.pendingNER && ns.mode.usesNextMessageTarget() {
		for _, b := range ns.tsoBuffer {
			if b.timestamp <= at && !internalTSO(b.event) {
				hasGroup = true
				break
			}
		}
	}
	ext.mu.Unlock()
	m.states.mu.Unlock()
	if !hasGroup {
		return false, nil
	}
	release := append(m.takeBufferedRO(fed, h), m.takeBufferedTSO(fed, h, &at, true)...)
	// Successful data prefixes record the request's delivered minimum before
	// any retry; the existing delivery path restores only the unsent suffix.
	return true, m.deliverBufferedSequence(ctx, fed, h, release)
}
