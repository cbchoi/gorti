package declaration

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var _ core.DeclarationObjectUnsubscription = (*Manager)(nil)

func (m *Manager) UnsubscribeObjectClass(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle) error {
	return m.unsubscribeObject(ctx, fed, sub, cls, nil, true)
}

func (m *Manager) UnsubscribeObjectClassAttributeSet(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) error {
	return m.unsubscribeObject(ctx, fed, sub, cls, attrs, false)
}

func (m *Manager) unsubscribeObject(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, whole bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	model, err := m.model(ctx, fed)
	if err != nil {
		return err
	}
	selected := slices.Clone(attrs)
	if err := validateObjectAttributes(model, cls, selected); err != nil {
		return err
	}
	// An explicit empty set still validates the FDD class, but must not commit
	// a declaration transaction or retry unrelated pending advisory deliveries.
	if !whole && len(selected) == 0 {
		return nil
	}
	slices.Sort(selected)
	selected = slices.Compact(selected)
	return m.change(ctx, fed, func(st *federationState) {
		remove := selected
		if whole {
			remove = nil
			for key, subscribers := range st.objSubs {
				if key.cls == cls {
					if _, exists := subscribers[sub]; exists {
						remove = append(remove, key.attr)
					}
				}
			}
		}
		for _, attr := range remove {
			key := objAttrKey{cls: cls, attr: attr}
			if _, exists := st.objSubs[key][sub]; !exists {
				continue
			}
			delete(st.objSubs[key], sub)
			if len(st.objSubs[key]) == 0 {
				delete(st.objSubs, key)
			}
			delete(st.objSubSnapshots, key)
			delete(st.objOptions[key], sub)
			if len(st.objOptions[key]) == 0 {
				delete(st.objOptions, key)
			}
		}
	})
}

func validateObjectAttributes(model core.FOMHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) error {
	if names, ok := model.(core.FOMHandleNameLookup); ok {
		if _, exists := names.ObjectClassName(cls); !exists {
			return core.ErrObjectClassNotFound
		}
		for _, attr := range attrs {
			if _, exists := names.AttributeName(cls, attr); !exists {
				return core.ErrAttributeNotFound
			}
		}
	}
	return nil
}
