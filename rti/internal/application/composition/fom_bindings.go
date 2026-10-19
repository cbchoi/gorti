package composition

import (
	"fmt"
	"reflect"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// Each slice is indexed by its assigned handle minus one, never by model sort
// position. Effective member tables belong to each class independently.
type fomBindings struct {
	objects      []model.ObjectClass
	interactions []model.InteractionClass
	dimensions   []model.Dimension
	attributes   [][]model.Attribute
	parameters   [][]model.Parameter

	objectNames          classNameIndex
	interactionNames     classNameIndex
	objectAncestors      [][]core.ObjectClassHandle
	interactionAncestors [][]core.InteractionClassHandle
	advisoryDefaults     [5]advisoryDefault
}

func initialBindings(fom *model.FOM) *fomBindings {
	b := &fomBindings{}
	if fom == nil {
		return b
	}
	b.objects = fom.ObjectClasses()
	b.interactions = fom.InteractionClasses()
	b.dimensions = fom.Dimensions()
	b.indexClasses()
	b.advisoryDefaults = indexAdvisoryDefaults(fom)
	for i := range b.objects {
		var members []model.Attribute
		for _, ancestor := range b.objectAncestors[i] {
			members = append(members, b.objects[ancestor-1].Attributes...)
		}
		b.attributes = append(b.attributes, members)
	}
	for i := range b.interactions {
		var members []model.Parameter
		for _, ancestor := range b.interactionAncestors[i] {
			members = append(members, b.interactions[ancestor-1].Parameters...)
		}
		b.parameters = append(b.parameters, members)
	}
	return b
}

// appendBindings retains every old identity and appends only new identities.
// Snapshot restore uses the same operation with an explicitly ordered base.
func appendBindings[T any](old, next []T, key func(T) string, compatible func(T, T) bool) ([]T, error) {
	byName := make(map[string]T, len(next))
	for _, item := range next {
		name := key(item)
		if _, exists := byName[name]; exists {
			return nil, fmt.Errorf("ambiguous FOM binding %q", name)
		}
		byName[name] = item
	}
	result := make([]T, 0, len(next))
	used := make(map[string]bool, len(old))
	for _, item := range old {
		name := key(item)
		current, ok := byName[name]
		if !ok || !compatible(item, current) {
			return nil, fmt.Errorf("FOM extension changes existing declaration %q", name)
		}
		result = append(result, current)
		used[name] = true
	}
	for _, item := range next {
		if !used[key(item)] {
			result = append(result, item)
		}
	}
	return result, nil
}

func stableBindings(base, candidate *fomHandle) (*fomBindings, error) {
	old, next := base.bindings, candidate.bindings
	b := &fomBindings{advisoryDefaults: next.advisoryDefaults}
	var err error
	b.objects, err = appendBindings(old.objects, next.objects, func(c model.ObjectClass) string { return c.Key() }, func(a, b model.ObjectClass) bool { return a.ParentKey() == b.ParentKey() })
	if err != nil {
		return nil, err
	}
	b.interactions, err = appendBindings(old.interactions, next.interactions, func(c model.InteractionClass) string { return c.Key() }, func(a, b model.InteractionClass) bool {
		a.Parameters = nil
		b.Parameters = nil
		return reflect.DeepEqual(a, b)
	})
	if err != nil {
		return nil, err
	}
	b.dimensions, err = appendBindings(old.dimensions, next.dimensions, func(d model.Dimension) string { return d.Name }, func(a, b model.Dimension) bool { return a == b })
	if err != nil {
		return nil, err
	}
	for _, c := range b.objects {
		ni := objectClassIndexByName(next.objects, c.Key())
		oi := objectClassIndexByName(old.objects, c.Key())
		var previous []model.Attribute
		if oi >= 0 {
			previous = old.attributes[oi]
		}
		members, err := appendBindings(previous, next.attributes[ni], func(a model.Attribute) string { return a.Name }, func(a, b model.Attribute) bool { return reflect.DeepEqual(a, b) })
		if err != nil {
			return nil, fmt.Errorf("object class %s: %w", c.Key(), err)
		}
		b.attributes = append(b.attributes, members)
	}
	for _, c := range b.interactions {
		ni := interactionClassIndexByName(next.interactions, c.Key())
		oi := interactionClassIndexByName(old.interactions, c.Key())
		var previous []model.Parameter
		if oi >= 0 {
			previous = old.parameters[oi]
		}
		members, err := appendBindings(previous, next.parameters[ni], func(p model.Parameter) string { return p.Name }, func(a, b model.Parameter) bool { return a == b })
		if err != nil {
			return nil, fmt.Errorf("interaction class %s: %w", c.Key(), err)
		}
		b.parameters = append(b.parameters, members)
	}
	b.indexClasses()
	return b, nil
}

func (h *fomHandle) effectiveAttributes(cls core.ObjectClassHandle) []model.Attribute {
	if !h.IsValid() || cls == 0 || uint64(cls) > uint64(len(h.bindings.attributes)) {
		return nil
	}
	return h.bindings.attributes[cls-1]
}

func (h *fomHandle) effectiveParameters(cls core.InteractionClassHandle) []model.Parameter {
	if !h.IsValid() || cls == 0 || uint64(cls) > uint64(len(h.bindings.parameters)) {
		return nil
	}
	return h.bindings.parameters[cls-1]
}
