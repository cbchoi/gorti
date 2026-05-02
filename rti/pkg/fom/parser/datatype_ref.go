package parser

import (
	"fmt"
)

// datatypeRefValidator emits FOM-001 for every attribute or parameter
// whose declared dataType name does not resolve to either a MIM-provided
// primitive or a type declared in the FOM module.
//
// TODO(#1): replace mimPrimitives with a lookup against the loaded MIM
// (rti/pkg/fom/mim) once TASK-008 lands. The hard-coded set below covers
// only the basic-data primitives the M1 spec tests reference; a full MIM
// also publishes simpleData (HLAcount, HLAtime, ...) and standard
// enumerations that user FOMs may reference.
type datatypeRefValidator struct{}

func init() {
	diagnosers = append(diagnosers, datatypeRefValidator{})
}

// mimPrimitives is the closed set of HLA Evolved basic data type names from
// IEEE 1516.2-2010 §6.2 (Annex B baseline). Used until TASK-008 lands a
// real MIM repository.
var mimPrimitives = map[string]struct{}{
	"HLAinteger16BE":   {},
	"HLAinteger16LE":   {},
	"HLAinteger32BE":   {},
	"HLAinteger32LE":   {},
	"HLAinteger64BE":   {},
	"HLAinteger64LE":   {},
	"HLAfloat32BE":     {},
	"HLAfloat32LE":     {},
	"HLAfloat64BE":     {},
	"HLAfloat64LE":     {},
	"HLAoctet":         {},
	"HLAoctetPairBE":   {},
	"HLAoctetPairLE":   {},
	"HLAboolean":       {},
	"HLAASCIIchar":     {},
	"HLAunicodeChar":   {},
	"HLAASCIIstring":   {},
	"HLAunicodeString": {},
	"HLAopaqueData":    {},
}

func (datatypeRefValidator) Run(in diagnosticInput) []Diagnostic {
	if in.fom == nil {
		return nil
	}
	declared := map[string]struct{}{}
	for _, dt := range in.fom.DataTypes() {
		declared[dt.Name()] = struct{}{}
	}
	resolves := func(name string) bool {
		if name == "" {
			return true
		}
		if _, ok := mimPrimitives[name]; ok {
			return true
		}
		_, ok := declared[name]
		return ok
	}

	var diags []Diagnostic
	for _, oc := range in.fom.ObjectClasses() {
		for _, a := range oc.Attributes {
			if !resolves(a.DataType) {
				diags = append(diags, Diagnostic{
					Code: "FOM-001",
					Message: fmt.Sprintf(
						"DataType %q referenced by attribute %s.%s but not defined",
						a.DataType, oc.Name, a.Name,
					),
					ModulePath: in.modulePath,
				})
			}
		}
	}
	for _, ic := range in.fom.InteractionClasses() {
		for _, p := range ic.Parameters {
			if !resolves(p.DataType) {
				diags = append(diags, Diagnostic{
					Code: "FOM-001",
					Message: fmt.Sprintf(
						"DataType %q referenced by parameter %s.%s but not defined",
						p.DataType, ic.Name, p.Name,
					),
					ModulePath: in.modulePath,
				})
			}
		}
	}
	return diags
}
