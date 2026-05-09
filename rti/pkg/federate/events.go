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

// RemoveObjectInstance corresponds to proto FederateEvent.remove (oneof
// tag 12) — IEEE 1516.1-2010 §6.16. Delivered to subscribers when an
// object instance owner calls DeleteObjectInstance. M23.
type RemoveObjectInstance struct {
	ObjectHandle    uint64
	Tag             []byte
	Timestamp       *float64 // nil = RO delete
}

func (RemoveObjectInstance) isFederateEvent() {}

// ProvideAttributeValueUpdate corresponds to proto
// FederateEvent.provide_update (oneof tag 15) — IEEE 1516.1-2010 §6.26.
// Delivered to the owner of an object instance when a peer calls
// RequestAttributeValueUpdate. The owner is expected to respond by
// calling Federate.UpdateAttributes with fresh values for the listed
// attribute set. M23 W2.
type ProvideAttributeValueUpdate struct {
	ObjectHandle     uint64
	AttributeHandles []uint64
	Tag              []byte
}

func (ProvideAttributeValueUpdate) isFederateEvent() {}
