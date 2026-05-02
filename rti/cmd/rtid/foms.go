package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
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

func (h *fomHandle) LookupAttribute(cls core.ObjectClassHandle, name string) (core.AttributeHandle, bool) {
	if !h.IsValid() {
		return core.InvalidAttributeHandle, false
	}
	classes := h.fom.ObjectClasses()
	idx := int(cls) - 1
	if idx < 0 || idx >= len(classes) {
		return core.InvalidAttributeHandle, false
	}
	for i, a := range classes[idx].Attributes {
		if a.Name == name {
			return core.AttributeHandle(i + 1), true
		}
	}
	return core.InvalidAttributeHandle, false
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
