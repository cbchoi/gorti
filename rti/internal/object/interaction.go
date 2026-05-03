package object

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// sendInteraction implements core.ObjectRegistry.SendInteraction.
//
// Symmetric to UpdateAttributes for interactions:
//  1. Verify the producer publishes the interaction class via
//     declaration.Manager. Reject with ErrInteractionClassNotPublished
//     otherwise.
//  2. Persist InteractionSent to the EventLog (write-ahead).
//  3. Resolve delivery order: when the federation is best-effort AND the
//     interaction class is declared OrderReceive in the FOM, the
//     outbound event drops its timestamp (RO). Otherwise TSO. See
//     TASK-077.
//  4. Fan ReceiveInteraction to subscribers in sorted FederateHandle
//     order, EXCEPT the producer.
func (r *Registry) sendInteraction(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	cls core.InteractionClassHandle,
	params map[core.ParameterHandle][]byte,
	ts *core.LogicalTime,
) error {
	if producer == core.InvalidFederateHandle {
		return core.ErrFederateNotJoined
	}
	if !r.producerPublishesInteraction(ctx, fed, producer, cls) {
		return core.ErrInteractionClassNotPublished
	}

	st := r.stateFor(fed)

	if r.opts.EventLog != nil {
		ev := newInteractionSentEvent(cls, producer, params, ts, r.opts.Clock.Now().UnixNano())
		if err := r.opts.EventLog.Append(ctx, fed, ev); err != nil {
			return fmt.Errorf("object: send interaction class %d in %q: eventlog append: %w", cls, fed, err)
		}
	}

	deliveryTs := r.deliveryTimestampForInteraction(fed, cls, ts)
	r.fanoutReceive(ctx, fed, st, producer, cls, params, deliveryTs)
	return nil
}

// deliveryTimestampForInteraction is the interaction-side analogue of
// deliveryTimestampForAttributes. See that function's doc for the
// best-effort-mode-plus-OrderReceive contract.
func (r *Registry) deliveryTimestampForInteraction(
	fed core.FederationName,
	cls core.InteractionClassHandle,
	ts *core.LogicalTime,
) *core.LogicalTime {
	if ts == nil {
		return nil
	}
	if r.opts.Federations == nil || r.opts.Orders == nil {
		return ts
	}
	mode, ok := r.opts.Federations.ModeFor(fed)
	if !ok || mode != core.ModeBestEffort {
		return ts
	}
	ord, known := r.opts.Orders.InteractionOrder(fed, cls)
	if !known || ord != OrderReceive {
		return ts
	}
	return nil
}

func (r *Registry) producerPublishesInteraction(ctx context.Context, fed core.FederationName, producer core.FederateHandle, cls core.InteractionClassHandle) bool {
	for _, h := range r.opts.Declarations.InteractionPublishersFor(ctx, fed, cls) {
		if h == producer {
			return true
		}
	}
	return false
}

func (r *Registry) fanoutReceive(ctx context.Context, fed core.FederationName, st *federationState, producer core.FederateHandle, cls core.InteractionClassHandle, params map[core.ParameterHandle][]byte, ts *core.LogicalTime) {
	subs := r.opts.Declarations.InteractionSubscribersFor(ctx, fed, cls)
	for _, sub := range subs {
		if sub == producer {
			continue
		}
		evt := r.buildReceiveEvent(st, cls, params, ts)
		_ = r.opts.Outbox.Send(ctx, fed, sub, evt)
	}
}

func (r *Registry) buildReceiveEvent(st *federationState, cls core.InteractionClassHandle, params map[core.ParameterHandle][]byte, ts *core.LogicalTime) *outboundEvent {
	st.mu.Lock()
	seq := st.nextOutboundSeqLocked()
	st.mu.Unlock()
	pb := &rtiv1.ReceiveInteraction{
		InteractionClassHandle: uint64(cls),
		Parameters:             copyParamMap(params),
	}
	if ts != nil {
		v := float64(*ts)
		pb.LogicalTime = &v
	}
	return &outboundEvent{pb: &rtiv1.FederateEvent{
		Seq:   seq,
		Event: &rtiv1.FederateEvent_Receive{Receive: pb},
	}}
}

func copyParamMap(params map[core.ParameterHandle][]byte) map[uint64][]byte {
	out := make(map[uint64][]byte, len(params))
	for k, v := range params {
		out[uint64(k)] = v
	}
	return out
}

func newInteractionSentEvent(cls core.InteractionClassHandle, producer core.FederateHandle, params map[core.ParameterHandle][]byte, ts *core.LogicalTime, wallNs int64) *eventRecord {
	pb := &rtiv1.InteractionSent{
		InteractionClassHandle: uint64(cls),
		ProducerFederateHandle: uint64(producer),
		Parameters:             copyParamMap(params),
	}
	if ts != nil {
		v := float64(*ts)
		pb.LogicalTime = &v
	}
	return &eventRecord{pb: &rtiv1.Event{
		WallNs: uint64(wallNs),
		Body:   &rtiv1.Event_InterSent{InterSent: pb},
	}}
}
