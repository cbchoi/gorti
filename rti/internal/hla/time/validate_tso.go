// Package time — outgoing-TSO timestamp validation (M37 EB-3).
//
// IEEE 1516.1-2010 §8.1.4 and §8.1.6 Table 3 (and the InvalidLogicalTime preconditions on
// §6.10 updateAttributeValues / §6.12 sendInteraction / §6.14
// deleteObjectInstance): a time-regulating federate promises never to
// send a TimeStampOrdered message with timestamp below its current
// logical time plus lookahead, or requested time plus lookahead while
// advancing. Constrained-only and non-time-aware
// senders carry no lookahead constraint and are exempt.
//
// The check is consumed by object.Registry's send/update/delete
// ingestion points via the object.OutgoingTSOValidator interface; the
// Manager satisfies it directly (wired through cmd/rtid alongside the
// M22 TSOGate).
package time

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ValidateOutgoingTSO reports whether a TSO message stamped ts may be
// sent by federate h. Returns nil for non-regulating senders (no
// lookahead constraint) and for regulating senders with
// ts >= timeBase + actual lookahead, where timeBase is the requested logical
// time while advancing and the current logical time otherwise. At zero
// lookahead, TAR/NMR exclude equality (including same-time grant history under
// 8.1.4). Otherwise returns an error
// wrapping core.ErrTimeInvalidLogicalTime.
//
// Read regulation and advance state atomically, in their shared lock order.
// Do not acquire the evaluator lease: atomic object admission already holds it.
func (m *ManagerOf[T]) ValidateOutgoingTSO(fed core.FederationName, h core.FederateHandle, ts T) error {
	if integerScalar[T]() && ts < 0 {
		return core.ErrTimeInvalidLogicalTime
	}
	m.states.mu.Lock()
	defer m.states.mu.Unlock()
	fs := m.states.getLocked(fed, h)
	if fs == nil || !fs.regulating {
		return nil
	}

	ext := extOfOf[T](m)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	timeBase, lookahead, exclusive := outgoingTSOBoundOf[T](fs, ext.getLocked(fed, h))

	floor := addHorizon(timeBase, lookahead)
	if !floor.Unbounded && timestampAtOrBeyond(ts, floor.Limit, exclusive) || !integerScalar[T]() && timestampAtOrBeyond(ts, horizonScalar(floor), exclusive) {
		return nil
	}
	return fmt.Errorf("%w: timestamp %v violates time-management base %v + lookahead %v (exclusive=%t)",
		core.ErrTimeInvalidLogicalTime, ts, timeBase, lookahead, exclusive)
}
