package object

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// fanoutDiscover sends DiscoverObjectInstance to every federate subscribed
// to the new object's class, EXCEPT the producer. Iteration order is
// sorted FederateHandle (see eligibleDiscoveryClasses), which is the
// determinism guarantee for replay.
//
// The registration path passes its publication snapshot so unrelated,
// unpublished attributes cannot cause default-region discovery.
func (r *RegistryOf[T]) fanoutDiscover(ctx context.Context, fed core.FederationName, st *federationStateOf[T], producer core.FederateHandle, inst *objectInstance, attrs []core.AttributeHandle) {
	inst.deliveryMu.Lock()
	defer inst.deliveryMu.Unlock()
	classes := r.eligibleDiscoveryClasses(ctx, fed, inst, attrs)
	for _, sub := range sortedDiscoveryRecipients(classes) {
		if sub == producer {
			// HLA semantics: a federate that owns / publishes an
			// instance never receives Discover for that instance,
			// even when subscribed to its class.
			continue
		}
		r.deliverDiscoveryLocked(ctx, fed, st, sub, inst, classes[sub])
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
// Recipient resolution reuses eligibleDiscoveryClasses — the exact set
// the register-time fan-out would target — so declaration-based and
// DDM region-scoped (RegionSubscribersFor, M36 DC-1) subscriptions
// behave identically in both orderings. Idempotency per (subscriber,
// object) is enforced through federationState.discovered, which the
// register-time path also records.
//
// The cls/attrs hook arguments are advisory only: membership in
// eligibleDiscoveryClasses is the authoritative filter, so a hook
// invocation never discovers an instance the register-time path would
// not have.
func (r *RegistryOf[T]) ObjectClassSubscribed(
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
		if inst.owner == internalProducer {
			continue
		}
		inst.deliveryMu.Lock()
		r.rediscoverIfRelevantLocked(ctx, fed, st, sub, inst)
		inst.deliveryMu.Unlock()
		r.FinishDeletionDelivery(fed)
	}
}

func (r *RegistryOf[T]) rediscoverIfRelevantLocked(ctx context.Context, fed core.FederationName, st *federationStateOf[T], sub core.FederateHandle, inst *objectInstance) {
	if !r.alreadyDiscovered(st, sub, inst.handle) {
		if cls := r.eligibleDiscoveryClasses(ctx, fed, inst, r.classAttributes(ctx, fed, inst.cls))[sub]; cls != 0 {
			r.deliverDiscoveryLocked(ctx, fed, st, sub, inst, cls)
		}
	}
}

// The instance delivery lock prevents failed sends from rolling back a newer
// discovery. Read-only queries may re-enter while Send observes the callback.
func (r *RegistryOf[T]) deliverDiscoveryLocked(ctx context.Context, fed core.FederationName, st *federationStateOf[T], sub core.FederateHandle, inst *objectInstance, cls core.ObjectClassHandle) {
	if cls == 0 {
		return
	}
	key := discoverKey{sub: sub, obj: inst.handle}
	st.mu.Lock()
	_, known := st.discovered[key]
	if known || st.instances[inst.handle] != inst {
		st.mu.Unlock()
		return
	}
	if deletion, pending := st.deletions[inst.handle]; pending {
		st.mu.Unlock()
		r.discoverPendingDeletionLocked(ctx, fed, st, sub, inst, deletion, cls)
		return
	}
	seq := st.nextOutboundSeqLocked()
	st.discovered[key] = cls
	st.mu.Unlock()
	evt := &CallbackEventOf[T]{Sequence: seq, Kind: CallbackDiscover, Object: inst.handle, ObjectClass: cls, ObjectName: inst.name, Metadata: metadataForOf[T](inst.owner, nil, 0)}
	if err := r.opts.Outbox.Send(ctx, fed, sub, evt); err != nil {
		st.mu.Lock()
		st.forgetKnowledgeLocked(key)
		st.mu.Unlock()
	}
}

// markDiscovered records that sub has been sent Discover for obj.
func (r *RegistryOf[T]) markDiscovered(st *federationStateOf[T], sub core.FederateHandle, obj core.ObjectHandle) {
	st.mu.Lock()
	key := discoverKey{sub: sub, obj: obj}
	if _, known := st.discovered[key]; !known {
		st.discovered[key] = st.registeredClass(obj)
	}
	st.mu.Unlock()
}

// alreadyDiscovered reports whether sub has been sent Discover for obj.
func (r *RegistryOf[T]) alreadyDiscovered(st *federationStateOf[T], sub core.FederateHandle, obj core.ObjectHandle) bool {
	st.mu.Lock()
	_, ok := st.discovered[discoverKey{sub: sub, obj: obj}]
	st.mu.Unlock()
	return ok
}

func sortedDiscoveryRecipients(classes map[core.FederateHandle]core.ObjectClassHandle) []core.FederateHandle {
	var out []core.FederateHandle
	for sub := range classes {
		out = append(out, sub)
	}
	slices.Sort(out)
	return out
}

func (r *RegistryOf[T]) eligibleDiscoveryClasses(ctx context.Context, fed core.FederationName, inst *objectInstance, attrs []core.AttributeHandle) map[core.FederateHandle]core.ObjectClassHandle {
	out := make(map[core.FederateHandle]core.ObjectClassHandle)
	for sub, cls := range r.candidateDiscoveryClasses(ctx, fed, inst, attrs...) {
		for _, attr := range attrs {
			if r.recipientInterested(ctx, fed, inst, sub, cls, attr, true) {
				out[sub] = cls
				break
			}
		}
	}
	return out
}

// RefreshDiscoveries is a post-commit hook for region/ownership changes that
// can make an existing object newly relevant without changing subscriptions.
// Composition calls it only after successful mutation admission and releasing
// object/ownership/DDM service locks. Known classes are never replaced.
func (r *RegistryOf[T]) RefreshDiscoveries(ctx context.Context, fed core.FederationName) {
	st := r.stateFor(fed)
	st.mu.Lock()
	var instances []*objectInstance
	for _, inst := range st.instances {
		if inst.owner != internalProducer {
			instances = append(instances, inst)
		}
	}
	st.mu.Unlock()
	slices.SortFunc(instances, func(a, b *objectInstance) int {
		if a.handle < b.handle {
			return -1
		}
		if a.handle > b.handle {
			return 1
		}
		return 0
	})
	for _, inst := range instances {
		inst.deliveryMu.Lock()
		classes := r.eligibleDiscoveryClasses(ctx, fed, inst, r.classAttributes(ctx, fed, inst.cls))
		for _, sub := range sortedDiscoveryRecipients(classes) {
			r.deliverDiscoveryLocked(ctx, fed, st, sub, inst, classes[sub])
		}
		inst.deliveryMu.Unlock()
		r.FinishDeletionDelivery(fed)
	}
}
