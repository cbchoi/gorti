// Package time — outgoing-TSO timestamp validation (M37 EB-3).
//
// IEEE 1516.1-2010 §8.1.2 (and the InvalidLogicalTime preconditions on
// §6.10 updateAttributeValues / §6.12 sendInteraction / §6.14
// deleteObjectInstance): a time-regulating federate promises never to
// send a TimeStampOrdered message with timestamp below its current
// logical time plus lookahead. Constrained-only and non-time-aware
// senders carry no lookahead constraint and are exempt.
//
// The check is consumed by object.Registry's send/update/delete
// ingestion points via the object.OutgoingTSOValidator interface; the
// Manager satisfies it directly (wired through cmd/rtid alongside the
// M22 TSOGate).

package time

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// tsoBoundaryEpsilon absorbs float64 addition noise at the legality
// boundary: a federate granted to 0.1 with lookahead 0.2 computes the
// floor 0.30000000000000004, but a sender at the decimal boundary 0.3
// is spec-legal (ts >= current + lookahead). The epsilon is far below
// any logical-time resolution the fixtures (or HLAfloat64Time users)
// exercise.
const tsoBoundaryEpsilon = 1e-9

// ValidateOutgoingTSO reports whether a TSO message stamped ts may be
// sent by federate h. Returns nil for non-regulating senders (no
// lookahead constraint) and for regulating senders with
// ts >= currentTime + lookahead (boundary INCLUSIVE — the tm fixtures
// send at exactly current+lookahead). Otherwise returns an error
// wrapping core.ErrTimeInvalidLogicalTime.
//
// Concurrency: takes stateStore.mu then nerStore.mu, never both at
// once — same discipline as ShouldDeliverNow.
func (m *Manager) ValidateOutgoingTSO(fed core.FederationName, h core.FederateHandle, ts core.LogicalTime) error {
	m.states.mu.Lock()
	fs := m.states.getLocked(fed, h)
	regulating := fs != nil && fs.regulating
	var lookahead core.LogicalTime
	if fs != nil {
		lookahead = fs.lookahead
	}
	m.states.mu.Unlock()
	if !regulating {
		return nil
	}

	ext := extOf(m)
	ext.mu.Lock()
	var current core.LogicalTime
	if ns := ext.getLocked(fed, h); ns != nil {
		current = ns.currentTime
	}
	ext.mu.Unlock()

	floor := float64(current) + float64(lookahead)
	if float64(ts)+tsoBoundaryEpsilon >= floor {
		return nil
	}
	return fmt.Errorf("%w: timestamp %g < logical time %g + lookahead %g",
		core.ErrTimeInvalidLogicalTime, float64(ts), float64(current), float64(lookahead))
}
