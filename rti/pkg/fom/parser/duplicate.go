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
	byName := map[string]model.ObjectClass{}
	for _, oc := range classes {
		byName[oc.Name] = oc
	}

	reported := map[string]struct{}{}
	var diags []Diagnostic
	for _, oc := range classes {
		// Visit each (className, attrName) only once across re-declarations
		// produced by the flat list. Skip a class whose ParentName forms a
		// cycle so this pass terminates even when FOM-002 also fires.
		ancestors := map[string]string{} // attrName -> declaring class
		for _, owner := range walkAncestorsInclusive(oc, byName) {
			for _, a := range owner.Attributes {
				if prev, dup := ancestors[a.Name]; dup {
					key := oc.Name + "\x00" + a.Name
					if _, already := reported[key]; already {
						continue
					}
					reported[key] = struct{}{}
					msg := fmt.Sprintf(
						"attribute %q duplicated in object class %q (also declared on %q)",
						a.Name, oc.Name, prev,
					)
					if prev == owner.Name {
						msg = fmt.Sprintf(
							"attribute %q duplicated in object class %q",
							a.Name, owner.Name,
						)
					}
					diags = append(diags, Diagnostic{
						Code:       "FOM-004",
						Message:    msg,
						ModulePath: in.modulePath,
					})
					continue
				}
				ancestors[a.Name] = owner.Name
			}
		}
	}
	return diags
}

// walkAncestorsInclusive returns the class itself followed by each ancestor
// (parent, grandparent, ...) up to the root. Stops if the chain loops or
// names a parent not in byName (other diagnostics handle those cases).
// Returned order: ancestor-first then self, so a child redeclaring an
// inherited attribute reports the ancestor as the prior owner.
func walkAncestorsInclusive(start model.ObjectClass, byName map[string]model.ObjectClass) []model.ObjectClass {
	var chain []model.ObjectClass
	visited := map[string]struct{}{}
	cur := start
	for {
		if _, loop := visited[cur.Name]; loop {
			break
		}
		visited[cur.Name] = struct{}{}
		chain = append([]model.ObjectClass{cur}, chain...)
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
