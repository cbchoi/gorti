package object

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var ErrStaleObjectKnowledge = errors.New("object callback knowledge is stale")

// KnowledgeEpoch identifies one continuous recipient/object knowledge lifetime.
// Validation is lock-free: delivery may check it while holding queue locks.
// Epochs are process-local capabilities, not handles or snapshot coordinates.
type KnowledgeEpoch struct {
	identity *atomic.Bool
	retired  *atomic.Bool
}

func (e KnowledgeEpoch) Validate() error {
	if e.identity == nil || e.retired == nil || e.identity.Load() || e.retired.Load() {
		return ErrStaleObjectKnowledge
	}
	return nil
}

// KnownAttributeProjection owns both coordinate sets and their knowledge epoch.
// It detects invalidation; it does not reserve callback delivery or acknowledge
// a terminal ownership notification. Copies share only the immutable snapshot.
type KnownAttributeProjection struct {
	class      core.ObjectClassHandle
	registered []core.AttributeHandle
	known      []core.AttributeHandle
	epoch      KnowledgeEpoch
}

func (p KnownAttributeProjection) KnownClass() core.ObjectClassHandle { return p.class }
func (p KnownAttributeProjection) RegisteredAttributes() []core.AttributeHandle {
	return slices.Clone(p.registered)
}
func (p KnownAttributeProjection) Attributes() []core.AttributeHandle { return slices.Clone(p.known) }
func (p KnownAttributeProjection) Epoch() KnowledgeEpoch              { return p.epoch }
func (p KnownAttributeProjection) Validate() error                    { return p.epoch.Validate() }

// CaptureKnownObjectAttributes maps and captures identity under one object lock.
// No lease is retained across outbox admission. A consumer must validate again
// at dispatch and preserve terminal notifications until actual receipt.
func (r *RegistryOf[T]) CaptureKnownObjectAttributes(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (KnownAttributeProjection, error) {
	if err := ctx.Err(); err != nil {
		return KnownAttributeProjection{}, err
	}
	st := r.stateFor(fed)
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return KnownAttributeProjection{}, err
	}
	inst, err := r.transportationObject(st, fed, recipient, obj, attrs)
	if err != nil {
		return KnownAttributeProjection{}, err
	}
	mapped, err := r.actualAttributesAtKnownClass(ctx, fed, st, recipient, inst, attrs)
	if err != nil {
		return KnownAttributeProjection{}, err
	}
	if err := ctx.Err(); err != nil {
		return KnownAttributeProjection{}, err
	}
	epoch := st.knowledgeEpochLocked(discoverKey{sub: recipient, obj: obj})
	if err := epoch.Validate(); err != nil {
		return KnownAttributeProjection{}, err
	}
	return KnownAttributeProjection{class: st.knownClass(recipient, inst), registered: slices.Clone(attrs), known: mapped, epoch: epoch}, nil
}

func (st *federationStateOf[T]) knowledgeEpochLocked(key discoverKey) KnowledgeEpoch {
	if st.knowledgeEpochs == nil {
		st.knowledgeEpochs = make(map[discoverKey]*atomic.Bool)
	}
	identity := st.knowledgeEpochs[key]
	if identity == nil {
		identity = new(atomic.Bool)
		st.knowledgeEpochs[key] = identity
	}
	return KnowledgeEpoch{identity: identity, retired: &st.knowledgeRetired}
}

// Forget before replacing knowledge, even when rediscovery conveys the same
// class. An old projection must never become valid again through an ABA cycle.
func (st *federationStateOf[T]) forgetKnowledgeLocked(key discoverKey) {
	if identity := st.knowledgeEpochs[key]; identity != nil {
		identity.Store(true)
		delete(st.knowledgeEpochs, key)
	}
	delete(st.discovered, key)
}
