package object

import (
	"context"
	"slices"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ResignDeletionPlan holds the registry lock until Commit or Release. The
// enclosing resign operation journals once and reserves all callbacks before
// committing this plan and the ownership plan.
type ResignDeletionPlanOf[T core.LogicalTimeScalar] struct {
	r              *RegistryOf[T]
	st             *federationStateOf[T]
	fed            core.FederationName
	h              core.FederateHandle
	objects        []core.ObjectHandle
	pending        []core.ObjectHandle
	transportation []attrTransportKey
	ddmCommit      func()
	deliveries     []core.OutboxDelivery
	once           sync.Once
	finish         sync.Once
	committed      atomic.Bool
	pinMu          sync.Mutex
	active         bool
}

func (p *ResignDeletionPlanOf[T]) Objects() []core.ObjectHandle { return slices.Clone(p.objects) }
func (p *ResignDeletionPlanOf[T]) Deliveries() []core.OutboxDelivery {
	return slices.Clone(p.deliveries)
}

func (p *ResignDeletionPlanOf[T]) RegisteredClass(obj core.ObjectHandle) core.ObjectClassHandle {
	p.pinMu.Lock()
	defer p.pinMu.Unlock()
	if !p.active || p.st.instances[obj] == nil {
		return 0
	}
	return p.st.instances[obj].cls
}

// ProjectKnownObjectAttributes uses the plan's existing object-state pin. The
// enclosing resignation must project callbacks before CommitState, rather than
// recursively acquiring the registry mutex through the ordinary query port.
func (p *ResignDeletionPlanOf[T]) ProjectKnownObjectAttributes(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, []core.AttributeHandle, error) {
	p.pinMu.Lock()
	defer p.pinMu.Unlock()
	if !p.active {
		return 0, nil, core.ErrObjectHandleInvalid
	}
	if fed != p.fed {
		return 0, nil, core.ErrObjectHandleInvalid
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	// The ownership plan may already hold its write lock. A callback recipient
	// must have recorded discovery; never infer knowledge by querying ownership.
	inst := p.st.instances[obj]
	if inst == nil || p.st.discovered[discoverKey{sub: recipient, obj: obj}] == 0 {
		return 0, nil, core.ErrObjectHandleInvalid
	}
	mapped, err := p.r.actualAttributesAtKnownClass(ctx, fed, p.st, recipient, inst, attrs)
	if err != nil {
		return 0, nil, err
	}
	return p.st.knownClass(recipient, inst), mapped, nil
}

func (r *RegistryOf[T]) hasDeletePrivilege(ctx context.Context, fed core.FederationName, h core.FederateHandle, inst *objectInstance) bool {
	if r.opts.Ownership != nil {
		fom, err := r.opts.FOMs.Get(ctx, fed)
		if err != nil || fom == nil || !fom.IsValid() {
			return false
		}
		if attr, ok := fom.LookupAttribute(inst.cls, "HLAprivilegeToDeleteObject"); ok {
			return r.opts.Ownership.IsOwnedBy(fed, h, inst.handle, attr)
		}
	}
	// Legacy embedded profiles without the standard privilege attribute retain
	// registration-based authority. Standard FOMs use the ownership lookup above.
	return inst.owner == h
}

// PrepareResignDeletions enumerates actual instances, not a handle-number range.
// It also removes the departing federate's discovery/scope state on commit.
func (r *RegistryOf[T]) PrepareResignDeletions(ctx context.Context, fed core.FederationName, h core.FederateHandle, deleteOwned bool) *ResignDeletionPlanOf[T] {
	st := r.stateFor(fed)
	st.mu.Lock()
	p := &ResignDeletionPlanOf[T]{r: r, st: st, fed: fed, h: h, active: true}
	p.ddmCommit = r.ddmResignCommit(fed, h)
	p.transportation = r.transportationOwnedAttributes(st, fed, h)
	for obj, d := range st.deletions {
		if d.Producer == h && !d.Committed {
			p.pending = append(p.pending, obj)
		}
	}
	if deleteOwned {
		for obj, inst := range st.instances {
			if r.hasDeletePrivilege(ctx, fed, h, inst) {
				p.objects = append(p.objects, obj)
			}
		}
		sort.Slice(p.objects, func(i, j int) bool { return p.objects[i] < p.objects[j] })
		seq := st.nextOutboundSeq
		for _, obj := range p.objects {
			var recipients []core.FederateHandle
			for k := range st.discovered {
				if k.obj == obj && k.sub != h {
					recipients = append(recipients, k.sub)
				}
			}
			sort.Slice(recipients, func(i, j int) bool { return recipients[i] < recipients[j] })
			for _, sub := range recipients {
				seq++
				p.deliveries = append(p.deliveries, core.OutboxDelivery{Recipient: sub, Event: &CallbackEventOf[T]{Sequence: seq, Kind: CallbackRemove, Object: obj, ObjectClass: st.knownClass(sub, st.instances[obj]), Metadata: metadataForOf[T](st.instances[obj].owner, nil, 0)}})
			}
		}
	}
	return p
}

func (p *ResignDeletionPlanOf[T]) Commit() {
	p.CommitState()
	p.Finish()
}

// CommitState applies object cleanup without acquiring the time-evaluation
// lease. A composed resign calls Finish after its message reservation ends.
func (p *ResignDeletionPlanOf[T]) CommitState() {
	p.once.Do(func() {
		p.pinMu.Lock()
		defer p.pinMu.Unlock()
		p.active = false
		defer p.st.mu.Unlock()
		if p.ddmCommit != nil {
			p.ddmCommit()
		}
		for _, obj := range p.pending {
			delete(p.st.deletions, obj)
		}
		// A later deletion retraction cannot reassume ownership for a federate
		// that is no longer a member of this execution.
		for obj, deletion := range p.st.deletions {
			deletion.Recipients = slices.DeleteFunc(deletion.Recipients, func(recipient DeletionRecipientState) bool { return recipient.Recipient == p.h })
			for i := range deletion.AttributeOwners {
				if deletion.AttributeOwners[i].Owner == p.h {
					deletion.AttributeOwners[i].Owner = core.InvalidFederateHandle
				}
			}
			p.st.deletions[obj] = deletion
		}
		deleted := make(map[core.ObjectHandle]bool, len(p.objects))
		for _, obj := range p.objects {
			delete(p.st.deletions, obj)
			deleted[obj] = true
			name := p.st.instances[obj].name
			delete(p.st.nameToHandle, name)
			p.r.reservations.ForgetRegistered(p.fed, name)
			delete(p.st.instances, obj)
			delete(p.st.attributeOrders, obj)
			delete(p.st.scope, obj)
			p.r.forgetObjectRegions(p.fed, obj)
		}
		for k := range p.st.discovered {
			if k.sub == p.h || deleted[k.obj] {
				p.st.forgetKnowledgeLocked(k)
			}
		}
		for k := range p.st.knowledgeEpochs {
			if k.sub == p.h || deleted[k.obj] {
				p.st.forgetKnowledgeLocked(k)
			}
		}
		pruneDeletionTombstonesLockedOf[T](p.st)
		delete(p.st.interactionOrders, p.h)
		for _, scope := range p.st.scope {
			delete(scope, p.h)
		}
		p.st.nextOutboundSeq += uint64(len(p.deliveries))
		p.retireResignedTransportation(deleted)
		p.committed.Store(true)
	})
}

// Finish discards superseded timed removals after all object, ownership,
// outbox, and time-evaluation leases have been released.
func (p *ResignDeletionPlanOf[T]) Finish() {
	if !p.committed.Load() {
		return
	}
	p.finish.Do(func() {
		if gate, ok := p.r.opts.TSOGate.(interface {
			DiscardObjectDeletions(core.FederationName, []core.ObjectHandle)
		}); ok {
			gate.DiscardObjectDeletions(p.fed, append(append([]core.ObjectHandle(nil), p.pending...), p.objects...))
		}
	})
}

func (p *ResignDeletionPlanOf[T]) Release() {
	p.once.Do(func() {
		p.pinMu.Lock()
		defer p.pinMu.Unlock()
		p.active = false
		p.st.mu.Unlock()
	})
}

func (p *ResignDeletionPlanOf[T]) retireResignedTransportation(deleted map[core.ObjectHandle]bool) {
	p.r.transports.mu.Lock()
	p.r.transports.retireTicketsLocked(func(c core.TransportationChange) bool {
		return c.Scope.Member.Federation == p.fed && (c.Scope.Member.Handle == p.h || deleted[c.Object])
	})
	for _, key := range p.transportation {
		delete(p.r.transports.attr, key)
	}
	for k := range p.r.transports.attr {
		if k.fed == p.fed && deleted[k.obj] {
			delete(p.r.transports.attr, k)
		}
	}
	for k := range p.r.transports.inter {
		if k.fed == p.fed && k.publisher == p.h {
			delete(p.r.transports.inter, k)
		}
	}
	p.r.transports.mu.Unlock()
}
