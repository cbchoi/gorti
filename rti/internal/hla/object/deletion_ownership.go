package object

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type preparedRemovalOf[T core.LogicalTimeScalar] struct {
	*preparedAttributeFanoutOf[T]
	ownership       core.OutboxReservation
	finishOwnership func(context.Context, error)
	objectPin       bool
}

func (p *preparedRemovalOf[T]) prepareOwnership(ctx context.Context, obj core.ObjectHandle, terminate bool) error {
	port, ok := p.registry.opts.Ownership.(interface {
		PrepareObjectDeletionAndRetentionProjected(context.Context, core.FederationName, []core.ObjectHandle, []core.ObjectHandle, func(core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (core.ObjectClassHandle, core.ObjectClassHandle, []core.AttributeHandle, error)) (core.OutboxReservation, []core.OutboxDelivery, func(context.Context, error), error)
	})
	if !ok {
		return nil
	}
	st := p.registry.stateFor(p.fed)
	st.mu.Lock()
	p.objectPin = true
	var deleting []core.ObjectHandle
	if terminate {
		deleting = []core.ObjectHandle{obj}
	}
	reservation, outcomes, finish, err := port.PrepareObjectDeletionAndRetentionProjected(ctx, p.fed, deleting, []core.ObjectHandle{obj}, func(recipient core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, core.ObjectClassHandle, []core.AttributeHandle, error) {
		inst := st.instances[obj]
		if inst == nil || st.discovered[discoverKey{sub: recipient, obj: obj}] == 0 {
			return 0, 0, nil, core.ErrObjectHandleInvalid
		}
		mapped, err := p.registry.actualAttributesAtKnownClass(ctx, p.fed, st, recipient, inst, attrs)
		return inst.cls, st.knownClass(recipient, inst), mapped, err
	})
	if err != nil {
		p.unlockObject()
		return err
	}
	p.ownership, p.finishOwnership = reservation, finish
	for i, outcome := range outcomes {
		if event, ok := outcome.Event.(interface{ ProjectedOwnershipCallback() core.OutboundEvent }); ok {
			outcomes[i].Event = event.ProjectedOwnershipCallback()
		}
	}
	p.immediate = append(outcomes, p.immediate...)
	return nil
}

func (p *preparedRemovalOf[T]) commit(ctx context.Context) error {
	if p.ownership != nil {
		if err := p.ownership.Commit(); err != nil {
			return err
		}
	}
	return p.preparedAttributeFanoutOf.commit(ctx)
}

func (p *preparedRemovalOf[T]) abort() {
	// Release ownership first: a time reservation can reevaluate and query it.
	if p.ownership != nil {
		p.ownership.Release()
	}
	p.unlockObject()
	p.preparedAttributeFanoutOf.abort()
}

func (p *preparedRemovalOf[T]) unlockObject() {
	if p.objectPin {
		p.objectPin = false
		p.registry.stateFor(p.fed).mu.Unlock()
	}
}

func (p *preparedRemovalOf[T]) finish(ctx context.Context, err error) {
	if p.finishOwnership != nil {
		p.finishOwnership(ctx, err)
	}
}
