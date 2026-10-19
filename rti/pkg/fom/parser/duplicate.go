package parser

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// duplicateAttributeValidator emits FOM-004 when an object class declares
// an attribute name that already exists in the same class or any of its
// ancestors. Direct duplicates (the same name listed twice on one class)
// also count.
type duplicateAttributeValidator struct{}

func init() {
	diagnosers = append(diagnosers, duplicateAttributeValidator{})
}

func (duplicateAttributeValidator) Run(in diagnosticInput) []Diagnostic {
	if in.fom == nil {
		return nil
	}
	classes := in.fom.ObjectClasses()
	byKey := map[string]model.ObjectClass{}
	for _, oc := range classes {
		byKey[oc.Key()] = oc
	}

	reported := map[string]struct{}{}
	var diags []Diagnostic
	for _, oc := range classes {
		// Report each (class key, attribute name) once across re-declarations.
		// Ancestor traversal stops at cycles even when FOM-002 also fires.
		ancestors := map[string]string{} // attrName -> declaring class
		for _, owner := range walkAncestorsInclusive(oc, byKey) {
			for _, a := range owner.Attributes {
				if prev, dup := ancestors[a.Name]; dup {
					key := oc.Key() + "\x00" + a.Name
					if _, already := reported[key]; already {
						continue
					}
					reported[key] = struct{}{}
					msg := fmt.Sprintf(
						"attribute %q duplicated in object class %q (also declared on %q)",
						a.Name, oc.Key(), prev,
					)
					if prev == owner.Key() {
						msg = fmt.Sprintf(
							"attribute %q duplicated in object class %q",
							a.Name, owner.Key(),
						)
					}
					diags = append(diags, Diagnostic{
						Code:       "FOM-004",
						Message:    msg,
						ModulePath: in.modulePath,
					})
					continue
				}
				ancestors[a.Name] = owner.Key()
			}
		}
	}
	return diags
}

// walkAncestorsInclusive returns ancestors followed by the class itself,
// so an inherited duplicate reports the ancestor as the prior owner.
// Stops if the chain repeats a class key or references a parent not in
// byKey (other diagnostics handle those cases).
func walkAncestorsInclusive(start model.ObjectClass, byKey map[string]model.ObjectClass) []model.ObjectClass {
	var chain []model.ObjectClass
	visited := map[string]struct{}{}
	cur := start
	for {
		if _, loop := visited[cur.Key()]; loop {
			break
		}
		visited[cur.Key()] = struct{}{}
		chain = append([]model.ObjectClass{cur}, chain...)
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
