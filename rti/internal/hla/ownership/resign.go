package ownership

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ResignPlan holds the ownership lock until Commit or Release. The caller
// must reserve Deliveries and journal the enclosing resignation first.
type ResignPlan struct {
	m                  *Manager
	fed                core.FederationName
	st                 *federationState
	h                  core.FederateHandle
	deleted            map[core.ObjectHandle]bool
	transfers          map[ownershipKey]core.FederateHandle
	deliveries         []core.OutboxDelivery
	terminalDeliveries []core.OutboxDelivery
	deletionDeliveries []core.OutboxDelivery
	unowned            map[core.ObjectHandle][]core.AttributeHandle
	changed            []core.ObjectHandle
	retained           map[*terminalCallback]*CallbackProjection
	once               sync.Once
	finish             sync.Once
	refresh            sync.Once
	committed          atomic.Bool
}

func (p *ResignPlan) Deliveries() []core.OutboxDelivery { return slices.Clone(p.deliveries) }

// DeletionDeliveries must precede the corresponding object removals in the
// enclosing atomic fanout. They are deliberately separate from surviving-object
// transfers, which can follow the removal messages without invalidating them.
func (p *ResignPlan) DeletionDeliveries() []core.OutboxDelivery {
	return slices.Clone(p.deletionDeliveries)
}

// PrepareCallbackProjections runs under the object and ownership preparation
// pins. Existing obligations are staged until commit, never changed on release.
func (p *ResignPlan) PrepareCallbackProjections(project func(core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (*CallbackProjection, error)) error {
	for _, delivery := range append(p.DeletionDeliveries(), p.Deliveries()...) {
		event := delivery.Event.(*CallbackEvent)
		projection, err := project(delivery.Recipient, event.Object, event.Attributes)
		if err != nil {
			return err
		}
		event.RetainedProjection = projection
	}
	objects := make([]core.ObjectHandle, 0, len(p.deleted))
	for obj := range p.deleted {
		objects = append(objects, obj)
	}
	return p.prepareRetainedProjections(objects, project)
}

func (p *ResignPlan) prepareRetainedProjections(objects []core.ObjectHandle, project func(core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (*CallbackProjection, error)) error {
	if p.st == nil {
		return nil
	}
	if p.retained == nil {
		p.retained = make(map[*terminalCallback]*CallbackProjection)
	}
	selected := make(map[core.ObjectHandle]bool, len(objects))
	for _, obj := range objects {
		selected[obj] = true
	}
	for recipient, session := range p.st.callbackSessions {
		if recipient == p.h {
			continue
		}
		for _, entry := range session.pending {
			if !selected[entry.event.Object] || entry.event.RetainedProjection != nil {
				continue
			}
			projection, err := project(recipient, entry.event.Object, entry.event.Attributes)
			if err != nil {
				return err
			}
			p.retained[entry] = projection
		}
	}
	return nil
}

// PrepareResign checks the selected action before changing any ownership.
// deleted must contain exactly the instances the object service will delete.
func (m *Manager) PrepareResign(fed core.FederationName, h core.FederateHandle, action core.ResignAction, deleted []core.ObjectHandle) (*ResignPlan, error) {
	cancel, divest := false, false
	switch action {
	case core.ResignActionUnconditionallyDivestAttributes, core.ResignActionDeleteThenDivest:
		divest = true
	case core.ResignActionCancelThenDelete:
		cancel, divest = true, true
	case core.ResignActionCancelPendingOwnership:
		cancel = true
	case core.ResignActionNoAction, core.ResignActionDeleteObjects:
	default:
		return nil, core.ErrResignActionUnsupported
	}
	m.mu.Lock()
	p := &ResignPlan{m: m, fed: fed, st: m.fed[fed], h: h, deleted: map[core.ObjectHandle]bool{}, transfers: map[ownershipKey]core.FederateHandle{}, unowned: map[core.ObjectHandle][]core.AttributeHandle{}}
	for _, obj := range deleted {
		p.deleted[obj] = true
	}
	if p.st == nil {
		return p, nil
	}
	if err := p.validateResignAcquisitions(cancel); err != nil {
		p.Release()
		return nil, err
	}
	p.prepareDeletionCallbacks()
	if err := p.prepareResignTransfers(divest); err != nil {
		p.Release()
		return nil, err
	}
	if err := m.validateTerminalCallbacksLocked(p.st, p.terminalDeliveries); err != nil {
		p.Release()
		return nil, err
	}
	return p, nil
}

func (p *ResignPlan) Commit() {
	p.once.Do(func() {
		defer func() { p.m.mu.Unlock(); p.committed.Store(true) }()
		if p.st == nil {
			return
		}
		for entry, projection := range p.retained {
			// Retry may already hold a detached event. Do not mutate that event.
			event := cloneCallback(entry.event)
			event.RetainedProjection = projection
			entry.event = event
		}
		// Capacity was validated while this plan held the ownership lock; no
		// other callback allocation can change it before the infallible commit.
		if err := p.m.registerTerminalCallbacksLocked(p.st, p.fed, p.terminalDeliveries); err != nil {
			panic(err)
		}
		delete(p.st.callbackSessions, p.h)
		for k := range p.st.owners {
			if p.deleted[k.obj] {
				delete(p.st.owners, k)
			}
		}
		for k, winner := range p.transfers {
			if winner == core.InvalidFederateHandle {
				delete(p.st.owners, k)
				p.st.unownedOffers[k] = 0
			} else {
				p.st.owners[k] = ownershipRecord{owner: winner}
				delete(p.st.unownedOffers, k)
				delete(p.st.pendingAcquires, acquireKey{obj: k.obj, attr: k.attr, acquirer: winner})
			}
		}
		p.removePendingResignState()
	})
}

func (p *ResignPlan) Release() { p.once.Do(func() { p.m.mu.Unlock() }) }

// Finish runs only after object, evaluator, and outbox reservations have been
// released. Queue admission completes legacy outcomes, never strict ones; a
// failed admission retains either profile for RetryOwnershipCallbacks.
func (p *ResignPlan) Finish(ctx context.Context, admissionError error) {
	p.FinishAdmission(admissionError)
	p.RefreshOwnership(ctx)
}

// FinishAdmission completes terminal delivery accounting without rediscovering
// a member whose declaration teardown has not yet finished.
func (p *ResignPlan) FinishAdmission(admissionError error) {
	if !p.committed.Load() {
		return
	}
	p.finish.Do(func() {
		p.m.finishTerminalAdmission(p.fed, p.terminalDeliveries, admissionError)
	})
}

// RefreshOwnership runs after the enclosing membership teardown, when discovery
// and offer resolvers can no longer select the departing federate.
func (p *ResignPlan) RefreshOwnership(ctx context.Context) {
	if !p.committed.Load() {
		return
	}
	p.refresh.Do(func() {
		for _, obj := range p.changed {
			p.m.notifyOwnershipChanged(ctx, p.fed, obj)
			// Membership teardown may follow this prepared commit. Do not offer
			// the just-divested attributes back to the departing member.
			if p.m.opts.Subscribers != nil {
				for _, attr := range p.unowned[obj] {
					candidates := slices.Clone(p.m.opts.Subscribers(ctx, p.fed, obj, []core.AttributeHandle{attr}))
					slices.Sort(candidates)
					for _, candidate := range slices.Compact(candidates) {
						if candidate != 0 && candidate != p.h && !p.m.HasAcquisitionObligation(p.fed, candidate, obj, []core.AttributeHandle{attr}) {
							p.m.fanoutAssumptionTo(ctx, p.fed, 0, obj, []core.AttributeHandle{attr}, nil, true, candidate)
						}
					}
				}
			}
		}
	})
}

// OnObjectDeleted removes ownership and pending transfers for a deleted object.
func (m *Manager) OnObjectDeleted(fed core.FederationName, obj core.ObjectHandle) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.fed[fed]
	if st == nil {
		return
	}
	for k := range st.owners {
		if k.obj == obj {
			delete(st.owners, k)
		}
	}
	for k := range st.pendingDivests {
		if k.obj == obj {
			delete(st.pendingDivests, k)
		}
	}
	for k := range st.pendingAcquires {
		if k.obj == obj {
			delete(st.pendingAcquires, k)
		}
	}
	for k := range st.unownedOffers {
		if k.obj == obj {
			delete(st.unownedOffers, k)
		}
	}
}
func (p *ResignPlan) validateResignAcquisitions(cancel bool) error {
	if !cancel {
		if session := p.st.callbackSessions[p.h]; session != nil && len(session.pending) != 0 {
			return core.ErrOwnershipAcquisitionPending
		}
		for k := range p.st.pendingAcquires {
			if k.acquirer == p.h {
				return core.ErrOwnershipAcquisitionPending
			}
		}
	}
	return nil
}

func (p *ResignPlan) prepareDeletionCallbacks() {
	var deletedRequests []acquireKey
	for key := range p.st.pendingAcquires {
		if p.deleted[key.obj] && key.acquirer != p.h {
			deletedRequests = append(deletedRequests, key)
		}
	}
	slices.SortFunc(deletedRequests, func(a, b acquireKey) int {
		if a.obj != b.obj {
			return compareUint64(uint64(a.obj), uint64(b.obj))
		}
		if a.acquirer != b.acquirer {
			return compareUint64(uint64(a.acquirer), uint64(b.acquirer))
		}
		return compareUint64(uint64(a.attr), uint64(b.attr))
	})
	for _, key := range deletedRequests {
		delivery := core.OutboxDelivery{Recipient: key.acquirer, Event: unavailableEvent(key.obj, []core.AttributeHandle{key.attr})}
		p.deletionDeliveries = append(p.deletionDeliveries, delivery)
		p.terminalDeliveries = append(p.terminalDeliveries, delivery)
	}
}

func (p *ResignPlan) prepareResignTransfers(divest bool) error {
	var owned []ownershipKey
	for k, rec := range p.st.owners {
		if rec.owner != p.h || p.deleted[k.obj] {
			continue
		}
		if !divest {
			return core.ErrFederateOwnsAttributes
		}
		owned = append(owned, k)
	}
	sort.Slice(owned, func(i, j int) bool {
		if owned[i].obj != owned[j].obj {
			return owned[i].obj < owned[j].obj
		}
		return owned[i].attr < owned[j].attr
	})
	for _, k := range owned {
		if err := p.prepareResignTransfer(k); err != nil {
			return err
		}
	}
	return nil
}

func (p *ResignPlan) prepareResignTransfer(k ownershipKey) error {
	if !slices.Contains(p.changed, k.obj) {
		p.changed = append(p.changed, k.obj)
	}
	var candidates []core.FederateHandle
	for a := range p.st.pendingAcquires {
		if a.obj == k.obj && a.attr == k.attr && a.acquirer != p.h {
			candidates = append(candidates, a.acquirer)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	winner := p.m.opts.Strategy.SelectAcquirer(SelectAcquirerContext{Phase: PhaseResign, Federation: p.fed, Object: k.obj, Attribute: k.attr, Owner: p.h, Candidates: append([]core.FederateHandle(nil), candidates...)})
	if winner != core.InvalidFederateHandle {
		if !slices.Contains(candidates, winner) {
			return fmt.Errorf("ownership: resign strategy selected noncandidate %d", winner)
		}
		delivery := core.OutboxDelivery{Recipient: winner, Event: acquireNotificationEvent(k.obj, []core.AttributeHandle{k.attr}, winner)}
		p.deliveries = append(p.deliveries, delivery)
		p.terminalDeliveries = append(p.terminalDeliveries, delivery)
		for _, candidate := range candidates {
			if candidate != winner {
				request := p.st.pendingAcquires[acquireKey{obj: k.obj, attr: k.attr, acquirer: candidate}]
				p.deliveries = append(p.deliveries, core.OutboxDelivery{Recipient: winner, Event: releaseRequestEvent(k.obj, []core.AttributeHandle{k.attr}, request.tag)})
			}
		}
	} else {
		p.unowned[k.obj] = append(p.unowned[k.obj], k.attr)
	}
	p.transfers[k] = winner
	return nil
}

func (p *ResignPlan) removePendingResignState() {
	for k, v := range p.st.pendingDivests {
		if p.deleted[k.obj] || v.owner == p.h {
			delete(p.st.pendingDivests, k)
		}
	}
	for k := range p.st.pendingAcquires {
		if p.deleted[k.obj] || k.acquirer == p.h {
			delete(p.st.pendingAcquires, k)
		}
	}
	for k, divester := range p.st.unownedOffers {
		if p.deleted[k.obj] {
			delete(p.st.unownedOffers, k)
		} else if divester == p.h {
			// Departed handles cannot acquire and are not restore participants.
			p.st.unownedOffers[k] = 0
		}
	}
}
