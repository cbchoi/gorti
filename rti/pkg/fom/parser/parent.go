package parser

import (
	"fmt"
)

// objectClassRootName is the canonical universal ancestor declared by the
// MIM (IEEE 1516.2-2010 §6 / Annex B). A FOM object class with no declared
// parent is only legitimate when it IS the root.
const objectClassRootName = "HLAobjectRoot"

// parentValidator emits FOM-011 for every object class whose ParentName
// fails to resolve. Resolution succeeds when the parent name is found in
// the same module's class set OR is the universal root (HLAobjectRoot).
//
// TODO(#1): once TASK-008 lands, "found in same module" expands to "found
// in module + loaded MIM" so user FOMs can extend MIM-defined classes.
type parentValidator struct{}

func init() {
	diagnosers = append(diagnosers, parentValidator{})
}

func (parentValidator) Run(in diagnosticInput) []Diagnostic {
	if in.fom == nil {
		return nil
	}
	classes := in.fom.ObjectClasses()
	known := map[string]struct{}{
		objectClassRootName: {},
	}
	for _, oc := range classes {
		known[oc.Name] = struct{}{}
	}

	reported := map[string]struct{}{}
	var diags []Diagnostic
	for _, oc := range classes {
		if oc.Name == objectClassRootName {
			continue
		}
		if oc.ParentName == "" {
			key := oc.Name + "\x00<root>"
			if _, dup := reported[key]; dup {
				continue
			}
			reported[key] = struct{}{}
			diags = append(diags, Diagnostic{
				Code: "FOM-011",
				Message: fmt.Sprintf(
					"object class %q is not nested under %s; missing parent class",
					oc.Name, objectClassRootName,
				),
				ModulePath: in.modulePath,
			})
			continue
		}
		if _, ok := known[oc.ParentName]; !ok {
			key := oc.Name + "\x00" + oc.ParentName
			if _, dup := reported[key]; dup {
				continue
			}
			reported[key] = struct{}{}
			diags = append(diags, Diagnostic{
				Code: "FOM-011",
				Message: fmt.Sprintf(
					"object class %q references non-existent parent %q",
					oc.Name, oc.ParentName,
				),
				ModulePath: in.modulePath,
			})
		}
	}
	return diags
}
