package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/ddm"
	"github.com/cbchoi/gorti/rti/internal/object"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/transport/grpc"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

// fomRepository adapts rti/pkg/fom into core.FOMRepository for the rtid
// composition. Load parses the supplied modules + applies MIM merge;
// Lookup by HLA-qualified name is fulfilled by walking the resulting
// *model.FOM. Per-federation handles are remembered via RememberFor
// (called by the federation manager wiring at CreateFederation success).
type fomRepository struct {
	mu       sync.RWMutex
	byFedKey map[core.FederationName]core.FOMHandle
}

// newFOMRepository constructs an empty repository.
func newFOMRepository() *fomRepository {
	return &fomRepository{
		byFedKey: map[core.FederationName]core.FOMHandle{},
	}
}

// Load implements core.FOMRepository.
//
// Empty modules list → returns a valid empty FOM handle (the federation
// manager treats this as "no FOM constraints"). This matches the M2 spec
// fixtures that supply nil modules to exercise lifecycle without FOM
// resolution; production code will typically supply at least one module.
func (r *fomRepository) Load(_ context.Context, modules []core.FOMModule) (core.FOMHandle, error) {
	if len(modules) == 0 {
		return &fomHandle{fom: model.NewFOM(nil, nil, nil)}, nil
	}

	parserModules := make([]parser.Module, len(modules))
	for i, m := range modules {
		parserModules[i] = parser.Module{Path: m.Path, XML: m.XML}
	}
	res, err := parser.Parse(parserModules)
	if err != nil {
		return nil, fmt.Errorf("rtid: parse FOM: %w", err)
	}
	if len(res.Diagnostics) > 0 {
		return nil, fmt.Errorf("rtid: FOM validation failed: %s", formatDiagnostics(res.Diagnostics))
	}
	fm, ok := res.FOM.(*model.FOM)
	if !ok {
		return nil, fmt.Errorf("rtid: parser returned unexpected FOM type %T", res.FOM)
	}
	mimFOM, mimErr := mim.StandardMIMHandle()
	if mimErr == nil && mimFOM != nil {
		merged, diags := mim.Merge(mimFOM, fm)
		if len(diags) > 0 {
			return nil, fmt.Errorf("rtid: MIM merge: %s", diags[0].Code+" "+diags[0].Message)
		}
		fm = merged
	}
	return &fomHandle{fom: fm}, nil
}

// Get implements core.FOMRepository. Returns ErrFederationNotFound if no
// RememberFor was issued for fed.
func (r *fomRepository) Get(_ context.Context, fed core.FederationName) (core.FOMHandle, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.byFedKey[fed]
	if !ok {
		return nil, core.ErrFederationNotFound
	}
	return h, nil
}

// RememberFor records the FOM handle returned from Load against fed so
// later Get calls return the same handle. Production wiring calls this
// from a CreateFederation post-hook (see main.go); tests call it directly.
func (r *fomRepository) RememberFor(fed core.FederationName, h core.FOMHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byFedKey[fed] = h
}

// formatDiagnostics builds a one-line summary of parser diagnostics for
// inclusion in the Load error message.
func formatDiagnostics(diags []parser.Diagnostic) string {
	parts := make([]string, 0, len(diags))
	for _, d := range diags {
		parts = append(parts, fmt.Sprintf("[%s] %s", d.Code, d.Message))
	}
	return strings.Join(parts, "; ")
}

// fomHandle is the core.FOMHandle implementation backed by *model.FOM.
//
// Lookup* accepts either an HLA-qualified name ("HLAobjectRoot.Vehicle")
// or a leaf name ("Vehicle"); cut-1 federates use the leaf form. The
// returned handles are derived from the position of the class in the
// model's name-sorted slice (1-based; matches the cut-1 test FOM
// fixtures' expectation that Vehicle = 1, Honk = 1).
type fomHandle struct {
	fom *model.FOM
}

func (h *fomHandle) IsValid() bool { return h != nil && h.fom != nil }

func (h *fomHandle) LookupObjectClass(name string) (core.ObjectClassHandle, bool) {
	if !h.IsValid() {
		return core.InvalidObjectClassHandle, false
	}
	target := leafName(name)
	for i, oc := range h.fom.ObjectClasses() {
		if oc.Name == target {
			return core.ObjectClassHandle(i + 1), true
		}
	}
	return core.InvalidObjectClassHandle, false
}

func (h *fomHandle) LookupInteractionClass(name string) (core.InteractionClassHandle, bool) {
	if !h.IsValid() {
		return core.InvalidInteractionClassHandle, false
	}
	target := leafName(name)
	for i, ic := range h.fom.InteractionClasses() {
		if ic.Name == target {
			return core.InteractionClassHandle(i + 1), true
		}
	}
	return core.InvalidInteractionClassHandle, false
}

// LookupAttribute resolves an attribute name on cls, walking the
// inheritance chain (M36 DC-4). Handle numbering stays backwards
// compatible: attributes declared directly on cls keep their 1-based
// positional handles; inherited attributes continue the numbering past
// the class's own attributes, nearest ancestor first. This is what
// makes the spec's implicit HLAprivilegeToDeleteObject (declared on
// the MIM's HLAobjectRoot) resolvable on every subclass.
//
// Name aliasing: "HLAprivilegeToDelete" (the HLA 1.3-lineage short
// form used by some federates/fixtures) resolves to the 1516-2010
// "HLAprivilegeToDeleteObject".
func (h *fomHandle) LookupAttribute(cls core.ObjectClassHandle, name string) (core.AttributeHandle, bool) {
	if !h.IsValid() {
		return core.InvalidAttributeHandle, false
	}
	classes := h.fom.ObjectClasses()
	idx := int(cls) - 1
	if idx < 0 || idx >= len(classes) {
		return core.InvalidAttributeHandle, false
	}
	target := canonicalAttributeName(name)
	offset := 0
	visited := map[int]bool{}
	for cur := idx; cur >= 0 && cur < len(classes) && !visited[cur]; {
		visited[cur] = true
		for i, a := range classes[cur].Attributes {
			if a.Name == target {
				return core.AttributeHandle(offset + i + 1), true
			}
		}
		offset += len(classes[cur].Attributes)
		cur = objectClassIndexByName(classes, classes[cur].ParentName)
	}
	return core.InvalidAttributeHandle, false
}

// canonicalAttributeName maps legacy attribute-name spellings onto the
// IEEE 1516-2010 canonical form. Currently only the privilege
// attribute needs this (M36 DC-4).
func canonicalAttributeName(name string) string {
	if name == "HLAprivilegeToDelete" {
		return "HLAprivilegeToDeleteObject"
	}
	return name
}

// objectClassIndexByName returns the slice index of the object class
// whose Name equals leafName(name), or -1 when name is empty or
// unknown. Used by the inheritance walk in LookupAttribute /
// AttributeName.
func objectClassIndexByName(classes []model.ObjectClass, name string) int {
	if name == "" {
		return -1
	}
	target := leafName(name)
	for i, oc := range classes {
		if oc.Name == target {
			return i
		}
	}
	return -1
}

func (h *fomHandle) LookupParameter(cls core.InteractionClassHandle, name string) (core.ParameterHandle, bool) {
	if !h.IsValid() {
		return core.InvalidParameterHandle, false
	}
	classes := h.fom.InteractionClasses()
	idx := int(cls) - 1
	if idx < 0 || idx >= len(classes) {
		return core.InvalidParameterHandle, false
	}
	for i, p := range classes[idx].Parameters {
		if p.Name == name {
			return core.ParameterHandle(i + 1), true
		}
	}
	return core.InvalidParameterHandle, false
}

// ObjectClassName implements core.FOMHandleNameLookup. M25 Phase B —
// reverse of LookupObjectClass.
func (h *fomHandle) ObjectClassName(cls core.ObjectClassHandle) (string, bool) {
	if !h.IsValid() {
		return "", false
	}
	classes := h.fom.ObjectClasses()
	idx := int(cls) - 1
	if idx < 0 || idx >= len(classes) {
		return "", false
	}
	return classes[idx].Name, true
}

// InteractionClassName implements core.FOMHandleNameLookup.
func (h *fomHandle) InteractionClassName(cls core.InteractionClassHandle) (string, bool) {
	if !h.IsValid() {
		return "", false
	}
	classes := h.fom.InteractionClasses()
	idx := int(cls) - 1
	if idx < 0 || idx >= len(classes) {
		return "", false
	}
	return classes[idx].Name, true
}

// AttributeName implements core.FOMHandleNameLookup. Mirrors
// LookupAttribute's inheritance walk (M36 DC-4): handles past the
// class's own attribute count fall through to the ancestor chain,
// nearest ancestor first.
func (h *fomHandle) AttributeName(cls core.ObjectClassHandle, attr core.AttributeHandle) (string, bool) {
	if !h.IsValid() {
		return "", false
	}
	classes := h.fom.ObjectClasses()
	cidx := int(cls) - 1
	if cidx < 0 || cidx >= len(classes) {
		return "", false
	}
	aidx := int(attr) - 1
	if aidx < 0 {
		return "", false
	}
	visited := map[int]bool{}
	for cur := cidx; cur >= 0 && cur < len(classes) && !visited[cur]; {
		visited[cur] = true
		attrs := classes[cur].Attributes
		if aidx < len(attrs) {
			return attrs[aidx].Name, true
		}
		aidx -= len(attrs)
		cur = objectClassIndexByName(classes, classes[cur].ParentName)
	}
	return "", false
}

// ParameterName implements core.FOMHandleNameLookup.
func (h *fomHandle) ParameterName(cls core.InteractionClassHandle, p core.ParameterHandle) (string, bool) {
	if !h.IsValid() {
		return "", false
	}
	classes := h.fom.InteractionClasses()
	cidx := int(cls) - 1
	if cidx < 0 || cidx >= len(classes) {
		return "", false
	}
	pidx := int(p) - 1
	params := classes[cidx].Parameters
	if pidx < 0 || pidx >= len(params) {
		return "", false
	}
	return params[pidx].Name, true
}

// LookupDimension implements core.FOMHandleNameLookup. Returns the
// 1-based position in the name-sorted dimension slice; 0 on miss.
func (h *fomHandle) LookupDimension(name string) (core.DimensionHandle, bool) {
	if !h.IsValid() {
		return core.InvalidDimensionHandle, false
	}
	for i, d := range h.fom.Dimensions() {
		if d.Name == name {
			return core.DimensionHandle(i + 1), true
		}
	}
	return core.InvalidDimensionHandle, false
}

// DimensionName implements core.FOMHandleNameLookup.
func (h *fomHandle) DimensionName(dh core.DimensionHandle) (string, bool) {
	if !h.IsValid() {
		return "", false
	}
	dims := h.fom.Dimensions()
	idx := int(dh) - 1
	if idx < 0 || idx >= len(dims) {
		return "", false
	}
	return dims[idx].Name, true
}

// DimensionUpperBound implements core.FOMHandleNameLookup.
func (h *fomHandle) DimensionUpperBound(dh core.DimensionHandle) (uint64, bool) {
	if !h.IsValid() {
		return 0, false
	}
	dims := h.fom.Dimensions()
	idx := int(dh) - 1
	if idx < 0 || idx >= len(dims) {
		return 0, false
	}
	return dims[idx].UpperBound, true
}

// OrderForAttribute returns the per-attribute delivery order ("TimeStamp"
// or "Receive") declared in the FOM. Implements
// transport/grpc.FOMOrderResolver so FOMRepoOrderLookup can drive the
// object registry's RO/TSO decision in best-effort mode (TASK-077).
//
// Lookup follows the same handle-from-1 indexing convention used by
// LookupObjectClass / LookupAttribute. Returns (OrderTimeStamp, false)
// for unknown classes/attributes; the FOMRepoOrderLookup adapter's
// "false" branch defaults to TSO, preserving the pre-best-effort
// behavior for unmapped attributes.
func (h *fomHandle) OrderForAttribute(cls core.ObjectClassHandle, attr core.AttributeHandle) (object.Order, bool) {
	if !h.IsValid() {
		return object.OrderTimeStamp, false
	}
	classes := h.fom.ObjectClasses()
	cidx := int(cls) - 1
	if cidx < 0 || cidx >= len(classes) {
		return object.OrderTimeStamp, false
	}
	aidx := int(attr) - 1
	attrs := classes[cidx].Attributes
	if aidx < 0 || aidx >= len(attrs) {
		return object.OrderTimeStamp, false
	}
	return orderFromString(attrs[aidx].Order), true
}

// OrderForInteraction returns the per-interaction declared delivery
// order. Implements transport/grpc.FOMOrderResolver. Same lookup +
// fallback semantics as OrderForAttribute.
func (h *fomHandle) OrderForInteraction(cls core.InteractionClassHandle) (object.Order, bool) {
	if !h.IsValid() {
		return object.OrderTimeStamp, false
	}
	classes := h.fom.InteractionClasses()
	idx := int(cls) - 1
	if idx < 0 || idx >= len(classes) {
		return object.OrderTimeStamp, false
	}
	return orderFromString(classes[idx].Order), true
}

// Dimensions implements ddm.DimensionEnumerator. The DDM manager
// consults this method (via type-assertion on the FOMHandle) to seed
// per-federation routing-space + dimension tables from the parsed FOM
// (M10 / FR-DDM-1).
func (h *fomHandle) Dimensions() []model.Dimension {
	if !h.IsValid() {
		return nil
	}
	return h.fom.Dimensions()
}

// orderFromString maps the FOM's declared order string to object.Order.
// "Receive" → OrderReceive; everything else (including "TimeStamp" and
// the empty string) → OrderTimeStamp. The TimeStamp default matches
// IEEE 1516-2010 §10.13's "TimeStamp is the FOM default when omitted".
func orderFromString(s string) object.Order {
	if strings.EqualFold(s, "Receive") {
		return object.OrderReceive
	}
	return object.OrderTimeStamp
}

// leafName returns the last dot-separated segment of name. "HLAobjectRoot.Vehicle"
// -> "Vehicle"; "Vehicle" -> "Vehicle".
func leafName(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// Compile-time assertion that fomRepository implements core.FOMRepository.
var _ core.FOMRepository = (*fomRepository)(nil)

// Compile-time assertion that *fomHandle satisfies the FOMOrderResolver
// contract consumed by transport/grpc.FOMRepoOrderLookup. If this drifts
// (e.g. a method signature changes), the build breaks here instead of
// silently falling back to the TSO default at runtime.
var _ grpcsvc.FOMOrderResolver = (*fomHandle)(nil)

// Compile-time assertion that *fomHandle satisfies ddm.DimensionEnumerator
// so the DDM manager can read routing-space declarations directly from
// the production FOM handle (M10 / FR-DDM-1).
var _ ddm.DimensionEnumerator = (*fomHandle)(nil)

// Compile-time assertion that *fomHandle satisfies the M25 Phase B
// reverse-lookup contract used by the SupportService.
var _ core.FOMHandleNameLookup = (*fomHandle)(nil)
