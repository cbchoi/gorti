package lifecycle

import (
	"context"
	"fmt"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

// PrepareResign composes object and ownership effects under the federation's
// exclusive operation gate. One reservation covers every resulting callback.
func PrepareResign(ctx context.Context, fed core.FederationName, h core.FederateHandle, action core.ResignAction, objects *object.Registry, owners *ownership.Manager, outbox core.ReservableOutbox, gates ...core.PreparingMessageDeliveryGate) (federation.ResignPreparation, error) {
	return PrepareResignOf(ctx, fed, h, action, objects, owners, outbox, gates...)
}

func PrepareResignOf[T core.LogicalTimeScalar](ctx context.Context, fed core.FederationName, h core.FederateHandle, action core.ResignAction, objects *object.RegistryOf[T], owners *ownership.Manager, outbox core.ReservableOutbox, gates ...core.PreparingMessageDeliveryGateOf[T]) (federation.ResignPreparation, error) {
	p := &resignPreparationOf[T]{ctx: ctx}
	prepare := func() ([]core.MessageDeliveryOf[T], error) {
		return p.prepareEffects(fed, h, action, objects, owners)
	}
	var deliveries []core.OutboxDelivery
	if len(gates) > 0 && gates[0] != nil {
		// Acquire the evaluator before object and ownership plans lock state.
		var err error
		p.messages, err = gates[0].ReserveMessagePreparation(fed, prepare)
		if err != nil {
			p.Release()
			return nil, err
		}
		if p.messages == nil {
			p.Release()
			return nil, fmt.Errorf("resign: nil message reservation")
		}
		for _, message := range p.messages.Immediate() {
			deliveries = append(deliveries, core.OutboxDelivery{Recipient: message.Recipient, Event: message.Event})
		}
	} else {
		if _, err := prepare(); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, p.objects.Deliveries()...)
	}
	deliveries = append(p.owners.DeletionDeliveries(), deliveries...)
	deliveries = append(deliveries, p.owners.Deliveries()...)
	for i, delivery := range deliveries {
		if event, ok := delivery.Event.(*ownership.CallbackEvent); ok {
			deliveries[i].Event = event.ProjectedOwnershipCallback()
		}
	}
	reservation, err := outbox.Reserve(ctx, fed, deliveries)
	if err != nil {
		p.Release()
		return nil, err
	}
	if reservation == nil {
		p.Release()
		return nil, fmt.Errorf("resign: nil outbox reservation")
	}
	p.reservation = reservation
	return p, nil
}

type resignPreparationOf[T core.LogicalTimeScalar] struct {
	ctx         context.Context
	objects     *object.ResignDeletionPlanOf[T]
	owners      *ownership.ResignPlan
	reservation core.OutboxReservation
	messages    core.TSOBufferReservationOf[T]
	once        sync.Once
	err         error
}

func (p *resignPreparationOf[T]) prepareEffects(fed core.FederationName, h core.FederateHandle, action core.ResignAction, objects *object.RegistryOf[T], owners *ownership.Manager) ([]core.MessageDeliveryOf[T], error) {
	ready := false
	defer func() {
		if !ready {
			p.Release()
		}
	}()
	deleteObjects := action == core.ResignActionDeleteObjects || action == core.ResignActionDeleteThenDivest || action == core.ResignActionCancelThenDelete
	p.objects = objects.PrepareResignDeletions(p.ctx, fed, h, deleteObjects)
	var err error
	p.owners, err = owners.PrepareResign(fed, h, action, p.objects.Objects())
	if err != nil {
		return nil, err
	}
	if err := p.owners.PrepareCallbackProjections(func(recipient core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (*ownership.CallbackProjection, error) {
		knownClass, mapped, err := p.objects.ProjectKnownObjectAttributes(p.ctx, fed, recipient, obj, attrs)
		if err != nil {
			return nil, err
		}
		return &ownership.CallbackProjection{RegisteredClass: p.objects.RegisteredClass(obj), KnownClass: knownClass, Attributes: mapped}, nil
	}); err != nil {
		return nil, err
	}
	var messages []core.MessageDeliveryOf[T]
	for _, delivery := range p.objects.Deliveries() {
		messages = append(messages, core.MessageDeliveryOf[T]{Recipient: delivery.Recipient, Sender: h, Event: delivery.Event, ReceiveOrder: true})
	}
	ready = true
	return messages, nil
}

func (p *resignPreparationOf[T]) Commit() error {
	p.once.Do(func() {
		p.owners.Commit()
		p.objects.CommitState()
		p.err = p.reservation.Commit()
		p.reservation.Release()
		if p.messages != nil {
			if p.err == nil {
				p.messages.Commit(p.ctx)
			} else {
				p.messages.Release()
			}
		}
		p.objects.Finish()
		p.owners.FinishAdmission(p.err)
	})
	return p.err
}

func (p *resignPreparationOf[T]) AfterResign() {
	p.owners.RefreshOwnership(p.ctx)
}

func (p *resignPreparationOf[T]) Release() {
	p.once.Do(func() {
		if p.reservation != nil {
			p.reservation.Release()
		}
		if p.owners != nil {
			p.owners.Release()
		}
		if p.objects != nil {
			p.objects.Release()
		}
		if p.messages != nil {
			p.messages.Release()
		}
	})
}
