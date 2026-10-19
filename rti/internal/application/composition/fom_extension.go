package composition

import (
	"context"
	"fmt"
	"reflect"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// FOMHandleChange identifies an assigned handle that a positional reload
// would invalidate. Zero After denotes a missing binding in the merged view.
type FOMHandleChange struct {
	Kind   string
	Class  string
	Name   string
	Before uint64
	After  uint64
}

// FOMExtensionPreparation is an immutable, non-publishing analysis. It has no
// Commit method and is NOT admission for a live Join. Even with no handle
// changes, client tables, caches, journals and checkpoint identity need a
// coordinated update protocol. The model is a parsed preview, not live handles.
type FOMExtensionPreparation struct {
	base              *fomHandle
	merged            *fomHandle
	changes           []FOMHandleChange
	dimensionsChanged bool
}

func (p *FOMExtensionPreparation) BaseDigest() string      { return p.base.sha256 }
func (p *FOMExtensionPreparation) MergedDigest() string    { return p.merged.sha256 }
func (p *FOMExtensionPreparation) MergedModel() *model.FOM { return p.merged.fom }
func (p *FOMExtensionPreparation) HandleChanges() []FOMHandleChange {
	return append([]FOMHandleChange(nil), p.changes...)
}
func (p *FOMExtensionPreparation) RequiresDimensionRefresh() bool { return p.dimensionsChanged }

// PrepareExtension validates the exact creation modules plus incoming bytes
// without changing the repository, roster, counters or any consumer cache.
// Callers must not use a preview to publish live state. A later transaction
// would also need to revalidate BaseDigest under federation operation admission.
func (r *Repository) PrepareExtension(ctx context.Context, fed core.FederationName, additional []core.FOMModule) (*FOMExtensionPreparation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h, err := r.Get(ctx, fed)
	if err != nil {
		return nil, err
	}
	base, ok := h.(*fomHandle)
	if !ok || !base.IsValid() || !base.sourceAvailable {
		return nil, fmt.Errorf("FOM extension preparation requires retained source modules")
	}
	modules := append(cloneFOMModules(base.modules), cloneFOMModules(additional)...)
	loaded, err := r.load(ctx, modules, base.mimModule, true)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	merged := loaded.(*fomHandle)
	if err := validateDelayedSubscriptionExtension(base, merged); err != nil {
		return nil, err
	}
	// The current parser does not merge user datatype definitions across
	// modules. Reject conflicting modeled definitions instead of choosing one.
	known := make(map[string]model.DataType)
	for _, datatype := range merged.fom.DataTypes() {
		if previous, exists := known[datatype.Name()]; exists && !reflect.DeepEqual(previous, datatype) {
			return nil, fmt.Errorf("FOM extension has conflicting datatype %q", datatype.Name())
		}
		known[datatype.Name()] = datatype
	}
	return &FOMExtensionPreparation{base: base, merged: merged, changes: extensionHandleChanges(base, merged), dimensionsChanged: !reflect.DeepEqual(base.Dimensions(), merged.Dimensions())}, nil
}

func cloneFOMModules(modules []core.FOMModule) []core.FOMModule {
	cloned := make([]core.FOMModule, len(modules))
	for i, module := range modules {
		cloned[i] = core.FOMModule{Path: module.Path, XML: append([]byte(nil), module.XML...)}
	}
	return cloned
}

func extensionHandleChanges(base, merged *fomHandle) []FOMHandleChange {
	var changes []FOMHandleChange
	add := func(kind, class, name string, before, after uint64) {
		if before != after {
			changes = append(changes, FOMHandleChange{Kind: kind, Class: class, Name: name, Before: before, After: after})
		}
	}
	for _, class := range base.fom.ObjectClasses() {
		before, _ := base.LookupObjectClass(class.Key())
		after, _ := merged.LookupObjectClass(class.Key())
		add("objectClass", "", class.Key(), uint64(before), uint64(after))
		for _, attribute := range base.AttributeHandles(before) {
			name, _ := base.AttributeName(before, attribute)
			next, _ := merged.LookupAttribute(after, name)
			add("attribute", class.Key(), name, uint64(attribute), uint64(next))
		}
	}
	for _, class := range base.fom.InteractionClasses() {
		before, _ := base.LookupInteractionClass(class.Key())
		after, _ := merged.LookupInteractionClass(class.Key())
		add("interactionClass", "", class.Key(), uint64(before), uint64(after))
		for parameter := core.ParameterHandle(1); ; parameter++ {
			name, ok := base.ParameterName(before, parameter)
			if !ok {
				break
			}
			next, _ := merged.LookupParameter(after, name)
			add("parameter", class.Key(), name, uint64(parameter), uint64(next))
		}
	}
	for _, dimension := range base.Dimensions() {
		before, _ := base.LookupDimension(dimension.Name)
		after, _ := merged.LookupDimension(dimension.Name)
		add("dimension", "", dimension.Name, uint64(before), uint64(after))
	}
	return changes
}
