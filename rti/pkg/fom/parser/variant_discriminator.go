package parser

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// variantDiscriminatorValidator emits FOM-013 for any VariantRecordData
// declared without a non-empty discriminant. Per IEEE 1516.2-2010 §6.7,
// the discriminant identifies the runtime field selecting which
// alternative carries the payload; without it, encoding is undefined.
type variantDiscriminatorValidator struct{}

func init() {
	diagnosers = append(diagnosers, variantDiscriminatorValidator{})
}

func (variantDiscriminatorValidator) Run(in diagnosticInput) []Diagnostic {
	if in.fom == nil {
		return nil
	}
	var diags []Diagnostic
	for _, dt := range in.fom.DataTypes() {
		v, ok := dt.(*model.VariantRecordData)
		if !ok {
			continue
		}
		if v.DiscriminantName == "" {
			diags = append(diags, Diagnostic{
				Code: "FOM-013",
				Message: fmt.Sprintf(
					"variantRecordData %q is missing a discriminant field",
					v.NameField,
				),
				ModulePath: in.modulePath,
			})
		}
	}
	return diags
}
