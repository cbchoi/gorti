package parser

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// Collapse repeated qualified containers before assigning positional handles.
// IEEE 1516.1-2010 4.1.4.1 and 1516.2-2010 Table 50 require matching
// regular member sets while allowing name-only scaffolding to be completed.
func mergeClassDeclarations(path string, objects []model.ObjectClass, interactions []model.InteractionClass, nextObjects []model.ObjectClass, nextInteractions []model.InteractionClass) ([]model.ObjectClass, []model.InteractionClass, []Diagnostic) {
	var diags []Diagnostic
	reject := func(kind, key string) {
		diags = append(diags, Diagnostic{Code: "FOM-014", ModulePath: path, Message: fmt.Sprintf("duplicate or conflicting %s class definition %q", kind, key)})
	}
	objects = mergeObjectDeclarations(objects, nextObjects, reject)
	interactions = mergeInteractionDeclarations(interactions, nextInteractions, reject)
	return objects, interactions, diags
}

// Compare sets by name, preserving the first declaration's member order.
func mergeMembers[T any](old, next []T, key func(T) string, merge func(T, T) (T, bool)) ([]T, bool) {
	if len(old) != len(next) {
		return nil, false
	}
	byName := make(map[string]T, len(next))
	for _, member := range next {
		byName[key(member)] = member
	}
	result := slices.Clone(old)
	for i, member := range old {
		other, exists := byName[key(member)]
		if !exists {
			return nil, false
		}
		var ok bool
		result[i], ok = merge(member, other)
		if !ok {
			return nil, false
		}
		delete(byName, key(member))
	}
	return result, len(byName) == 0
}

func conflictingValue(a, b string) bool { return a != "" && b != "" && a != b }

func objectContainer(c model.ObjectClass) bool {
	return c.Sharing == "" && !c.Semantics.Present && len(c.Attributes) == 0
}

func interactionContainer(c model.InteractionClass) bool {
	return c.Sharing == "" && !c.Semantics.Present && c.Order == "" && c.Transportation == "" && len(c.Parameters) == 0 && len(c.Dimensions) == 0
}

func mergeAttribute(a, b model.Attribute) (model.Attribute, bool) {
	result := a
	result.Semantics = retainDescription(a.Semantics, b.Semantics)
	result.UpdateCondition = retainDescription(a.UpdateCondition, b.UpdateCondition)
	a.Semantics, b.Semantics = model.OptionalString{}, model.OptionalString{}
	a.UpdateCondition, b.UpdateCondition = model.OptionalString{}, model.OptionalString{}
	if !sameDimensionSet(a.Dimensions, b.Dimensions) {
		return model.Attribute{}, false
	}
	a.Dimensions, b.Dimensions = nil, nil
	return result, reflect.DeepEqual(a, b)
}

func mergeParameter(a, b model.Parameter) (model.Parameter, bool) {
	result := a
	result.Semantics = retainDescription(a.Semantics, b.Semantics)
	a.Semantics, b.Semantics = model.OptionalString{}, model.OptionalString{}
	return result, a == b
}

// Description differences do not reject FDD combination (1516.1-2010
// 4.1.4-4.1.4.1). Retaining the first supplied value is an engineering policy.
func retainDescription(a, b model.OptionalString) model.OptionalString {
	if a.Present {
		return a
	}
	return b
}

func sameDimensionSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

func mergeObjectDeclarations(objects, nextObjects []model.ObjectClass, reject func(string, string)) []model.ObjectClass {
	objectIndex := make(map[string]int, len(objects))
	for i, c := range objects {
		objectIndex[c.Key()] = i
	}
	seen := make(map[string]bool)
	for _, c := range nextObjects {
		key := c.Key()
		if seen[key] {
			reject(objectKind, key)
			continue
		}
		seen[key] = true
		if i, exists := objectIndex[key]; exists {
			old := objects[i]
			if old.ParentKey() != c.ParentKey() {
				reject(objectKind, key)
				continue
			}
			if objectContainer(c) {
				continue
			}
			if objectContainer(old) {
				objects[i] = c
				continue
			}
			members, ok := mergeMembers(old.Attributes, c.Attributes, func(a model.Attribute) string { return a.Name }, mergeAttribute)
			if conflictingValue(old.Sharing, c.Sharing) || !ok {
				reject(objectKind, key)
				continue
			}
			if old.Sharing == "" {
				old.Sharing = c.Sharing
			}
			old.Semantics = retainDescription(old.Semantics, c.Semantics)
			old.Attributes = members
			objects[i] = old
			continue
		}
		objectIndex[key] = len(objects)
		objects = append(objects, c)
	}
	return objects
}

func mergeInteractionDeclarations(interactions, nextInteractions []model.InteractionClass, reject func(string, string)) []model.InteractionClass {
	interactionIndex := make(map[string]int, len(interactions))
	for i, c := range interactions {
		interactionIndex[c.Key()] = i
	}
	seen := make(map[string]bool)
	for _, c := range nextInteractions {
		key := c.Key()
		if seen[key] {
			reject(interactionKind, key)
			continue
		}
		seen[key] = true
		if i, exists := interactionIndex[key]; exists {
			old := interactions[i]
			if old.ParentKey() != c.ParentKey() {
				reject(interactionKind, key)
				continue
			}
			if interactionContainer(c) {
				continue
			}
			if interactionContainer(old) {
				interactions[i] = c
				continue
			}
			parameters, ok := mergeMembers(old.Parameters, c.Parameters, func(p model.Parameter) string { return p.Name }, mergeParameter)
			if conflictingInteractionMetadata(old, c) || !ok {
				reject(interactionKind, key)
				continue
			}
			old.Parameters = parameters
			old.Semantics = retainDescription(old.Semantics, c.Semantics)
			if old.Sharing == "" {
				old.Sharing = c.Sharing
			}
			if old.Order == "" {
				old.Order = c.Order
			}
			if old.Transportation == "" {
				old.Transportation = c.Transportation
			}
			interactions[i] = old
			continue
		}
		interactionIndex[key] = len(interactions)
		interactions = append(interactions, c)
	}
	return interactions
}

func conflictingInteractionMetadata(old, c model.InteractionClass) bool {
	return conflictingValue(old.Sharing, c.Sharing) || conflictingValue(old.Order, c.Order) || conflictingValue(old.Transportation, c.Transportation) || !sameDimensionSet(old.Dimensions, c.Dimensions)
}
