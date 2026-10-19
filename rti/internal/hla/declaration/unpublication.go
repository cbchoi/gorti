package declaration

import (
	"context"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Compose before serving requests; both ports belong to the same federation
// state. The lock order is object, ownership, declaration.
func (m *Manager) SetObjectUnpublication(objects core.ObjectClassUnpublicationPreparer, ownership core.OwnershipUnpublicationCoordinator) {
	m.unpublicationObjects, m.unpublicationOwnership = objects, ownership
}

func (m *Manager) UnpublishObjectClass(ctx context.Context, fed core.FederationName, pub core.FederateHandle, cls core.ObjectClassHandle) error {
	return m.unpublishObject(ctx, fed, pub, cls, nil, true)
}

func (m *Manager) UnpublishObjectClassAttributeSet(ctx context.Context, fed core.FederationName, pub core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) error {
	return m.unpublishObject(ctx, fed, pub, cls, attrs, false)
}

func publishedAttributes(st *federationState, pub core.FederateHandle, cls core.ObjectClassHandle) []core.AttributeHandle {
	var attrs []core.AttributeHandle
	if st != nil {
		for key, publishers := range st.objPubs {
			if key.cls == cls {
				if _, ok := publishers[pub]; ok {
					attrs = append(attrs, key.attr)
				}
			}
		}
	}
	slices.Sort(attrs)
	return attrs
}

func (m *Manager) unpublishObject(ctx context.Context, fed core.FederationName, pub core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, whole bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	model, err := m.model(ctx, fed)
	if err != nil {
		return err
	}
	candidates, err := m.unpublicationCandidates(fed, pub, cls, attrs, whole, model)
	if err != nil {
		return err
	}
	// Even the empty-set no-op has validated the FOM class first.
	if !whole && len(candidates) == 0 {
		return nil
	}
	privilege := deletePrivilege(model, cls)
	projectionAttrs := slices.Clone(candidates)
	if privilege != 0 && !slices.Contains(projectionAttrs, privilege) {
		projectionAttrs = append(projectionAttrs, privilege)
		slices.Sort(projectionAttrs)
	}
	objectPlan, err := m.prepareObjectUnpublication(ctx, fed, pub, cls, projectionAttrs)
	if err != nil {
		return err
	}
	return m.unpublishPrepared(ctx, fed, pub, cls, candidates, whole, model, privilege, objectPlan)
}

func (m *Manager) unpublishPrepared(ctx context.Context, fed core.FederationName, pub core.FederateHandle, cls core.ObjectClassHandle, candidates []core.AttributeHandle, whole bool, model core.FOMHandle, privilege core.AttributeHandle, objectPlan core.PreparedObjectClassUnpublication) error {
	var err error
	defer func() {
		if objectPlan != nil {
			objectPlan.Release()
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		m.mu.RLock()
		expectedState := m.fed[fed]
		expected := publishedAttributes(expectedState, pub, cls)
		historyKey := objectPublicationKey{cls, pub}
		touched := false
		if expectedState != nil {
			touched = expectedState.objPubHistory[historyKey]
		}
		m.mu.RUnlock()
		if whole {
			var retry bool
			candidates, objectPlan, retry, err = m.expandUnpublication(ctx, fed, pub, cls, candidates, expected, objectPlan)
			if err != nil {
				return err
			}
			if retry {
				continue
			}
		}
		selected := selectUnpublicationAttributes(expected, candidates, privilege, touched)
		ownershipPlan, err := m.prepareOwnershipUnpublication(ctx, fed, pub, selected, objectPlan)
		if err != nil {
			return err
		}
		m.mu.Lock()
		previous := m.fed[fed]
		if previous != expectedState || !slices.Equal(expected, publishedAttributes(previous, pub, cls)) {
			m.mu.Unlock()
			if ownershipPlan != nil {
				ownershipPlan.Release()
			}
			continue
		}
		err = m.commitUnpublicationLocked(ctx, fed, pub, cls, previous, model, candidates, selected, privilege, ownershipPlan)
		m.mu.Unlock()
		if err != nil {
			if ownershipPlan != nil {
				ownershipPlan.Release()
			}
			return err
		}
		return dispatchUnpublication(ctx, objectPlan, ownershipPlan)
	}
}

func dispatchUnpublication(ctx context.Context, objectPlan core.PreparedObjectClassUnpublication, ownershipPlan core.PreparedOwnershipUnpublication) error {
	if objectPlan != nil {
		objectPlan.Release()
	}
	if ownershipPlan != nil {
		return ownershipPlan.Dispatch(ctx)
	}
	return nil
}
func (m *Manager) unpublicationCandidates(fed core.FederationName, pub core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, whole bool, model core.FOMHandle) ([]core.AttributeHandle, error) {
	candidates := slices.Clone(attrs)
	if names, ok := model.(core.FOMHandleNameLookup); ok {
		if _, exists := names.ObjectClassName(cls); !exists {
			return nil, core.ErrObjectClassNotFound
		}
		for _, attr := range attrs {
			if _, exists := names.AttributeName(cls, attr); !exists {
				return nil, core.ErrAttributeNotFound
			}
		}
	}
	if whole {
		if available, ok := model.(interface {
			AttributeHandles(core.ObjectClassHandle) []core.AttributeHandle
		}); ok {
			candidates = slices.Clone(available.AttributeHandles(cls))
		} else {
			candidates = m.PublishedObjectAttributes(fed, pub, cls)
		}
	}
	slices.Sort(candidates)
	candidates = slices.Compact(candidates)
	return candidates, nil
}

func (m *Manager) prepareObjectUnpublication(ctx context.Context, fed core.FederationName, pub core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) (core.PreparedObjectClassUnpublication, error) {
	if m.unpublicationObjects == nil && m.unpublicationOwnership == nil {
		return nil, nil
	}
	if m.unpublicationObjects == nil || m.unpublicationOwnership == nil {
		return nil, fmt.Errorf("unpublication requires both object and ownership ports")
	}
	plan, err := m.unpublicationObjects.PrepareObjectClassUnpublication(ctx, fed, pub, cls, attrs)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, fmt.Errorf("unpublication object preparation returned nil")
	}
	return plan, nil
}

func (m *Manager) expandUnpublication(ctx context.Context, fed core.FederationName, pub core.FederateHandle, cls core.ObjectClassHandle, candidates, expected []core.AttributeHandle, objectPlan core.PreparedObjectClassUnpublication) ([]core.AttributeHandle, core.PreparedObjectClassUnpublication, bool, error) {
	var added []core.AttributeHandle
	for _, attr := range expected {
		if !slices.Contains(candidates, attr) {
			added = append(added, attr)
		}
	}
	if len(added) != 0 {
		candidates = append(candidates, added...)
		slices.Sort(candidates)
		candidates = slices.Compact(candidates)
		if objectPlan != nil {
			// Publication may grow while the object pin is held. Rebuild
			// projection as well as selection before preparing ownership.
			objectPlan.Release()
			var err error
			objectPlan, err = m.unpublicationObjects.PrepareObjectClassUnpublication(ctx, fed, pub, cls, candidates)
			if err != nil {
				return candidates, objectPlan, false, err
			}
			if objectPlan == nil {
				return candidates, nil, false, fmt.Errorf("unpublication object preparation returned nil")
			}
			return candidates, objectPlan, true, nil
		}
	}
	return candidates, objectPlan, false, nil
}

func selectUnpublicationAttributes(expected, candidates []core.AttributeHandle, privilege core.AttributeHandle, touched bool) []core.AttributeHandle {
	selected := make([]core.AttributeHandle, 0, len(expected))
	for _, attr := range expected {
		if slices.Contains(candidates, attr) {
			selected = append(selected, attr)
		}
	}
	// Removing the last explicit attribute also removes implicit privilege.
	// Include it in the prepared ownership transaction, not just declaration.
	if privilege != 0 && !touched && slices.Contains(expected, privilege) && !slices.Contains(selected, privilege) && len(selected) == len(expected)-1 {
		selected = append(selected, privilege)
		slices.Sort(selected)
	}
	return selected
}

func (m *Manager) prepareOwnershipUnpublication(ctx context.Context, fed core.FederationName, pub core.FederateHandle, selected []core.AttributeHandle, objectPlan core.PreparedObjectClassUnpublication) (core.PreparedOwnershipUnpublication, error) {
	var err error
	var ownershipPlan core.PreparedOwnershipUnpublication
	if objectPlan != nil {
		ownershipPlan, err = m.unpublicationOwnership.PrepareUnpublicationCommit(ctx, fed, pub, objectPlan.SelectAttributes(selected))
		if err != nil {
			return nil, err
		}
		if ownershipPlan == nil {
			return nil, fmt.Errorf("unpublication ownership preparation returned nil")
		}
		if projection, ok := ownershipPlan.(core.PreparedOwnershipUnpublicationProjection); ok {
			if err := projection.PrepareCallbackProjections(func(recipient core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, core.ObjectClassHandle, []core.AttributeHandle, error) {
				return objectPlan.ProjectKnownObjectAttributes(ctx, fed, recipient, obj, attrs)
			}); err != nil {
				ownershipPlan.Release()
				return nil, err
			}
		}
	}
	return ownershipPlan, nil
}

func (m *Manager) commitUnpublicationLocked(ctx context.Context, fed core.FederationName, pub core.FederateHandle, cls core.ObjectClassHandle, previous *federationState, model core.FOMHandle, candidates, selected []core.AttributeHandle, privilege core.AttributeHandle, ownershipPlan core.PreparedOwnershipUnpublication) error {
	var err error
	historyKey := objectPublicationKey{cls, pub}
	st := cloneFederationState(previous)
	if previous == nil {
		st.defaults = defaultSwitches(model)
	}
	before := desiredAdvice(st, model)
	for _, attr := range selected {
		key := objAttrKey{cls, attr}
		delete(st.objPubs[key], pub)
		if len(st.objPubs[key]) == 0 {
			delete(st.objPubs, key)
		}
	}
	if len(publishedAttributes(st, pub, cls)) == 0 {
		delete(st.objPubHistory, historyKey)
	} else if privilege != 0 && slices.Contains(candidates, privilege) {
		st.objPubHistory[historyKey] = true
	}
	after := desiredAdvice(st, model)
	for key, value := range after {
		if before[key] != value && switchEnabled(st, key.Recipient, key.Kind) {
			st.pendingAdvice[key] = struct{}{}
		}
	}
	if m.advisoryOutbox == nil {
		m.fed[fed] = st
		err = nil
	} else {
		err = m.commitDeclarationChange(ctx, fed, previous, st, pendingDeliveries(st, after), false)
	}
	if err == nil {
		clear(st.pendingAdvice)
		if ownershipPlan != nil {
			ownershipPlan.Commit()
		}
	}
	return err
}
