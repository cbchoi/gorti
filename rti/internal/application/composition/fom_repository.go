package composition

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	grpcsvc "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

// Repository adapts rti/pkg/fom into core.FOMRepository for the rtid
// composition. Load parses the supplied modules + applies MIM merge;
// Lookup by HLA-qualified name uses immutable assigned-handle indexes.
// Per-federation handles are remembered via RememberFor
// (called by the federation manager wiring at CreateFederation success).
type Repository struct {
	mu       sync.RWMutex
	byFedKey map[core.FederationName]core.FOMHandle
	slots    map[core.FederationName]*fomRepositorySlot
}

const standardMIMDesignator = "HLAstandardMIM"

// NewFOMRepository constructs an empty repository.
func NewFOMRepository() *Repository {
	return &Repository{
		byFedKey: map[core.FederationName]core.FOMHandle{},
		slots:    map[core.FederationName]*fomRepositorySlot{},
	}
}

// Load implements core.FOMRepository.
//
// An empty module list is a product extension retained for existing Go/Python
// callers. It still includes the standard MIM. Standard API creation must
// supply at least one FOM module at its admission boundary.
func (r *Repository) Load(ctx context.Context, modules []core.FOMModule) (core.FOMHandle, error) {
	return r.load(ctx, modules, nil, false)
}

func (r *Repository) LoadWithMIM(ctx context.Context, modules []core.FOMModule, selected *core.FOMModule) (core.FOMHandle, error) {
	return r.load(ctx, modules, selected, false)
}

func (r *Repository) load(ctx context.Context, modules []core.FOMModule, selected *core.FOMModule, additive bool) (core.FOMHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	modules = cloneFOMModules(modules)
	selected = cloneMIMModule(selected)
	if selected != nil && strings.TrimSpace(selected.Path) == "" {
		return nil, fmt.Errorf("rtid: explicit MIM requires a nonempty designator")
	}
	if selected != nil && selected.Path == standardMIMDesignator {
		return nil, fmt.Errorf("rtid: explicit MIM designator must not be HLAstandardMIM")
	}
	digest := DigestFOMSources(modules, selected)
	fm, err := parseFOMModules(modules, additive)
	if err != nil {
		return nil, err
	}
	mimFOM, mimErr := mim.StandardMIMHandle()
	if selected != nil {
		mimFOM, mimErr = mim.ParseSelected(selected.XML)
	}
	if mimErr != nil {
		return nil, fmt.Errorf("rtid: MIM validation: %w", mimErr)
	}
	merged, diags := mim.Merge(mimFOM, fm)
	if len(diags) > 0 {
		return nil, fmt.Errorf("rtid: MIM merge: %s", diags[0].Code+" "+diags[0].Message)
	}
	return &fomHandle{fom: merged, bindings: initialBindings(merged), sha256: digest, modules: modules, mimModule: selected, sourceAvailable: true, revision: 1}, nil
}

func parseFOMModules(modules []core.FOMModule, additive bool) (*model.FOM, error) {
	parserModules := make([]parser.Module, len(modules))
	for i, m := range modules {
		parserModules[i] = parser.Module{Path: m.Path, XML: m.XML}
	}
	parse := parser.Parse
	if additive {
		parse = parser.ParseAdditive
	}
	res, err := parse(parserModules)
	if err != nil {
		return nil, fmt.Errorf("rtid: %w: parse FOM: %w", core.ErrFOMInvalidModule, err)
	}
	if len(res.Diagnostics) > 0 {
		for _, diagnostic := range res.Diagnostics {
			if diagnostic.Code == "FOM-017" {
				return nil, fmt.Errorf("rtid: %w: %s", core.ErrFOMInvalidName, formatDiagnostics(res.Diagnostics))
			}
		}
		return nil, fmt.Errorf("rtid: %w: FOM validation failed: %s", core.ErrFOMInvalidModule, formatDiagnostics(res.Diagnostics))
	}
	fm, ok := res.FOM.(*model.FOM)
	if !ok {
		return nil, fmt.Errorf("rtid: parser returned unexpected FOM type %T", res.FOM)
	}
	return fm, nil
}

// DigestFOMSources preserves the historical omitted-MIM digest. Explicit MIM
// source uses a distinct domain and length-framed role, not another FOM module.
func DigestFOMSources(modules []core.FOMModule, selected *core.FOMModule) string {
	if selected == nil {
		return DigestFOMModules(modules)
	}
	h := sha256.New()
	_, _ = h.Write([]byte("gorti:FOM-sources:v1\x00MIM\x00"))
	var framed [8]byte
	binary.LittleEndian.PutUint64(framed[:], uint64(len(selected.XML)))
	_, _ = h.Write(framed[:])
	_, _ = h.Write(selected.XML)
	_, _ = h.Write([]byte("\x00FOM\x00" + DigestFOMModules(modules)))
	return hex.EncodeToString(h.Sum(nil))
}

func cloneMIMModule(module *core.FOMModule) *core.FOMModule {
	if module == nil {
		return nil
	}
	return &core.FOMModule{Path: module.Path, XML: append([]byte(nil), module.XML...)}
}

// MIMDesignator reports the supplied path without reading it from the server.
// Omission preserves the historical standard label without a filename suffix.
func (r *Repository) MIMDesignator(ctx context.Context, fed core.FederationName) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	handle, err := r.Get(ctx, fed)
	if err != nil {
		return "", err
	}
	h, ok := handle.(*fomHandle)
	if !ok {
		return "", fmt.Errorf("MIM designator unavailable")
	}
	if h.mimModule != nil && h.mimModule.Path != "" {
		return h.mimModule.Path, nil
	}
	return standardMIMDesignator, nil
}

// DigestFOMModules fingerprints the exact ordered XML byte slices. Length
// framing keeps module boundaries unambiguous while deliberately excluding
// diagnostic filesystem paths from compatibility identity.
func DigestFOMModules(modules []core.FOMModule) string {
	h := sha256.New()
	var framed [8]byte
	binary.LittleEndian.PutUint64(framed[:], uint64(len(modules)))
	_, _ = h.Write(framed[:])
	for _, module := range modules {
		binary.LittleEndian.PutUint64(framed[:], uint64(len(module.XML)))
		_, _ = h.Write(framed[:])
		_, _ = h.Write(module.XML)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Get implements core.FOMRepository. Returns ErrFederationNotFound if no
// RememberFor was issued for fed.
func (r *Repository) Get(_ context.Context, fed core.FederationName) (core.FOMHandle, error) {
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
func (r *Repository) RememberFor(fed core.FederationName, h core.FOMHandle) {
	slot := r.slotFor(fed)
	slot.write.Lock()
	defer slot.write.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byFedKey[fed] = h
	slot.reference.publish(h)
	slot.generation++
}

func (r *Repository) ForgetFor(fed core.FederationName) {
	slot := r.slotFor(fed)
	slot.write.Lock()
	defer slot.write.Unlock()
	r.mu.Lock()
	delete(r.byFedKey, fed)
	slot.reference.publish(nil)
	slot.generation++
	r.mu.Unlock()
}

func (r *Repository) DigestFor(fed core.FederationName) (string, bool) {
	r.mu.RLock()
	h, ok := r.byFedKey[fed]
	r.mu.RUnlock()
	if !ok {
		return "", false
	}
	digest, ok := h.(interface{ FOMSHA256() string })
	if !ok || digest.FOMSHA256() == "" {
		return "", false
	}
	return digest.FOMSHA256(), true
}

// formatDiagnostics builds a one-line summary of parser diagnostics for
// inclusion in the Load error message.
func formatDiagnostics(diags []parser.Diagnostic) string {
	parts := make([]string, 0, len(diags))
	for _, d := range diags {
		if d.ModulePath != "" {
			parts = append(parts, fmt.Sprintf("[%s] %s: %s", d.Code, d.ModulePath, d.Message))
		} else {
			parts = append(parts, fmt.Sprintf("[%s] %s", d.Code, d.Message))
		}
	}
	return strings.Join(parts, "; ")
}

// fomHandle is the core.FOMHandle implementation backed by *model.FOM.
//
// Lookup* accepts either an HLA-qualified name ("HLAobjectRoot.Vehicle")
// or a leaf name ("Vehicle"); cut-1 federates use the leaf form. The
// initial handles follow the model's sorted declarations. Live extensions use
// immutable append-only binding tables, independent of subsequent model order.
type fomHandle struct {
	fom             *model.FOM
	sha256          string
	modules         []core.FOMModule
	mimModule       *core.FOMModule
	sourceAvailable bool
	revision        uint64
	bindings        *fomBindings
}

// NewModelHandle adapts an already parsed FOM model to the HLA core handle
// contract. Application composers that build models programmatically can use
// this without depending on the repository's parsing path.
func NewModelHandle(fom *model.FOM) core.FOMHandle {
	return &fomHandle{fom: fom, bindings: initialBindings(fom), revision: 1}
}

func (h *fomHandle) FOMSHA256() string {
	if h == nil {
		return ""
	}
	return h.sha256
}

func (h *fomHandle) SelectedMIM() *core.FOMModule { return cloneMIMModule(h.mimModule) }

func (h *fomHandle) IsValid() bool { return h != nil && h.fom != nil }

func (h *fomHandle) LookupObjectClass(name string) (core.ObjectClassHandle, bool) {
	if !h.IsValid() {
		return core.InvalidObjectClassHandle, false
	}
	if i := h.bindings.objectNames.lookup(name); i >= 0 {
		return core.ObjectClassHandle(i) + 1, true
	}
	return core.InvalidObjectClassHandle, false
}

func (h *fomHandle) LookupInteractionClass(name string) (core.InteractionClassHandle, bool) {
	if !h.IsValid() {
		return core.InvalidInteractionClassHandle, false
	}
	if i := h.bindings.interactionNames.lookup(name); i >= 0 {
		return core.InteractionClassHandle(i) + 1, true
	}
	return core.InvalidInteractionClassHandle, false
}

// LookupAttribute resolves cls's effective immutable member table. Initial
// assignment places local declarations before inherited declarations. Later
// additions, including ancestor additions, append without moving old handles.
//
// Name aliasing: "HLAprivilegeToDelete" (the HLA 1.3-lineage short
// form used by some federates/fixtures) resolves to the 1516-2010
// "HLAprivilegeToDeleteObject".
func (h *fomHandle) LookupAttribute(cls core.ObjectClassHandle, name string) (core.AttributeHandle, bool) {
	target := canonicalAttributeName(name)
	if h.IsValid() && cls > 0 && uint64(cls) <= uint64(len(h.bindings.objects)) {
		target = mim.CanonicalAttributeName(h.bindings.objects[cls-1].Key(), target)
	}
	for i, a := range h.effectiveAttributes(cls) {
		if a.Name == target {
			return core.AttributeHandle(i) + 1, true //nolint:gosec // G115: a slice index is nonnegative and fits the uint64 handle.
		}
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

// Class lookup accepts full paths, root-omitted paths, and unambiguous local
// names. An incorrect qualified path must never fall back to a local name.
func objectClassIndexByName(classes []model.ObjectClass, name string) int {
	if name == "" {
		return -1
	}
	match := -1
	for i, oc := range classes {
		if classNameMatches(name, oc.Name, oc.Key(), "HLAobjectRoot") {
			if match >= 0 {
				return -1
			}
			match = i
		}
	}
	return match
}

func interactionClassIndexByName(classes []model.InteractionClass, name string) int {
	if name == "" {
		return -1
	}
	match := -1
	for i, ic := range classes {
		if classNameMatches(name, ic.Name, ic.Key(), "HLAinteractionRoot") {
			if match >= 0 {
				return -1
			}
			match = i
		}
	}
	return match
}

func classNameMatches(query, local, key, root string) bool {
	if !strings.Contains(query, ".") {
		return query == local || query == key
	}
	return strings.TrimPrefix(query, root+".") == strings.TrimPrefix(key, root+".")
}

func (h *fomHandle) LookupParameter(cls core.InteractionClassHandle, name string) (core.ParameterHandle, bool) {
	for i, p := range h.effectiveParameters(cls) {
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
	classes := h.bindings.objects
	idx := int(cls) - 1
	if idx < 0 || idx >= len(classes) {
		return "", false
	}
	return classes[idx].Key(), true
}

// InteractionClassName implements core.FOMHandleNameLookup.
func (h *fomHandle) InteractionClassName(cls core.InteractionClassHandle) (string, bool) {
	if !h.IsValid() {
		return "", false
	}
	classes := h.bindings.interactions
	idx := int(cls) - 1
	if idx < 0 || idx >= len(classes) {
		return "", false
	}
	return classes[idx].Key(), true
}

// AttributeName implements core.FOMHandleNameLookup using the same assigned
// effective-member table as LookupAttribute.
func (h *fomHandle) AttributeName(cls core.ObjectClassHandle, attr core.AttributeHandle) (string, bool) {
	declaration, ok := h.attributeDeclaration(cls, attr)
	return declaration.Name, ok
}

// ParameterName implements core.FOMHandleNameLookup.
func (h *fomHandle) ParameterName(cls core.InteractionClassHandle, p core.ParameterHandle) (string, bool) {
	parameters := h.effectiveParameters(cls)
	if p == 0 || uint64(p) > uint64(len(parameters)) {
		return "", false
	}
	return parameters[p-1].Name, true
}

// LookupDimension implements core.FOMHandleNameLookup using stable assigned
// dimension order, which need not remain alphabetical after extension.
func (h *fomHandle) LookupDimension(name string) (core.DimensionHandle, bool) {
	if !h.IsValid() {
		return core.InvalidDimensionHandle, false
	}
	for i, d := range h.bindings.dimensions {
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
	dims := h.bindings.dimensions
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
	dims := h.bindings.dimensions
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
	declaration, ok := h.attributeDeclaration(cls, attr)
	if !ok {
		return object.OrderTimeStamp, false
	}
	return orderFromString(declaration.Order), true
}

// OrderForInteraction returns the per-interaction declared delivery
// order. Implements transport/grpc.FOMOrderResolver. Same lookup +
// fallback semantics as OrderForAttribute.
func (h *fomHandle) OrderForInteraction(cls core.InteractionClassHandle) (object.Order, bool) {
	if !h.IsValid() {
		return object.OrderTimeStamp, false
	}
	classes := h.bindings.interactions
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
	return append([]model.Dimension(nil), h.bindings.dimensions...)
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

// Compile-time assertion that Repository implements core.FOMRepository.
var _ core.FOMRepository = (*Repository)(nil)

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
