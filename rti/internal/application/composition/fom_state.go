package composition

import (
	"context"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// FOMRepositoryState preserves both exact source and history-dependent handle
// assignments. Re-parsing source alone is insufficient after a live extension.
type FOMRepositoryState = core.FOMRepositoryState

func (r *Repository) ExportState(ctx context.Context, fed core.FederationName) (*FOMRepositoryState, error) {
	view, err := r.FOMView(ctx, fed)
	if err != nil {
		return nil, err
	}
	return &FOMRepositoryState{Version: 1, View: view}, nil
}

// PrepareImportState parses and validates the entire snapshot without changing
// live state. The coordinator must lease this transaction before its durable
// restore boundary and publish all dependent services under operation admission.
func (r *Repository) PrepareImportState(ctx context.Context, fed core.FederationName, state *FOMRepositoryState) (core.FOMExtensionPreparation, error) {
	if state == nil || state.Version != 1 || state.View.Revision == 0 {
		return nil, fmt.Errorf("invalid FOM repository snapshot version or revision")
	}
	slot := r.slotFor(fed)
	r.mu.RLock()
	generation := slot.generation
	var baseRevision uint64
	if base, ok := r.byFedKey[fed].(*fomHandle); ok {
		baseRevision = base.revision
	}
	r.mu.RUnlock()
	loaded, err := r.load(ctx, state.View.Modules, state.View.MIMModule, true)
	if err != nil {
		return nil, err
	}
	candidate := loaded.(*fomHandle)
	if err := validateLiveDefinitions(candidate); err != nil {
		return nil, err
	}
	if candidate.sha256 != state.View.Digest {
		return nil, fmt.Errorf("FOM snapshot source digest mismatch")
	}
	bindings, err := restoreBindings(candidate.bindings, state.View)
	if err != nil {
		return nil, fmt.Errorf("FOM snapshot assignments: %w", err)
	}
	candidate.bindings = bindings
	candidate.revision = state.View.Revision
	return &LiveFOMExtension{repository: r, fed: fed, slot: slot, generation: generation, baseRevision: baseRevision, candidate: candidate}, nil
}

func restoreOrdered[T any](items []T, names []string, key func(T) string) ([]T, error) {
	if len(items) != len(names) {
		return nil, fmt.Errorf("binding count mismatch")
	}
	known := make(map[string]T, len(items))
	for _, item := range items {
		if _, exists := known[key(item)]; exists {
			return nil, fmt.Errorf("ambiguous declaration %q", key(item))
		}
		known[key(item)] = item
	}
	result := make([]T, 0, len(names))
	for _, name := range names {
		item, ok := known[name]
		if !ok {
			return nil, fmt.Errorf("missing or repeated binding %q", name)
		}
		delete(known, name)
		result = append(result, item)
	}
	return result, nil
}

func restoreBindings(parsed *fomBindings, view core.FOMView) (*fomBindings, error) {
	b := &fomBindings{advisoryDefaults: parsed.advisoryDefaults}
	if err := b.restoreObjects(parsed, view); err != nil {
		return nil, err
	}
	if err := b.restoreInteractions(parsed, view); err != nil {
		return nil, err
	}
	if err := b.restoreDimensions(parsed, view); err != nil {
		return nil, err
	}
	b.indexClasses()
	return b, nil
}

func (b *fomBindings) restoreObjects(parsed *fomBindings, view core.FOMView) error {
	var err error
	names := make([]string, len(view.ObjectClasses))
	for i, c := range view.ObjectClasses {
		if c.Handle != core.ObjectClassHandle(i)+1 { //nolint:gosec // G115: a slice index is nonnegative and fits the uint64 handle.
			return fmt.Errorf("noncanonical object class handle %d", c.Handle)
		}
		names[i] = c.Name
	}
	b.objects, err = restoreOrdered(parsed.objects, names, func(c model.ObjectClass) string { return c.Key() })
	if err != nil {
		return err
	}
	for _, c := range view.ObjectClasses {
		idx := objectClassIndexByName(parsed.objects, c.Name)
		names = make([]string, len(c.Attributes))
		for i, a := range c.Attributes {
			if a.Handle != core.AttributeHandle(i)+1 { //nolint:gosec // G115: a slice index is nonnegative and fits the uint64 handle.
				return fmt.Errorf("noncanonical attribute handle %d", a.Handle)
			}
			// Version-1 snapshots may use the original Annex G spelling.
			names[i] = mim.CanonicalAttributeName(c.Name, a.Name)
		}
		members, err := restoreOrdered(parsed.attributes[idx], names, func(a model.Attribute) string { return a.Name })
		if err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
		b.attributes = append(b.attributes, members)
	}
	return nil
}

func (b *fomBindings) restoreInteractions(parsed *fomBindings, view core.FOMView) error {
	var err error
	names := make([]string, len(view.InteractionClasses))
	for i, c := range view.InteractionClasses {
		if c.Handle != core.InteractionClassHandle(i)+1 { //nolint:gosec // G115: a slice index is nonnegative and fits the uint64 handle.
			return fmt.Errorf("noncanonical interaction class handle %d", c.Handle)
		}
		names[i] = c.Name
	}
	b.interactions, err = restoreOrdered(parsed.interactions, names, func(c model.InteractionClass) string { return c.Key() })
	if err != nil {
		return err
	}
	for _, c := range view.InteractionClasses {
		idx := interactionClassIndexByName(parsed.interactions, c.Name)
		names = make([]string, len(c.Parameters))
		for i, p := range c.Parameters {
			if p.Handle != core.ParameterHandle(i)+1 { //nolint:gosec // G115: a slice index is nonnegative and fits the uint64 handle.
				return fmt.Errorf("noncanonical parameter handle %d", p.Handle)
			}
			names[i] = p.Name
		}
		members, err := restoreOrdered(parsed.parameters[idx], names, func(p model.Parameter) string { return p.Name })
		if err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
		b.parameters = append(b.parameters, members)
	}
	return nil
}

func (b *fomBindings) restoreDimensions(parsed *fomBindings, view core.FOMView) error {
	var err error
	names := make([]string, len(view.Dimensions))
	for i, d := range view.Dimensions {
		if d.Handle != core.DimensionHandle(i)+1 { //nolint:gosec // G115: a slice index is nonnegative and fits the uint64 handle.
			return fmt.Errorf("noncanonical dimension handle %d", d.Handle)
		}
		names[i] = d.Name
	}
	b.dimensions, err = restoreOrdered(parsed.dimensions, names, func(d model.Dimension) string { return d.Name })
	if err != nil {
		return err
	}
	for i, d := range b.dimensions {
		if d.UpperBound != view.Dimensions[i].UpperBound {
			return fmt.Errorf("dimension %s upper bound mismatch", d.Name)
		}
	}
	return nil
}
