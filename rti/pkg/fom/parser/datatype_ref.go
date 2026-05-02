package parser

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
)

// datatypeRefValidator emits FOM-001 for every attribute or parameter
// whose declared dataType name does not resolve to either a MIM-provided
// dataType or a type declared in the FOM module.
//
// The MIM-provided set is sourced from the embedded standard MIM via
// mim.StandardMIMHandle (TASK-008 / TASK-009). Names covered include the
// basic representations (HLAinteger*, HLAfloat*, HLAoctet*), the simpleData
// aliases (HLAboolean, HLAhandle, HLAASCIIchar, HLAunicodeChar) and the
// arrayData strings (HLAASCIIstring, HLAunicodeString, HLAopaqueData) — i.e.
// every name the standard MIM declares.
type datatypeRefValidator struct{}

func init() {
	diagnosers = append(diagnosers, datatypeRefValidator{})
}

// mimDataTypeNames returns the set of dataType names declared by the
// embedded standard MIM. If the MIM cannot be loaded (build-time bug in
// the vendored XML), returns nil; the caller treats that as an empty set
// and FOM-001 firing for every previously-resolved name surfaces the
// problem loudly. The set is recomputed on every call rather than cached
// to keep the code path stateless; mim.StandardMIMHandle is itself
// memoized so the underlying parse runs at most once per process.
func mimDataTypeNames() map[string]struct{} {
	base, err := mim.StandardMIMHandle()
	if err != nil || base == nil {
		return nil
	}
	out := make(map[string]struct{}, len(base.DataTypes()))
	for _, dt := range base.DataTypes() {
		out[dt.Name()] = struct{}{}
	}
	return out
}

func (datatypeRefValidator) Run(in diagnosticInput) []Diagnostic {
	if in.fom == nil {
		return nil
	}
	mimNames := mimDataTypeNames()
	declared := map[string]struct{}{}
	for _, dt := range in.fom.DataTypes() {
		declared[dt.Name()] = struct{}{}
	}
	resolves := func(name string) bool {
		if name == "" {
			return true
		}
		if _, ok := mimNames[name]; ok {
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
