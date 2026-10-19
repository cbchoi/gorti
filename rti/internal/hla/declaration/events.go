// Registration and interaction advisories (IEEE 1516.1-2010
// sections 5.10-5.13).
package declaration

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// AdvisoryKind identifies one declaration-management callback without tying
// the service to a transport encoding.
type AdvisoryKind uint8

const (
	AdvisoryStartRegistration AdvisoryKind = iota + 1
	AdvisoryStopRegistration
	AdvisoryTurnInteractionsOn
	AdvisoryTurnInteractionsOff
)

// AdvisoryEvent is the transport-neutral declaration callback emitted by the
// declaration manager. The gRPC adapter maps it to the corresponding
// FederateEvent oneof.
type AdvisoryEvent struct {
	Sequence         uint64
	Kind             AdvisoryKind
	ObjectClass      core.ObjectClassHandle
	InteractionClass core.InteractionClassHandle
}

func (e *AdvisoryEvent) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.Sequence
}

func startRegistrationEvent(cls core.ObjectClassHandle) *AdvisoryEvent {
	return &AdvisoryEvent{Kind: AdvisoryStartRegistration, ObjectClass: cls}
}

func stopRegistrationEvent(cls core.ObjectClassHandle) *AdvisoryEvent {
	return &AdvisoryEvent{Kind: AdvisoryStopRegistration, ObjectClass: cls}
}

func turnInteractionsOnEvent(cls core.InteractionClassHandle) *AdvisoryEvent {
	return &AdvisoryEvent{Kind: AdvisoryTurnInteractionsOn, InteractionClass: cls}
}

func turnInteractionsOffEvent(cls core.InteractionClassHandle) *AdvisoryEvent {
	return &AdvisoryEvent{Kind: AdvisoryTurnInteractionsOff, InteractionClass: cls}
}
