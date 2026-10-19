package ownership

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var _ core.OwnershipUnpublicationGuard = (*Manager)(nil)
var _ core.OwnershipUnpublicationCoordinator = (*Manager)(nil)

// The caller holds object knowledge/registration read pins, then this lease,
// then the declaration commit lock. Attributes are canonical registered-class
// coordinates; an empty selected attribute set does not select every attribute.
func (m *Manager) PrepareUnpublication(ctx context.Context, fed core.FederationName, publisher core.FederateHandle, objects map[core.ObjectHandle][]core.AttributeHandle) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for obj, attrs := range objects {
		if obj == 0 {
			return nil, core.ErrObjectHandleInvalid
		}
		for _, attr := range attrs {
			if attr == 0 {
				return nil, core.ErrAttributeNotFound
			}
		}
	}
	m.mu.RLock()
	var once sync.Once
	release := func() { once.Do(m.mu.RUnlock) }
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	if st := m.fed[fed]; st != nil {
		for obj, attrs := range objects {
			if st.unpublicationBlockedLocked(publisher, obj, attrs) {
				release()
				return nil, core.ErrOwnershipAcquisitionPending
			}
		}
	}
	return release, nil
}

func (st *federationState) unpublicationBlockedLocked(publisher core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) bool {
	for _, attr := range attrs {
		owner, exists := st.owners[ownershipKey{obj: obj, attr: attr}]
		if exists && owner.owner != 0 && owner.owner != publisher && owner.owner != ^core.FederateHandle(0) && st.hasAcquisitionObligationLocked(publisher, obj, []core.AttributeHandle{attr}) {
			return true
		}
	}
	return false
}

type unpublicationTransfer struct {
	key    ownershipKey
	winner core.FederateHandle
}

func (p *unpublicationPlan) PrepareCallbackProjections(project func(core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (core.ObjectClassHandle, core.ObjectClassHandle, []core.AttributeHandle, error)) error {
	if project == nil {
		return fmt.Errorf("ownership: unpublication callback projection unavailable")
	}
	projections := make(map[*CallbackEvent]*CallbackProjection, len(p.deliveries))
	for _, delivery := range p.deliveries {
		event, ok := delivery.Event.(*CallbackEvent)
		if !ok {
			continue
		}
		registered, known, attrs, err := project(delivery.Recipient, event.Object, slices.Clone(event.Attributes))
		if err != nil {
			return err
		}
		if registered == 0 || known == 0 || len(attrs) != len(event.Attributes) {
			return fmt.Errorf("ownership: invalid unpublication callback projection")
		}
		projections[event] = &CallbackProjection{RegisteredClass: registered, KnownClass: known, Attributes: slices.Clone(attrs)}
	}
	for event, projection := range projections {
		event.RetainedProjection = projection
	}
	return nil
}

type unpublicationPlan struct {
	m           *Manager
	st          *federationState
	fed         core.FederationName
	publisher   core.FederateHandle
	transfers   []unpublicationTransfer
	terminals   []core.OutboxDelivery
	deliveries  []core.OutboxDelivery
	unowned     map[core.ObjectHandle][]core.AttributeHandle
	changed     []core.ObjectHandle
	finish      sync.Once
	dispatch    sync.Once
	committed   atomic.Bool
	dispatchErr error
}

func (m *Manager) PrepareUnpublicationCommit(ctx context.Context, fed core.FederationName, publisher core.FederateHandle, objects map[core.ObjectHandle][]core.AttributeHandle) (core.PreparedOwnershipUnpublication, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var keys []ownershipKey
	for obj, attrs := range objects {
		if obj == 0 {
			return nil, core.ErrObjectHandleInvalid
		}
		for _, attr := range attrs {
			if attr == 0 {
				return nil, core.ErrAttributeNotFound
			}
			keys = append(keys, ownershipKey{obj: obj, attr: attr})
		}
	}
	slices.SortFunc(keys, func(a, b ownershipKey) int { return compareObjectAttribute(a.obj, a.attr, b.obj, b.attr) })
	keys = slices.Compact(keys)
	m.mu.Lock()
	p := &unpublicationPlan{m: m, st: m.fed[fed], fed: fed, publisher: publisher, unowned: map[core.ObjectHandle][]core.AttributeHandle{}}
	fail := func(err error) (core.PreparedOwnershipUnpublication, error) { p.Release(); return nil, err }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if p.st == nil {
		return p, nil
	}
	for _, k := range keys {
		if p.st.unpublicationBlockedLocked(publisher, k.obj, []core.AttributeHandle{k.attr}) {
			return fail(core.ErrOwnershipAcquisitionPending)
		}
	}
	if err := p.prepareTransfers(keys); err != nil {
		return fail(err)
	}
	if err := m.validateTerminalCallbacksLocked(p.st, p.terminals); err != nil {
		return fail(err)
	}
	return p, nil
}

func (p *unpublicationPlan) Commit() {
	p.finish.Do(func() {
		defer func() { p.m.mu.Unlock(); p.committed.Store(true) }()
		if p.st != nil {
			// All callback capacity and winners were validated under this lock.
			if err := p.m.registerTerminalCallbacksLocked(p.st, p.fed, p.terminals); err != nil {
				panic(err)
			}
			for _, transfer := range p.transfers {
				k := transfer.key
				delete(p.st.pendingDivests, k)
				if transfer.winner == 0 {
					delete(p.st.owners, k)
					p.st.unownedOffers[k] = p.publisher
				} else {
					p.st.owners[k] = ownershipRecord{owner: transfer.winner}
					delete(p.st.unownedOffers, k)
					delete(p.st.pendingAcquires, acquireKey{obj: k.obj, attr: k.attr, acquirer: transfer.winner})
				}
			}
		}
	})
}

func (p *unpublicationPlan) Release() { p.finish.Do(p.m.mu.Unlock) }

// Called after declaration/object admission has been released. Terminal results
// remain in the ledger even when the atomic fanout cannot be admitted/sent.
func (p *unpublicationPlan) Dispatch(ctx context.Context) error {
	if !p.committed.Load() {
		return core.ErrOwnershipNotInTransfer
	}
	p.dispatch.Do(func() {
		if len(p.deliveries) != 0 {
			if reservable, ok := p.m.opts.Outbox.(core.ReservableOutbox); ok {
				reservation, err := reservable.Reserve(ctx, p.fed, p.deliveries)
				if err == nil && reservation == nil {
					err = fmt.Errorf("ownership: nil unpublication callback reservation")
				}
				if err == nil {
					err = reservation.Commit()
					reservation.Release()
				}
				p.m.finishTerminalAdmission(p.fed, p.terminals, err)
				p.dispatchErr = err
			} else {
				var failures []error
				for _, d := range p.deliveries {
					e := d.Event.(*CallbackEvent)
					var err error
					if e.Kind == CallbackAcquisitionNotification {
						err = p.m.sendTerminalCallback(ctx, p.fed, d.Recipient, e)
					} else {
						err = p.m.opts.Outbox.Send(ctx, p.fed, d.Recipient, e)
					}
					if err != nil {
						failures = append(failures, err)
					}
				}
				p.dispatchErr = errors.Join(failures...)
			}
		}
		for _, obj := range p.changed {
			p.m.notifyOwnershipChanged(ctx, p.fed, obj)
			p.m.fanoutAssumption(ctx, p.fed, p.publisher, obj, p.unowned[obj], nil, true)
		}
	})
	return p.dispatchErr
}
func (p *unpublicationPlan) prepareTransfers(keys []ownershipKey) error {
	for _, k := range keys {
		owner, exists := p.st.owners[k]
		if !exists || owner.owner != p.publisher {
			continue
		}
		candidates := p.st.queuedAcquirersLocked(k.obj, k.attr)
		winner := p.m.opts.Strategy.SelectAcquirer(SelectAcquirerContext{Phase: PhaseUnconditionalDivest, Federation: p.fed, Object: k.obj, Attribute: k.attr, Owner: p.publisher, Candidates: slices.Clone(candidates)})
		if winner != 0 && !slices.Contains(candidates, winner) {
			return fmt.Errorf("ownership: unpublication strategy selected noncandidate %d", winner)
		}
		p.transfers = append(p.transfers, unpublicationTransfer{key: k, winner: winner})
		if !slices.Contains(p.changed, k.obj) {
			p.changed = append(p.changed, k.obj)
		}
		if winner == 0 {
			p.unowned[k.obj] = append(p.unowned[k.obj], k.attr)
			continue
		}
		request := p.st.pendingAcquires[acquireKey{obj: k.obj, attr: k.attr, acquirer: winner}]
		terminal := core.OutboxDelivery{Recipient: winner, Event: acquireNotificationEvent(k.obj, []core.AttributeHandle{k.attr}, winner, request.tag)}
		p.terminals = append(p.terminals, terminal)
		p.deliveries = append(p.deliveries, terminal)
		for _, candidate := range candidates {
			if candidate != winner {
				pa := p.st.pendingAcquires[acquireKey{obj: k.obj, attr: k.attr, acquirer: candidate}]
				p.deliveries = append(p.deliveries, core.OutboxDelivery{Recipient: winner, Event: releaseRequestEvent(k.obj, []core.AttributeHandle{k.attr}, pa.tag)})
			}
		}
	}
	return nil
}
