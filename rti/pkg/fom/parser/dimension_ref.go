package parser

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// Resolve after all modules are collected: a container extension can refer
// to a dimension supplied by a different module or the standard MIM.
func validateDimensionReferences(inputs []diagnosticInput, dimensions []model.Dimension) []Diagnostic {
	known := make(map[string]bool, len(dimensions))
	for _, d := range dimensions {
		known[d.Name] = true
	}
	if base, err := mim.StandardMIMHandle(); err == nil && base != nil {
		for _, d := range base.Dimensions() {
			known[d.Name] = true
		}
	}
	var diags []Diagnostic
	for _, in := range inputs {
		check := func(owner string, refs []string) {
			for _, name := range refs {
				if name == "" || !known[name] {
					diags = append(diags, Diagnostic{
						Code:       "FOM-015",
						ModulePath: in.modulePath,
						Message:    fmt.Sprintf("%s references undefined dimension %q", owner, name),
					})
				}
			}
		}
		for _, c := range in.fom.ObjectClasses() {
			for _, a := range c.Attributes {
				check(fmt.Sprintf("attribute %q of object class %q", a.Name, c.Key()), a.Dimensions)
			}
		}
		for _, c := range in.fom.InteractionClasses() {
			check(fmt.Sprintf("interaction class %q", c.Key()), c.Dimensions)
		}
	}
	return diags
}
