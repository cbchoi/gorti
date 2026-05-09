package federate

// Event is the sealed interface implemented by every value emitted on
// a Federate's Events() channel. Concrete types are exported below;
// callers type-switch on them.
type Event interface {
	isFederateEvent()
}

// ReceiveInteraction corresponds to proto FederateEvent.receive
// (oneof tag 13). Parameters is keyed by the parameter NAME (the SDK
// resolves wire-side parameter handles back to names via the FOM
// tables built at join time).
type ReceiveInteraction struct {
	ClassName  string
	Parameters map[string][]byte
	// Timestamp is the per-interaction logical_time the sender supplied.
	// nil if the sender did not pass one (untimed interaction).
	Timestamp *float64
}

func (ReceiveInteraction) isFederateEvent() {}

// TimeAdvanceGrant corresponds to proto FederateEvent.grant (oneof tag 14).
// Cut-3 rtid does not produce these cross-process (timeService=nil),
// but the type is exposed so callers can prepare for the M-N wiring.
type TimeAdvanceGrant struct {
	Time float64
}

func (TimeAdvanceGrant) isFederateEvent() {}

// FederationHalted corresponds to proto FederateEvent.halted (terminal,
// oneof tag 99). When this arrives the Events channel will close
// shortly after; the SDK does not auto-resign in response — callers
// can decide whether to retry or unwind.
type FederationHalted struct {
	Reason string
}

func (FederationHalted) isFederateEvent() {}
