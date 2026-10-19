package parser

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// Positional dimension handles require one definition per name after merging.
func mergeDimensionDeclarations(path string, dimensions, next []model.Dimension) ([]model.Dimension, []Diagnostic) {
	known := make(map[string]model.Dimension, len(dimensions))
	for _, d := range dimensions {
		known[d.Name] = d
	}
	var diags []Diagnostic
	for _, d := range next {
		if previous, exists := known[d.Name]; exists {
			if previous != d {
				diags = append(diags, Diagnostic{Code: "FOM-015", ModulePath: path, Message: fmt.Sprintf("conflicting dimension definition %q", d.Name)})
			}
			continue
		}
		known[d.Name] = d
		dimensions = append(dimensions, d)
	}
	return dimensions, diags
}
