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
	if r.opts.OnInteractionSent != nil {
		r.opts.OnInteractionSent(fed, producer)
	}
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
	// Hoist the inner proto and the param-map defensive copy out of
	// the per-subscriber loop. The producer's input map is copied once
	// (so the producer can mutate post-call); the resulting map and
	// the *ReceiveInteraction wrapping it are read-only from here on
	// and shared by every per-subscriber FederateEvent. proto.Marshal
	// does not mutate, so concurrent serialization across subscriber
	// streams is safe.
	pb := &rtiv1.ReceiveInteraction{
		InteractionClassHandle: uint64(cls),
		Parameters:             copyParamMap(params),
	}
	if ts != nil {
		v := float64(*ts)
		pb.LogicalTime = &v
	}
	// Acquire a seq range once per fanout instead of once per
	// subscriber to cut N-1 lock acquires from the hot path.
	nSeq := 0
	for _, sub := range subs {
		if sub != producer {
			nSeq++
		}
	}
	if nSeq == 0 {
		return
	}
	st.mu.Lock()
	startSeq := st.nextOutboundSeqRangeLocked(nSeq)
	st.mu.Unlock()
	seq := startSeq
	for _, sub := range subs {
		if sub == producer {
			continue
		}
		evt := &outboundEvent{pb: &rtiv1.FederateEvent{
			Seq:   seq,
			Event: &rtiv1.FederateEvent_Receive{Receive: pb},
		}}
		seq++
		_ = r.opts.Outbox.Send(ctx, fed, sub, evt)
		if r.opts.OnInteractionDelivered != nil {
			r.opts.OnInteractionDelivered(fed, sub)
		}
	}
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
