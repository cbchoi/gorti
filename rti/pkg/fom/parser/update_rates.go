package parser

import "github.com/cbchoi/gorti/rti/pkg/fom/model"

// Source: supplied IEEE1516-FDD-2010.xsd updateRatesType/updateRateType,
// RateType (xs:decimal). This decoder is not a full XSD validator.
type xmlUpdateRates struct {
	Rates []xmlUpdateRate `xml:"updateRate"`
}
type xmlUpdateRate struct {
	Name      string  `xml:"name"`
	Rate      string  `xml:"rate"`
	Semantics *string `xml:"semantics"`
}

func mergeUpdateRateDeclarations(path string, base []model.UpdateRate, raw *xmlUpdateRates) ([]model.UpdateRate, []Diagnostic) {
	if raw == nil {
		return base, nil
	}
	out := base
	var diags []Diagnostic
	for _, entry := range raw.Rates {
		semantics := model.OptionalStringFrom(entry.Semantics)
		rate, err := model.NewUpdateRate(entry.Name, entry.Rate, semantics.Value)
		rate.SemanticsPresent = semantics.Present
		if err == nil {
			out, err = model.MergeUpdateRates(out, []model.UpdateRate{rate})
		}
		if err != nil {
			diags = append(diags, Diagnostic{Code: "FOM-016", ModulePath: path, Message: err.Error()})
		}
	}
	return out, diags
}
