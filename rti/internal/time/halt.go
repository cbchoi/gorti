package time

import "github.com/cbchoi/gorti/rti/internal/core"

// HaltCauseStall is the canonical value used in FederationHalted.Cause
// when the halt was triggered by Manager.CheckStalls detecting an
// outstanding NER older than the federation's StallTimeout. Future halt
// causes (operator-driven, crash-driven) will introduce sibling
// constants here.
const HaltCauseStall = "stall"

// FederationHalted is the cut-1 core.OutboundEvent emitted by Manager
// to every federate of a federation that has just transitioned to the
// halted terminal state. Mirrors TimeAdvanceGrant's shape so the gRPC
// stream multiplexer can wrap it into rtiv1.FederateEvent without a
// translation table.
//
// StalledFederate names the federate whose pending NER triggered the
// detection. The receiving federate may equal StalledFederate (the
// stalled peer is told "you stalled the federation") or a sibling
// (a peer is told "<other federate> stalled the federation").
//
// The seq field is currently always zero — the gRPC stream stamps a
// per-federate monotonic seq when wrapping into FederateEvent. Keeping
// the field preserves API symmetry with TimeAdvanceGrant.
//
//revive:disable-next-line:exported
type FederationHalted struct {
	seq             uint64 //nolint:unused // reserved for future stream-seq wiring; see doc.
	Cause           string
	StalledFederate core.FederateHandle
}

// Seq satisfies core.OutboundEvent.
func (h *FederationHalted) Seq() uint64 {
	if h == nil {
		return 0
	}
	return h.seq
}

// federationHaltedRecord is the cut-1 core.EventRecord recording that
// a federation has been halted. Mirrors timeAdvanceGrantedRecord (W2)
// in shape so the permissive log used by spec tests can record it
// without a proto adapter; cmd/rtid wires the production path through
// a proto adapter in M3 Wave 4.
type federationHaltedRecord struct {
	seq             uint64 //nolint:unused // assigned by eventlog writer via reflection.
	Cause           string
	StalledFederate core.FederateHandle
}

// Seq satisfies core.EventRecord.
func (r *federationHaltedRecord) Seq() uint64 {
	if r == nil {
		return 0
	}
	return r.seq
}
