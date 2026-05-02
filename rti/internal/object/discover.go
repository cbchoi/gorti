package object

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// fanoutDiscover sends DiscoverObjectInstance to every federate subscribed
// to the new object's class, EXCEPT the producer. Iteration order matches
// declaration.Manager.SubscribersFor (sorted FederateHandle), which is the
// determinism guarantee for replay.
//
// Cut-1 simplification: we probe a small fixed attribute range
// (fanoutAttrProbe). HLA semantics require Discover to fire when a
// federate is subscribed to ANY published attribute of the class; FOM-
// driven enumeration of all class attributes is the proper source. The
// follow-up task is captured at registry.go's fanoutAttrProbe doc.
func (r *Registry) fanoutDiscover(ctx context.Context, fed core.FederationName, st *federationState, producer core.FederateHandle, inst *objectInstance) {
	subs := r.opts.Declarations.SubscribersFor(ctx, fed, inst.cls, fanoutAttrProbe)
	for _, sub := range subs {
		if sub == producer {
			// HLA semantics: a federate that owns / publishes an
			// instance never receives Discover for that instance,
			// even when subscribed to its class.
			continue
		}
		evt := r.buildDiscoverEvent(st, inst)
		// Outbox.Send errors are recorded by the federation manager as
		// "federate crashed"; the registry treats them as best-effort
		// (the durable eventlog already has the registration). Per
		// SRS NFR-CRASH-1.
		_ = r.opts.Outbox.Send(ctx, fed, sub, evt)
	}
}

// buildDiscoverEvent allocates one outbound DiscoverObjectInstance per
// subscriber (never shared) so downstream stream-multiplexing can mutate
// per-subscriber metadata without aliasing. The per-federation outbound
// seq is assigned here.
func (r *Registry) buildDiscoverEvent(st *federationState, inst *objectInstance) *outboundEvent {
	st.mu.Lock()
	seq := st.nextOutboundSeqLocked()
	st.mu.Unlock()
	return &outboundEvent{pb: &rtiv1.FederateEvent{
		Seq: seq,
		Event: &rtiv1.FederateEvent_Discover{Discover: &rtiv1.DiscoverObjectInstance{
			ObjectHandle:      uint64(inst.handle),
			ObjectClassHandle: uint64(inst.cls),
			ObjectName:        inst.name,
		}},
	}}
}
