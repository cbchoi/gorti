package composition

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

var ErrFOMExtensionStale = errors.New("FOM extension base changed")
var ErrFOMExtensionReleased = errors.New("FOM extension was released")

type fomRepositorySlot struct {
	write      sync.Mutex
	generation uint64 // Protected by Repository.mu.
	reference  fomReference
}

func (r *Repository) slotFor(fed core.FederationName) *fomRepositorySlot {
	r.mu.Lock()
	defer r.mu.Unlock()
	slot := r.slots[fed]
	if slot == nil {
		slot = &fomRepositorySlot{}
		r.slots[fed] = slot
	}
	return slot
}

// LiveFOMExtension owns an unpublished immutable view and, after PrepareCommit,
// a per-federation writer lease. No repository read lock spans user work.
type LiveFOMExtension struct {
	mu           sync.Mutex
	repository   *Repository
	fed          core.FederationName
	slot         *fomRepositorySlot
	generation   uint64
	baseRevision uint64
	candidate    *fomHandle
	leased       bool
	committed    bool
	released     bool
}

func (p *LiveFOMExtension) FOM() core.FOMHandle       { return p.candidate }
func (p *LiveFOMExtension) Candidate() core.FOMHandle { return p.candidate }
func (p *LiveFOMExtension) Modules() []core.FOMModule { return cloneFOMModules(p.candidate.modules) }
func (p *LiveFOMExtension) Revision() uint64          { return p.candidate.revision }
func (p *LiveFOMExtension) BaseRevision() uint64      { return p.baseRevision }
func (p *LiveFOMExtension) View() core.FOMView        { return p.candidate.View() }
func (h *fomHandle) FOMRevision() uint64              { return h.revision }

func (r *Repository) PrepareLiveExtension(ctx context.Context, fed core.FederationName, additional []core.FOMModule) (core.FOMExtensionPreparation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slot := r.slotFor(fed)
	r.mu.RLock()
	base, ok := r.byFedKey[fed].(*fomHandle)
	generation := slot.generation
	r.mu.RUnlock()
	if !ok {
		return nil, core.ErrFederationNotFound
	}
	if !base.sourceAvailable {
		return nil, fmt.Errorf("live FOM extension requires retained source modules")
	}
	if len(additional) == 0 {
		return &LiveFOMExtension{repository: r, fed: fed, slot: slot, generation: generation, baseRevision: base.revision, candidate: base}, nil
	}
	// Parse against the captured immutable base, not a second live lookup.
	loaded, err := r.load(ctx, append(cloneFOMModules(base.modules), cloneFOMModules(additional)...), base.mimModule, true)
	if err != nil {
		return nil, err
	}
	if base.revision == ^uint64(0) {
		return nil, fmt.Errorf("FOM revision exhausted")
	}
	candidate := loaded.(*fomHandle)
	if err := validateDelayedSubscriptionExtension(base, candidate); err != nil {
		return nil, err
	}
	if err := validateLiveDefinitions(candidate); err != nil {
		return nil, err
	}
	candidate.bindings, err = stableBindings(base, candidate)
	if err != nil {
		return nil, err
	}
	candidate.revision = base.revision + 1
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &LiveFOMExtension{repository: r, fed: fed, slot: slot, generation: generation, baseRevision: base.revision, candidate: candidate}, nil
}

func validateLiveDefinitions(h *fomHandle) error {
	known := make(map[string]model.DataType)
	for _, datatype := range h.fom.DataTypes() {
		if old, exists := known[datatype.Name()]; exists && !reflect.DeepEqual(old, datatype) {
			return fmt.Errorf("FOM extension has conflicting datatype %q", datatype.Name())
		}
		known[datatype.Name()] = datatype
	}
	return nil
}

func (p *LiveFOMExtension) PrepareCommit() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.released {
		return ErrFOMExtensionReleased
	}
	if p.committed || p.leased {
		return nil
	}
	p.slot.write.Lock()
	p.repository.mu.RLock()
	current := p.slot.generation
	p.repository.mu.RUnlock()
	if current != p.generation {
		p.slot.write.Unlock()
		return ErrFOMExtensionStale
	}
	p.leased = true
	return nil
}

func (p *LiveFOMExtension) Commit() error {
	if err := p.PrepareCommit(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.released {
		return ErrFOMExtensionReleased
	}
	if p.committed {
		return nil
	}
	p.repository.mu.Lock()
	p.repository.byFedKey[p.fed] = p.candidate
	p.slot.reference.publish(p.candidate)
	p.slot.generation++
	p.repository.mu.Unlock()
	p.committed = true
	p.leased = false
	p.slot.write.Unlock()
	return nil
}

func (p *LiveFOMExtension) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.committed || p.released {
		return
	}
	p.released = true
	if p.leased {
		p.leased = false
		p.slot.write.Unlock()
	}
}

func (r *Repository) FOMView(ctx context.Context, fed core.FederationName) (core.FOMView, error) {
	h, err := r.Get(ctx, fed)
	if err != nil {
		return core.FOMView{}, err
	}
	view, ok := h.(interface{ View() core.FOMView })
	if !ok {
		return core.FOMView{}, fmt.Errorf("FOM handle has no authoritative view")
	}
	return view.View(), nil
}

func (h *fomHandle) View() core.FOMView {
	v := core.FOMView{Revision: h.revision, Digest: h.sha256, Modules: cloneFOMModules(h.modules), MIMModule: cloneMIMModule(h.mimModule)}
	for _, cls := range h.bindings.objects {
		handle, _ := h.LookupObjectClass(cls.Key())
		binding := core.FOMObjectClassBinding{Handle: handle, Name: cls.Key()}
		for _, attr := range h.AttributeHandles(handle) {
			name, _ := h.AttributeName(handle, attr)
			binding.Attributes = append(binding.Attributes, core.FOMAttributeBinding{Handle: attr, Name: name})
		}
		v.ObjectClasses = append(v.ObjectClasses, binding)
	}
	for _, cls := range h.bindings.interactions {
		handle, _ := h.LookupInteractionClass(cls.Key())
		binding := core.FOMInteractionClassBinding{Handle: handle, Name: cls.Key()}
		for p := core.ParameterHandle(1); ; p++ {
			name, ok := h.ParameterName(handle, p)
			if !ok {
				break
			}
			binding.Parameters = append(binding.Parameters, core.FOMParameterBinding{Handle: p, Name: name})
		}
		v.InteractionClasses = append(v.InteractionClasses, binding)
	}
	for _, dimension := range h.Dimensions() {
		handle, _ := h.LookupDimension(dimension.Name)
		v.Dimensions = append(v.Dimensions, core.FOMDimensionBinding{Handle: handle, Name: dimension.Name, UpperBound: dimension.UpperBound})
	}
	return v
}

var _ core.FOMExtensionPreparation = (*LiveFOMExtension)(nil)
