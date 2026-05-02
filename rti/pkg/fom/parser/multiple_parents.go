package parser

import (
	"fmt"
	"sort"
	"strings"
)

// multipleParentsValidator emits FOM-003 when an object class name appears
// in the FOM with two or more distinct parent names. Detected at the
// post-walk stage by reading the flattened ObjectClass list, where each
// re-declaration of a class under a different parent creates an additional
// (Name, ParentName) entry.
type multipleParentsValidator struct{}

func init() {
	diagnosers = append(diagnosers, multipleParentsValidator{})
}

func (multipleParentsValidator) Run(in diagnosticInput) []Diagnostic {
	if in.fom == nil {
		return nil
	}
	parents := buildObjectClassParentSets(in.fom.ObjectClasses())

	var diags []Diagnostic
	for _, name := range sortedKeys(parents) {
		ps := parents[name]
		if len(ps) < 2 {
			continue
		}
		parentList := sortedKeys(ps)
		// Stable for diagnostic comparability across runs.
		sort.Strings(parentList)
		diags = append(diags, Diagnostic{
			Code: "FOM-003",
			Message: fmt.Sprintf(
				"object class %q has multiple parents: %s",
				name, strings.Join(parentList, ", "),
			),
			ModulePath: in.modulePath,
		})
	}
	return diags
}
