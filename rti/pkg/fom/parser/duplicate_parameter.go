package parser

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// duplicateParameterValidator emits FOM-005 when an interaction class
// declares a parameter name that already exists in the same class or any
// of its ancestors. Symmetric to duplicate.go (FOM-004) for object class
// attributes.
type duplicateParameterValidator struct{}

func init() {
	diagnosers = append(diagnosers, duplicateParameterValidator{})
}

func (duplicateParameterValidator) Run(in diagnosticInput) []Diagnostic {
	if in.fom == nil {
		return nil
	}
	classes := in.fom.InteractionClasses()
	byKey := map[string]model.InteractionClass{}
	for _, ic := range classes {
		byKey[ic.Key()] = ic
	}

	reported := map[string]struct{}{}
	var diags []Diagnostic
	for _, ic := range classes {
		ancestors := map[string]string{} // paramName -> declaring interaction
		for _, owner := range walkInteractionAncestorsInclusive(ic, byKey) {
			for _, p := range owner.Parameters {
				if prev, dup := ancestors[p.Name]; dup {
					key := ic.Key() + "\x00" + p.Name
					if _, already := reported[key]; already {
						continue
					}
					reported[key] = struct{}{}
					msg := fmt.Sprintf(
						"parameter %q duplicated in interaction class %q (also declared on %q)",
						p.Name, ic.Key(), prev,
					)
					if prev == owner.Key() {
						msg = fmt.Sprintf(
							"parameter %q duplicated in interaction class %q",
							p.Name, owner.Key(),
						)
					}
					diags = append(diags, Diagnostic{
						Code:       "FOM-005",
						Message:    msg,
						ModulePath: in.modulePath,
					})
					continue
				}
				ancestors[p.Name] = owner.Key()
			}
		}
	}
	return diags
}

// walkInteractionAncestorsInclusive mirrors walkAncestorsInclusive for
// InteractionClass; see that helper for behavior.
func walkInteractionAncestorsInclusive(start model.InteractionClass, byKey map[string]model.InteractionClass) []model.InteractionClass {
	var chain []model.InteractionClass
	visited := map[string]struct{}{}
	cur := start
	for {
		if _, loop := visited[cur.Key()]; loop {
			break
		}
		visited[cur.Key()] = struct{}{}
		chain = append([]model.InteractionClass{cur}, chain...)
		if cur.ParentKey() == "" {
			break
		}
		next, ok := byKey[cur.ParentKey()]
		if !ok {
			break
		}
		cur = next
	}
	return chain
}
