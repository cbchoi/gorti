package parser

import (
	"strings"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

type xmlTransportations struct {
	Declarations []struct {
		Name      string  `xml:"name"`
		Reliable  string  `xml:"reliable"`
		Semantics *string `xml:"semantics"`
	} `xml:"transportation"`
}

func mergeTransportationDeclarations(path string, base []model.Transportation, raw *xmlTransportations) ([]model.Transportation, []Diagnostic) {
	if raw == nil {
		return base, nil
	}
	values := make([]model.Transportation, 0, len(raw.Declarations))
	for _, entry := range raw.Declarations {
		values = append(values, model.Transportation{
			Name: strings.TrimSpace(entry.Name), Reliable: entry.Reliable,
			Semantics: model.OptionalStringFrom(entry.Semantics),
		})
	}
	merged, err := model.MergeTransportations(base, values)
	if err != nil {
		return base, []Diagnostic{{Code: "FOM-016", ModulePath: path, Message: err.Error()}}
	}
	return merged, nil
}
