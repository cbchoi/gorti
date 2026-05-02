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
	byName := map[string]model.InteractionClass{}
	for _, ic := range classes {
		byName[ic.Name] = ic
	}

	reported := map[string]struct{}{}
	var diags []Diagnostic
	for _, ic := range classes {
		ancestors := map[string]string{} // paramName -> declaring interaction
		for _, owner := range walkInteractionAncestorsInclusive(ic, byName) {
			for _, p := range owner.Parameters {
				if prev, dup := ancestors[p.Name]; dup {
					key := ic.Name + "\x00" + p.Name
					if _, already := reported[key]; already {
						continue
					}
					reported[key] = struct{}{}
					msg := fmt.Sprintf(
						"parameter %q duplicated in interaction class %q (also declared on %q)",
						p.Name, ic.Name, prev,
					)
					if prev == owner.Name {
						msg = fmt.Sprintf(
							"parameter %q duplicated in interaction class %q",
							p.Name, owner.Name,
						)
					}
					diags = append(diags, Diagnostic{
						Code:       "FOM-005",
						Message:    msg,
						ModulePath: in.modulePath,
					})
					continue
				}
				ancestors[p.Name] = owner.Name
			}
		}
	}
	return diags
}

// walkInteractionAncestorsInclusive mirrors walkAncestorsInclusive for
// InteractionClass; see that helper for behavior.
func walkInteractionAncestorsInclusive(start model.InteractionClass, byName map[string]model.InteractionClass) []model.InteractionClass {
	var chain []model.InteractionClass
	visited := map[string]struct{}{}
	cur := start
	for {
		if _, loop := visited[cur.Name]; loop {
			break
		}
		visited[cur.Name] = struct{}{}
		chain = append([]model.InteractionClass{cur}, chain...)
		if cur.ParentName == "" {
			break
		}
		next, ok := byName[cur.ParentName]
		if !ok {
			break
		}
		cur = next
	}
	return chain
}
