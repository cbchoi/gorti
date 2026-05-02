package mim

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// Diagnostic mirrors parser.Diagnostic but is declared locally in this
// package so mim/merge.go can be import-cycle-free with respect to the
// parser package (which itself imports mim to wire FOM-101 into the parser
// pipeline). The parser-side mim_merge.go shim translates these to
// parser.Diagnostic — see rti/pkg/fom/parser/mim_merge.go.
type Diagnostic struct {
	Code       string
	Message    string
	ModulePath string
	Line       int
}

// Merge folds a user FOM module on top of the standard-MIM base FOM. It
// rejects user modules that redefine an MIM-provided name with FOM-101 —
// "redefine" means the user re-declares an MIM dataType, OR adds attributes
// to an MIM object class, OR adds parameters to an MIM interaction class.
// Pass-through declarations of the MIM root classes (HLAobjectRoot /
// HLAinteractionRoot used purely as the inheritance root, with no
// attributes / parameters added by the user) are NOT redefinition — that
// is the canonical pattern user FOMs follow per IEEE 1516.2-2010 Annex A.
//
// On any FOM-101 emission Merge returns (nil, diagnostics) so callers do
// not propagate a partial FOM downstream. On success the returned *model.FOM
// is a fresh value combining base + user declarations (user adds, never
// replaces). Slices in the result are sorted by name (model.NewFOM does the
// sort).
func Merge(base, user *model.FOM) (*model.FOM, []Diagnostic) {
	if base == nil {
		return user, nil
	}
	if user == nil {
		return base, nil
	}

	var diags []Diagnostic

	mimObjectClasses := indexObjectClasses(base)
	mimInteractionClasses := indexInteractionClasses(base)
	mimDataTypes := indexDataTypes(base)

	for _, oc := range user.ObjectClasses() {
		if _, ok := mimObjectClasses[oc.Name]; !ok {
			continue
		}
		if isObjectClassPassthrough(oc) {
			continue
		}
		diags = append(diags, Diagnostic{
			Code:    "FOM-101",
			Message: fmt.Sprintf("user module redefines MIM object class %q", oc.Name),
		})
	}
	for _, ic := range user.InteractionClasses() {
		if _, ok := mimInteractionClasses[ic.Name]; !ok {
			continue
		}
		if isInteractionClassPassthrough(ic) {
			continue
		}
		diags = append(diags, Diagnostic{
			Code:    "FOM-101",
			Message: fmt.Sprintf("user module redefines MIM interaction class %q", ic.Name),
		})
	}
	for _, dt := range user.DataTypes() {
		if _, ok := mimDataTypes[dt.Name()]; !ok {
			continue
		}
		diags = append(diags, Diagnostic{
			Code:    "FOM-101",
			Message: fmt.Sprintf("user module redefines MIM dataType %q", dt.Name()),
		})
	}

	if len(diags) > 0 {
		return nil, diags
	}

	return mergeNoCollision(base, user), nil
}

// indexObjectClasses returns a name-keyed lookup of the FOM's object
// classes. The FOM is assumed name-deduplicated (NewFOM sorts and the
// upstream parser stores a flat tree); duplicates within the input would be
// silently overwritten which mirrors the existing parser behavior.
func indexObjectClasses(f *model.FOM) map[string]model.ObjectClass {
	out := make(map[string]model.ObjectClass)
	for _, oc := range f.ObjectClasses() {
		out[oc.Name] = oc
	}
	return out
}

func indexInteractionClasses(f *model.FOM) map[string]model.InteractionClass {
	out := make(map[string]model.InteractionClass)
	for _, ic := range f.InteractionClasses() {
		out[ic.Name] = ic
	}
	return out
}

func indexDataTypes(f *model.FOM) map[string]model.DataType {
	out := make(map[string]model.DataType)
	for _, dt := range f.DataTypes() {
		out[dt.Name()] = dt
	}
	return out
}

// isObjectClassPassthrough reports whether the user's declaration of an MIM
// object class adds any new content (attributes). Pass-through containers
// reproduce the MIM root in the user FOM only to anchor the inheritance
// chain — they are not redefinition.
func isObjectClassPassthrough(oc model.ObjectClass) bool {
	return len(oc.Attributes) == 0
}

// isInteractionClassPassthrough mirrors isObjectClassPassthrough for
// interaction classes. Parameters are the only content the user can
// meaningfully add to an MIM interaction-root reference.
func isInteractionClassPassthrough(ic model.InteractionClass) bool {
	return len(ic.Parameters) == 0
}

// mergeNoCollision unions the declarations from base and user. Where the
// same name appears in both, the base (MIM) version wins — user is allowed
// to mention the name (pass-through) but not override its definition.
// model.NewFOM stable-sorts the resulting slices.
func mergeNoCollision(base, user *model.FOM) *model.FOM {
	mimObjectClasses := indexObjectClasses(base)
	mimInteractionClasses := indexInteractionClasses(base)
	mimDataTypes := indexDataTypes(base)

	objectClasses := append([]model.ObjectClass(nil), base.ObjectClasses()...)
	for _, oc := range user.ObjectClasses() {
		if _, ok := mimObjectClasses[oc.Name]; ok {
			continue
		}
		objectClasses = append(objectClasses, oc)
	}

	interactionClasses := append([]model.InteractionClass(nil), base.InteractionClasses()...)
	for _, ic := range user.InteractionClasses() {
		if _, ok := mimInteractionClasses[ic.Name]; ok {
			continue
		}
		interactionClasses = append(interactionClasses, ic)
	}

	dataTypes := append([]model.DataType(nil), base.DataTypes()...)
	for _, dt := range user.DataTypes() {
		if _, ok := mimDataTypes[dt.Name()]; ok {
			continue
		}
		dataTypes = append(dataTypes, dt)
	}

	return model.NewFOM(objectClasses, interactionClasses, dataTypes)
}
