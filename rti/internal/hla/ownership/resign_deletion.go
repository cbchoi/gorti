package ownership

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// PrepareObjectDeletion resolves acquisition requests before the caller retires
// object knowledge. Reserve the returned outcomes before the removal callbacks
// in one fanout, and call finish only after all object/time/outbox leases end.
// The returned preparation holds ownership state; no callback is sent here.
func (m *Manager) PrepareObjectDeletion(ctx context.Context, fed core.FederationName, obj core.ObjectHandle) (core.OutboxReservation, []core.OutboxDelivery, func(context.Context, error), error) {
	return m.PrepareObjectDeletionProjected(ctx, fed, obj, nil)
}

func (m *Manager) PrepareObjectDeletionProjected(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, project func(core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (core.ObjectClassHandle, core.ObjectClassHandle, []core.AttributeHandle, error)) (core.OutboxReservation, []core.OutboxDelivery, func(context.Context, error), error) {
	return m.PrepareObjectDeletionsProjected(ctx, fed, []core.ObjectHandle{obj}, project)
}

func (m *Manager) PrepareObjectDeletionsProjected(ctx context.Context, fed core.FederationName, objects []core.ObjectHandle, project func(core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (core.ObjectClassHandle, core.ObjectClassHandle, []core.AttributeHandle, error)) (core.OutboxReservation, []core.OutboxDelivery, func(context.Context, error), error) {
	return m.PrepareObjectDeletionAndRetentionProjected(ctx, fed, objects, objects, project)
}

func (m *Manager) PrepareObjectDeletionAndRetentionProjected(ctx context.Context, fed core.FederationName, objects, retained []core.ObjectHandle, project func(core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (core.ObjectClassHandle, core.ObjectClassHandle, []core.AttributeHandle, error)) (core.OutboxReservation, []core.OutboxDelivery, func(context.Context, error), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	if fed == "" || len(objects)+len(retained) == 0 {
		return nil, nil, nil, core.ErrObjectHandleInvalid
	}
	for _, obj := range objects {
		if obj == 0 {
			return nil, nil, nil, core.ErrObjectHandleInvalid
		}
	}
	for _, obj := range retained {
		if obj == 0 {
			return nil, nil, nil, core.ErrObjectHandleInvalid
		}
	}
	// No member departs. The reserved invalid handle selects only deletion
	// cleanup and never supplies a candidate owner or callback recipient.
	plan, err := m.PrepareResign(fed, core.InvalidFederateHandle, core.ResignActionNoAction, objects)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		plan.Release()
		return nil, nil, nil, err
	}
	if project != nil {
		projection := func(recipient core.FederateHandle, object core.ObjectHandle, attrs []core.AttributeHandle) (*CallbackProjection, error) {
			registered, known, mapped, err := project(recipient, object, attrs)
			if err != nil {
				return nil, err
			}
			return &CallbackProjection{RegisteredClass: registered, KnownClass: known, Attributes: mapped}, nil
		}
		if err := plan.PrepareCallbackProjections(projection); err != nil {
			plan.Release()
			return nil, nil, nil, err
		}
		if err := plan.prepareRetainedProjections(retained, projection); err != nil {
			plan.Release()
			return nil, nil, nil, err
		}
	}
	return &objectDeletionReservation{plan: plan}, plan.DeletionDeliveries(), plan.Finish, nil
}

type objectDeletionReservation struct {
	plan *ResignPlan
}

// PreparedAttributeOwners is valid only while the reservation pins ownership.
func (p *objectDeletionReservation) PreparedAttributeOwners(obj core.ObjectHandle, attrs []core.AttributeHandle) map[core.AttributeHandle]core.FederateHandle {
	owners := make(map[core.AttributeHandle]core.FederateHandle, len(attrs))
	if p.plan.st == nil {
		return owners
	}
	for _, attr := range attrs {
		owners[attr] = p.plan.st.owners[ownershipKey{obj: obj, attr: attr}].owner
	}
	return owners
}

func (p *objectDeletionReservation) Commit() error {
	p.plan.Commit()
	return nil
}

func (p *objectDeletionReservation) Release() { p.plan.Release() }
