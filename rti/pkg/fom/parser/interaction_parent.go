package parser

import (
	"fmt"
)

// interactionClassRootName is the canonical universal interaction
// ancestor declared by the MIM (IEEE 1516.2-2010 §6 / Annex B).
const interactionClassRootName = "HLAinteractionRoot"

// interactionParentValidator emits FOM-012 for every interaction class
// whose ParentName fails to resolve. Resolution succeeds when the parent
// name is found in the same module's interaction set OR is the universal
// root (HLAinteractionRoot).
//
// TODO(#1): once TASK-008 lands, "found in same module" expands to "found
// in module + loaded MIM" so user FOMs can extend MIM-defined interactions.
type interactionParentValidator struct{}

func init() {
	diagnosers = append(diagnosers, interactionParentValidator{})
}

func (interactionParentValidator) Run(in diagnosticInput) []Diagnostic {
	if in.fom == nil {
		return nil
	}
	classes := in.fom.InteractionClasses()
	known := map[string]struct{}{
		interactionClassRootName: {},
	}
	for _, ic := range classes {
		known[ic.Name] = struct{}{}
	}

	reported := map[string]struct{}{}
	var diags []Diagnostic
	for _, ic := range classes {
		if ic.Name == interactionClassRootName {
			continue
		}
		if ic.ParentName == "" {
			key := ic.Name + "\x00<root>"
			if _, dup := reported[key]; dup {
				continue
			}
			reported[key] = struct{}{}
			diags = append(diags, Diagnostic{
				Code: "FOM-012",
				Message: fmt.Sprintf(
					"interaction class %q is not nested under %s; missing parent class",
					ic.Name, interactionClassRootName,
				),
				ModulePath: in.modulePath,
			})
			continue
		}
		if _, ok := known[ic.ParentName]; !ok {
			key := ic.Name + "\x00" + ic.ParentName
			if _, dup := reported[key]; dup {
				continue
			}
			reported[key] = struct{}{}
			diags = append(diags, Diagnostic{
				Code: "FOM-012",
				Message: fmt.Sprintf(
					"interaction class %q references non-existent parent %q",
					ic.Name, ic.ParentName,
				),
				ModulePath: in.modulePath,
			})
		}
	}
	return diags
}
