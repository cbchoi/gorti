package parser

import (
	"fmt"
	"sort"
	"strings"
)

// multipleParentsValidator emits FOM-003 when an object class key appears
// with two or more distinct parent keys. Equal XML leaf names under
// different parents are separate identities, not multiple inheritance.
// Legacy programmatic graphs can still declare multiple parents for one key.
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
