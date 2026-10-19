package mim

import (
	"fmt"
	"slices"

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
	if base == nil && user == nil {
		return nil, nil
	}
	if base == nil {
		base = model.NewFOM(nil, nil, nil)
	}
	if user == nil {
		user = model.NewFOM(nil, nil, nil)
	}

	dimensions, diags := mergeDimensionDeclarations(base.Dimensions(), user.Dimensions())
	transportations, transportationErr := model.MergeTransportations(base.Transportations(), user.Transportations())
	if transportationErr != nil {
		diags = append(diags, Diagnostic{Code: "FOM-101", Message: transportationErr.Error()})
	}
	updateRates, rateErr := model.MergeUpdateRates(base.UpdateRates(), user.UpdateRates())
	advisorySwitches, advisoryErr := model.MergeAdvisorySwitches(base.AdvisorySwitches(), user.AdvisorySwitches())
	if advisoryErr != nil {
		diags = append(diags, Diagnostic{Code: "FOM-101", Message: advisoryErr.Error()})
	}
	if rateErr != nil {
		diags = append(diags, Diagnostic{Code: "FOM-101", Message: rateErr.Error()})
	}
	if base.AutomaticResignAction() != "" && user.AutomaticResignAction() != "" && base.AutomaticResignAction() != user.AutomaticResignAction() {
		diags = append(diags, Diagnostic{Code: "FOM-101", Message: "conflicting automatic resign action during MIM merge"})
	}

	diags = append(diags, mimRedefinitions(base, user)...)

	if len(diags) > 0 {
		return nil, diags
	}

	merged := mergeNoCollision(base, user, dimensions)
	automaticResign := user.AutomaticResignAction()
	if automaticResign == "" {
		automaticResign = base.AutomaticResignAction()
	}
	return model.NewFOMWithUpdateRates(merged.ObjectClasses(), merged.InteractionClasses(), merged.DataTypes(), dimensions, updateRates).WithTransportations(transportations).WithAdvisorySwitches(advisorySwitches).WithAutomaticResignAction(automaticResign), nil
}

func mergeDimensionDeclarations(base, user []model.Dimension) ([]model.Dimension, []Diagnostic) {
	known := make(map[string]model.Dimension, len(base)+len(user))
	var dimensions []model.Dimension
	var diags []Diagnostic
	for _, definitions := range [][]model.Dimension{base, user} {
		for _, d := range definitions {
			if previous, exists := known[d.Name]; exists {
				if previous != d {
					diags = append(diags, Diagnostic{Code: "FOM-101", Message: fmt.Sprintf("conflicting dimension definition %q during MIM merge", d.Name)})
				}
				continue
			}
			known[d.Name] = d
			dimensions = append(dimensions, d)
		}
	}
	return dimensions, diags
}

// indexObjectClasses returns a name-keyed lookup of the FOM's object
// classes. The FOM is assumed name-deduplicated (NewFOM sorts and the
// upstream parser stores a flat tree); duplicates within the input would be
// silently overwritten which mirrors the existing parser behavior.
func indexObjectClasses(f *model.FOM) map[string]model.ObjectClass {
	out := make(map[string]model.ObjectClass)
	for _, oc := range f.ObjectClasses() {
		out[oc.Key()] = oc
	}
	return out
}

func indexInteractionClasses(f *model.FOM) map[string]model.InteractionClass {
	out := make(map[string]model.InteractionClass)
	for _, ic := range f.InteractionClasses() {
		out[ic.Key()] = ic
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
func isObjectClassPassthrough(oc, base model.ObjectClass) bool {
	_, compatible := model.MergeOptionalString(base.Semantics, oc.Semantics)
	return compatible && len(oc.Attributes) == 0 && (oc.Sharing == "" || base.Sharing == "" || oc.Sharing == base.Sharing)
}

// isInteractionClassPassthrough mirrors isObjectClassPassthrough for
// interaction classes. A supplied dimension set must match the MIM; an
// omitted set leaves the inherited container's dimension context intact.
func isInteractionClassPassthrough(ic, base model.InteractionClass) bool {
	_, compatible := model.MergeOptionalString(base.Semantics, ic.Semantics)
	if !compatible || len(ic.Parameters) != 0 {
		return false
	}
	if len(ic.Dimensions) == 0 {
		return true
	}
	left, right := slices.Clone(ic.Dimensions), slices.Clone(base.Dimensions)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(slices.Compact(left), slices.Compact(right))
}

// mergeNoCollision unions the declarations from base and user. Where the
// same name appears in both, the base (MIM) version wins — user is allowed
// to mention the name (pass-through) but not override its definition.
// model.NewFOM stable-sorts the resulting slices.
func mergeNoCollision(base, user *model.FOM, dimensions []model.Dimension) *model.FOM {
	mimObjectClasses := indexObjectClasses(base)
	mimInteractionClasses := indexInteractionClasses(base)
	mimDataTypes := indexDataTypes(base)

	objectClasses := append([]model.ObjectClass(nil), base.ObjectClasses()...)
	for _, oc := range user.ObjectClasses() {
		if existing, ok := mimObjectClasses[oc.Key()]; ok {
			for i := range objectClasses {
				if objectClasses[i].Key() == oc.Key() {
					objectClasses[i].Semantics, _ = model.MergeOptionalString(existing.Semantics, oc.Semantics)
					if existing.Sharing == "" && oc.Sharing != "" {
						objectClasses[i].Sharing = oc.Sharing
					}
					break
				}
			}
			continue
		}
		objectClasses = append(objectClasses, oc)
	}

	interactionClasses := append([]model.InteractionClass(nil), base.InteractionClasses()...)
	for _, ic := range user.InteractionClasses() {
		if existing, ok := mimInteractionClasses[ic.Key()]; ok {
			for i := range interactionClasses {
				if interactionClasses[i].Key() == ic.Key() {
					interactionClasses[i].Semantics, _ = model.MergeOptionalString(existing.Semantics, ic.Semantics)
					break
				}
			}
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

	return model.NewFOMWithDimensions(objectClasses, interactionClasses, dataTypes, dimensions)
}

func mimRedefinitions(base, user *model.FOM) []Diagnostic {
	var diags []Diagnostic
	mimObjectClasses := indexObjectClasses(base)
	mimInteractionClasses := indexInteractionClasses(base)
	mimDataTypes := indexDataTypes(base)

	for _, oc := range user.ObjectClasses() {
		baseClass, ok := mimObjectClasses[oc.Key()]
		if !ok {
			continue
		}
		if isObjectClassPassthrough(oc, baseClass) {
			continue
		}
		diags = append(diags, Diagnostic{
			Code:    "FOM-101",
			Message: fmt.Sprintf("user module redefines MIM object class %q", oc.Name),
		})
	}
	for _, ic := range user.InteractionClasses() {
		baseClass, ok := mimInteractionClasses[ic.Key()]
		if !ok {
			continue
		}
		if isInteractionClassPassthrough(ic, baseClass) {
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
	return diags
}
