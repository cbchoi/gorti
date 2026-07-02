package object

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// fanoutDiscover sends DiscoverObjectInstance to every federate subscribed
// to the new object's class, EXCEPT the producer. Iteration order is
// sorted FederateHandle (see subscribersForDiscover), which is the
// determinism guarantee for replay.
//
// Cut-1 simplification: we probe a small fixed attribute range
// (fanoutAttrProbe). HLA semantics require Discover to fire when a
// federate is subscribed to ANY published attribute of the class; FOM-
// driven enumeration of all class attributes is the proper source. The
// follow-up task is captured at registry.go's fanoutAttrProbe doc.
func (r *Registry) fanoutDiscover(ctx context.Context, fed core.FederationName, st *federationState, producer core.FederateHandle, inst *objectInstance) {
	subs := r.subscribersForDiscover(ctx, fed, inst)
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
		// M37 EB-4 — record delivery so the retroactive subscribe-time
		// path stays idempotent per (subscriber, object).
		r.markDiscovered(st, sub, inst.handle)
	}
}

// internalProducer mirrors mom.momProducer (max-uint64): the RTI-owned
// producer handle for MOM object instances. The generic retroactive
// discover path skips instances it owns — the MOM manager delivers its
// own retroactive Discover+Reflect pair for its classes (M36 DD-2),
// and double discovers would confuse late MOM subscribers. Real
// federate handles are small monotonic values and can never collide.
const internalProducer = ^core.FederateHandle(0)

// ObjectClassSubscribed is the declaration-manager post-subscribe hook
// (declaration.Manager.SetOnSubscribeObjectClass) for the object
// registry — M37 EB-4, generalizing the M36 DD-2 MOM pattern.
//
// IEEE 1516.1-2010 §6.9: discoverObjectInstance fires when an instance
// of a subscribed class becomes relevant, regardless of the
// subscribe/register ordering. The register-time fanoutDiscover only
// covers subscribe-first federates; this hook covers
// subscribe-after-register by retroactively sending Discover for every
// existing matching instance to the new subscriber.
//
// Recipient resolution reuses subscribersForDiscover — the exact set
// the register-time fan-out would target — so declaration-based and
// DDM region-scoped (RegionSubscribersFor, M36 DC-1) subscriptions
// behave identically in both orderings. Idempotency per (subscriber,
// object) is enforced through federationState.discovered, which the
// register-time path also records.
//
// The cls/attrs hook arguments are advisory only: membership in
// subscribersForDiscover is the authoritative filter, so a hook
// invocation never discovers an instance the register-time path would
// not have.
func (r *Registry) ObjectClassSubscribed(
	ctx context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	_ core.ObjectClassHandle,
	_ []core.AttributeHandle,
) {
	st := r.stateFor(fed)

	// Snapshot instances in sorted handle order for deterministic
	// retroactive delivery (NFR-DET-1).
	st.mu.Lock()
	insts := make([]*objectInstance, 0, len(st.instances))
	for _, inst := range st.instances {
		insts = append(insts, inst)
	}
	st.mu.Unlock()
	slices.SortFunc(insts, func(a, b *objectInstance) int {
		switch {
		case a.handle < b.handle:
			return -1
		case a.handle > b.handle:
			return 1
		default:
			return 0
		}
	})

	for _, inst := range insts {
		if inst.owner == sub || inst.owner == internalProducer {
			continue
		}
		if r.alreadyDiscovered(st, sub, inst.handle) {
			continue
		}
		if !slices.Contains(r.subscribersForDiscover(ctx, fed, inst), sub) {
			continue
		}
		evt := r.buildDiscoverEvent(st, inst)
		_ = r.opts.Outbox.Send(ctx, fed, sub, evt)
		r.markDiscovered(st, sub, inst.handle)
	}
}

// markDiscovered records that sub has been sent Discover for obj.
func (r *Registry) markDiscovered(st *federationState, sub core.FederateHandle, obj core.ObjectHandle) {
	st.mu.Lock()
	st.discovered[discoverKey{sub: sub, obj: obj}] = struct{}{}
	st.mu.Unlock()
}

// alreadyDiscovered reports whether sub has been sent Discover for obj.
func (r *Registry) alreadyDiscovered(st *federationState, sub core.FederateHandle, obj core.ObjectHandle) bool {
	st.mu.Lock()
	_, ok := st.discovered[discoverKey{sub: sub, obj: obj}]
	st.mu.Unlock()
	return ok
}

// subscribersForDiscover resolves the Discover recipient set: the
// declaration-based subscribers (cut-1) UNIONed with region-scoped
// subscribers (M36 DC-1), in sorted FederateHandle order.
//
// M36 DC-1 — DDM-aware discovery, mirroring subscribersForReflect:
// a federate that subscribed via SubscribeObjectClassAttributesWithRegions
// only (never a plain Subscribe) is invisible to
// declaration.Manager.SubscribersFor, so it received every
// region-filtered REFLECT but never the §6.9 discoverObjectInstance
// that must precede them. Per attribute:
//   - publisher regions associated with the instance → overlap-tested
//     subscribers (DDM.SubscribersForUpdate), same as the reflect path;
//   - no publisher regions (the common register-time case — gorti's
//     register/associate split lands associations AFTER Register) →
//     default-region semantics: the default region overlaps every
//     subscriber region, so ALL region-scoped subscribers of the class
//     attribute discover the instance (DDM.RegionSubscribersFor).
//
// FR-DDM-6 zero-cost contract: with no DDM filter wired this is one
// nil-check; with a filter but no region subscriptions it adds one map
// miss per probed attribute.
func (r *Registry) subscribersForDiscover(ctx context.Context, fed core.FederationName, inst *objectInstance) []core.FederateHandle {
	declSubs := r.opts.Declarations.SubscribersFor(ctx, fed, inst.cls, fanoutAttrProbe)
	if r.opts.DDM == nil {
		return declSubs
	}
	union := map[core.FederateHandle]struct{}{}
	for _, h := range declSubs {
		union[h] = struct{}{}
	}
	regionHit := false
	for _, attr := range fanoutAttrProbe {
		var subs []core.FederateHandle
		if pubRegions := r.opts.DDM.PublisherRegionsFor(fed, inst.handle, attr); len(pubRegions) > 0 {
			subs = r.opts.DDM.SubscribersForUpdate(fed, inst.cls, attr, pubRegions)
		} else {
			subs = r.opts.DDM.RegionSubscribersFor(fed, inst.cls, attr)
		}
		for _, h := range subs {
			regionHit = true
			union[h] = struct{}{}
		}
	}
	if !regionHit {
		// Preserve the cut-1 slice (already sorted by the declaration
		// manager) when DDM contributed nothing.
		return declSubs
	}
	out := make([]core.FederateHandle, 0, len(union))
	for h := range union {
		out = append(out, h)
	}
	slices.Sort(out)
	return out
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
