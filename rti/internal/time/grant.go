package time

import "github.com/cbchoi/gorti/rti/internal/core"

// TimeAdvanceGrant is the cut-1 core.OutboundEvent emitted by Manager
// when a federate's Next-Event-Request can be satisfied. Production
// (cmd/rtid) wraps these values into the generated rtiv1.FederateEvent
// at the gRPC stream boundary; this struct is the package-internal
// payload travelling through core.Outbox to the stream multiplexer.
//
// The seq field is currently always zero — the gRPC stream is what
// stamps a per-federate monotonic seq when wrapping into FederateEvent.
// Keeping the field (rather than dropping the Seq() method) preserves
// API symmetry with future event types and lets the writer's reflection
// path assign a value if a code path ever flows TimeAdvanceGrant
// through the binary event log.
//
// The TimeAdvance prefix mirrors the proto message name
// (rtiv1.TimeAdvanceGrant) so the stream multiplexer can map between
// them without a translation table; the apparent "stutter" of
// time.TimeAdvanceGrant is intentional and matches the IDD.
//
//revive:disable-next-line:exported
type TimeAdvanceGrant struct {
	seq  uint64 //nolint:unused // reserved for future stream-seq wiring; see doc.
	Time core.LogicalTime
}

// Seq satisfies core.OutboundEvent.
func (g *TimeAdvanceGrant) Seq() uint64 {
	if g == nil {
		return 0
	}
	return g.seq
}

// timeAdvanceGrantedRecord is the cut-1 core.EventRecord recording the
// granting of a NER (post-condition: federate.currentTime advances).
// Cut-1 spec tests use a permissiveEventLog that does not depend on
// proto marshaling, so a minimal struct with an unexported seq field
// (writable via the eventlog writer's reflection path) suffices.
// cmd/rtid wires the production path through a proto adapter
// (object.eventRecord) — that work is M3 Wave 4's concern.
//
// The companion TimeAdvanceRequested record is intentionally NOT
// emitted in cut-1: the NER-request → grant pair lives in the same
// Manager method on the same goroutine, so a single TimeAdvanceGranted
// suffices for replay. M4 may split the pair when NER becomes
// asynchronous (queued grants resolved by stall detection).
type timeAdvanceGrantedRecord struct {
	seq      uint64 //nolint:unused // assigned by eventlog writer via reflection.
	Federate core.FederateHandle
	Time     core.LogicalTime
}

// Seq satisfies core.EventRecord.
func (r *timeAdvanceGrantedRecord) Seq() uint64 {
	if r == nil {
		return 0
	}
	return r.seq
}
