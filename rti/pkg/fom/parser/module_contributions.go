package parser

import (
	"fmt"
	"strings"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// ContributingModuleIndices identifies first additions to the federation FDD
// under IEEE 1516.1-2010 4.1.4.2(a)(1). Inputs must already have passed admission;
// this is provenance accounting, not a replacement for Parse validation.
func ContributingModuleIndices(modules []Module, baseline *model.FOM) ([]int, error) {
	seen := make(map[string]bool)
	add := func(kind, name string) bool {
		key := kind + ":" + name
		fresh := !seen[key]
		seen[key] = true
		return fresh
	}
	addBaselineContributions(baseline, add)
	var indices []int
	for i, module := range modules {
		om, err := decodeModule(module)
		if err != nil {
			return nil, fmt.Errorf("module contribution %q: %w", module.Path, err)
		}
		contributes := addModuleContributions(om, add)
		if contributes {
			indices = append(indices, i)
		}
	}
	return indices, nil
}

func addBaselineContributions(baseline *model.FOM, add func(string, string) bool) {
	if baseline != nil {
		for _, c := range baseline.ObjectClasses() {
			add(objectKind, c.Key())
		}
		for _, c := range baseline.InteractionClasses() {
			add(interactionKind, c.Key())
		}
		for _, d := range baseline.Dimensions() {
			add("dimension", d.Name)
		}
		for _, t := range baseline.Transportations() {
			add("transportation", t.Name)
		}
		for _, r := range baseline.UpdateRates() {
			add("rate", r.Name)
		}
	}
}

func addModuleContributions(om *xmlObjectModel, add func(string, string) bool) bool {
	var objects []model.ObjectClass
	var interactions []model.InteractionClass
	if om.Objects != nil {
		flattenObjectClasses(om.Objects.ObjectClass, "", &objects)
	}
	if om.Interactions != nil {
		flattenInteractionClasses(om.Interactions.InteractionClass, "", &interactions)
	}
	contributes := false
	for _, c := range objects {
		contributes = add(objectKind, c.Key()) || contributes
	}
	for _, c := range interactions {
		contributes = add(interactionKind, c.Key()) || contributes
	}
	return addMetadataContributions(om, add) || contributes
}

func addMetadataContributions(om *xmlObjectModel, add func(string, string) bool) bool {
	contributes := false
	if om.Dimensions != nil {
		for _, d := range om.Dimensions.Dimension {
			contributes = add("dimension", strings.TrimSpace(d.Name)) || contributes
		}
	}
	if om.Transportations != nil {
		for _, t := range om.Transportations.Declarations {
			contributes = add("transportation", strings.TrimSpace(t.Name)) || contributes
		}
	}
	if om.UpdateRates != nil {
		for _, r := range om.UpdateRates.Rates {
			contributes = add("rate", strings.TrimSpace(r.Name)) || contributes
		}
	}
	return contributes
}
