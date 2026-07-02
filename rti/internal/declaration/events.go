// Registration / interaction advisory outbound events (IEEE
// 1516.1-2010 §5.10-§5.13) — M37 Agent EA.
//
// The declaration Manager was outbox-less ("pure") through M36; the
// advisories give it an OPTIONAL core.Outbox (SetAdvisoryOutbox) so
// publisher-relevant subscription flips can be pushed to publishers.
// When no outbox is wired the manager stays pure and emits nothing.

package declaration

import (
	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// advisoryOutbound wraps one of the four §5.10-§5.13 advisory protos.
// One adapter type suffices — the payloads are single-handle bodies and
// tests unwrap via Inner().
type advisoryOutbound struct {
	pb *rtiv1.FederateEvent
}

func (o *advisoryOutbound) Seq() uint64 {
	if o == nil || o.pb == nil {
		return 0
	}
	return o.pb.Seq
}
func (o *advisoryOutbound) Inner() *rtiv1.FederateEvent { return o.pb }

// startRegistrationEvent — §5.10 startRegistrationForObjectClass.
func startRegistrationEvent(cls core.ObjectClassHandle) *advisoryOutbound {
	return &advisoryOutbound{pb: &rtiv1.FederateEvent{
		Event: &rtiv1.FederateEvent_StartRegistration{
			StartRegistration: &rtiv1.StartRegistrationForObjectClass{
				ObjectClassHandle: uint64(cls),
			},
		},
	}}
}

// stopRegistrationEvent — §5.11 stopRegistrationForObjectClass.
func stopRegistrationEvent(cls core.ObjectClassHandle) *advisoryOutbound {
	return &advisoryOutbound{pb: &rtiv1.FederateEvent{
		Event: &rtiv1.FederateEvent_StopRegistration{
			StopRegistration: &rtiv1.StopRegistrationForObjectClass{
				ObjectClassHandle: uint64(cls),
			},
		},
	}}
}

// turnInteractionsOnEvent — §5.12 turnInteractionsOn.
func turnInteractionsOnEvent(cls core.InteractionClassHandle) *advisoryOutbound {
	return &advisoryOutbound{pb: &rtiv1.FederateEvent{
		Event: &rtiv1.FederateEvent_TurnInteractionsOn{
			TurnInteractionsOn: &rtiv1.TurnInteractionsOn{
				InteractionClassHandle: uint64(cls),
			},
		},
	}}
}

// turnInteractionsOffEvent — §5.13 turnInteractionsOff.
func turnInteractionsOffEvent(cls core.InteractionClassHandle) *advisoryOutbound {
	return &advisoryOutbound{pb: &rtiv1.FederateEvent{
		Event: &rtiv1.FederateEvent_TurnInteractionsOff{
			TurnInteractionsOff: &rtiv1.TurnInteractionsOff{
				InteractionClassHandle: uint64(cls),
			},
		},
	}}
}
