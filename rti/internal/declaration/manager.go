package declaration

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrNotImplemented is retained for callers that may still match on it during
// the M2 transition. Implemented methods never return it.
var ErrNotImplemented = errors.New("declaration: not implemented (Agent A M2 deliverable)")

// objAttrKey identifies a (class, attribute) pair within a federation.
type objAttrKey struct {
	cls  core.ObjectClassHandle
	attr core.AttributeHandle
}

// federationState holds the four pub/sub matrices for one federation.
//
// Sets of federate handles are stored as map[FederateHandle]struct{} so that
// idempotent Publish/Subscribe calls collapse naturally. All observable
// iteration sorts the keys before returning.
type federationState struct {
	objPubs map[objAttrKey]map[core.FederateHandle]struct{}
	objSubs map[objAttrKey]map[core.FederateHandle]struct{}
	intPubs map[core.InteractionClassHandle]map[core.FederateHandle]struct{}
	intSubs map[core.InteractionClassHandle]map[core.FederateHandle]struct{}
}

func newFederationState() *federationState {
	return &federationState{
		objPubs: map[objAttrKey]map[core.FederateHandle]struct{}{},
		objSubs: map[objAttrKey]map[core.FederateHandle]struct{}{},
		intPubs: map[core.InteractionClassHandle]map[core.FederateHandle]struct{}{},
		intSubs: map[core.InteractionClassHandle]map[core.FederateHandle]struct{}{},
	}
}

// Manager holds per-federation pub/sub matrices and answers
// SubscribersFor / PublishersFor queries in deterministic handle order.
type Manager struct {
	mu  sync.RWMutex
	fed map[core.FederationName]*federationState
}

// Compile-time assertion: *Manager satisfies core.DeclarationManagement.
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.6) introduced the interface; production keeps using *Manager. The
// "no abstraction layer" stance from the cut-2 docs/idd.md §3 note has
// been revised in the same Phase 1 commit (research reachability over
// purity).
var _ core.DeclarationManagement = (*Manager)(nil)

// New constructs a Manager. No external dependencies — Manager is pure.
func New() *Manager {
	return &Manager{
		fed: map[core.FederationName]*federationState{},
	}
}

// stateLocked returns the federationState for fed, creating it if missing.
// Caller MUST hold m.mu (write lock).
func (m *Manager) stateLocked(fed core.FederationName) *federationState {
	st, ok := m.fed[fed]
	if !ok {
		st = newFederationState()
		m.fed[fed] = st
	}
	return st
}

// PublishObjectClassAttributes records that federate `pub` publishes the
// given attributes of `cls` in `fed`. Idempotent — repeated calls do not
// duplicate publications.
func (m *Manager) PublishObjectClassAttributes(
	_ context.Context,
	fed core.FederationName,
	pub core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateLocked(fed)
	for _, a := range attrs {
		k := objAttrKey{cls: cls, attr: a}
		set, ok := st.objPubs[k]
		if !ok {
			set = map[core.FederateHandle]struct{}{}
			st.objPubs[k] = set
		}
		set[pub] = struct{}{}
	}
	return nil
}

// UnpublishObjectClassAttributes removes federate `pub`'s publication of
// the listed attributes. Removing a non-existent publication is a no-op.
func (m *Manager) UnpublishObjectClassAttributes(
	_ context.Context,
	fed core.FederationName,
	pub core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil
	}
	for _, a := range attrs {
		k := objAttrKey{cls: cls, attr: a}
		if set, ok := st.objPubs[k]; ok {
			delete(set, pub)
			if len(set) == 0 {
				delete(st.objPubs, k)
			}
		}
	}
	return nil
}

// SubscribeObjectClassAttributes records federate `sub`'s subscription.
func (m *Manager) SubscribeObjectClassAttributes(
	_ context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateLocked(fed)
	for _, a := range attrs {
		k := objAttrKey{cls: cls, attr: a}
		set, ok := st.objSubs[k]
		if !ok {
			set = map[core.FederateHandle]struct{}{}
			st.objSubs[k] = set
		}
		set[sub] = struct{}{}
	}
	return nil
}

// UnsubscribeObjectClassAttributes is the symmetric remover.
func (m *Manager) UnsubscribeObjectClassAttributes(
	_ context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil
	}
	for _, a := range attrs {
		k := objAttrKey{cls: cls, attr: a}
		if set, ok := st.objSubs[k]; ok {
			delete(set, sub)
			if len(set) == 0 {
				delete(st.objSubs, k)
			}
		}
	}
	return nil
}

// PublishInteractionClass records federate `pub` publishes interactions
// of `cls`.
func (m *Manager) PublishInteractionClass(
	_ context.Context,
	fed core.FederationName,
	pub core.FederateHandle,
	cls core.InteractionClassHandle,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateLocked(fed)
	set, ok := st.intPubs[cls]
	if !ok {
		set = map[core.FederateHandle]struct{}{}
		st.intPubs[cls] = set
	}
	set[pub] = struct{}{}
	return nil
}

// UnpublishInteractionClass removes the publication.
func (m *Manager) UnpublishInteractionClass(
	_ context.Context,
	fed core.FederationName,
	pub core.FederateHandle,
	cls core.InteractionClassHandle,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil
	}
	if set, ok := st.intPubs[cls]; ok {
		delete(set, pub)
		if len(set) == 0 {
			delete(st.intPubs, cls)
		}
	}
	return nil
}

// SubscribeInteractionClass records federate `sub`'s subscription.
func (m *Manager) SubscribeInteractionClass(
	_ context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	cls core.InteractionClassHandle,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateLocked(fed)
	set, ok := st.intSubs[cls]
	if !ok {
		set = map[core.FederateHandle]struct{}{}
		st.intSubs[cls] = set
	}
	set[sub] = struct{}{}
	return nil
}

// UnsubscribeInteractionClass is the symmetric remover.
func (m *Manager) UnsubscribeInteractionClass(
	_ context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	cls core.InteractionClassHandle,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil
	}
	if set, ok := st.intSubs[cls]; ok {
		delete(set, sub)
		if len(set) == 0 {
			delete(st.intSubs, cls)
		}
	}
	return nil
}

// SubscribersFor returns federate handles subscribed to ANY of attrs on cls
// in fed, in sorted handle order. Returns an empty slice (never nil) when
// no subscribers match.
//
// Deterministic order: callers (object registry update path) rely on this
// for reproducible fanout sequences.
func (m *Manager) SubscribersFor(
	_ context.Context,
	fed core.FederationName,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
) []core.FederateHandle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok || len(attrs) == 0 {
		return []core.FederateHandle{}
	}
	union := map[core.FederateHandle]struct{}{}
	for _, a := range attrs {
		if set, ok := st.objSubs[objAttrKey{cls: cls, attr: a}]; ok {
			for h := range set {
				union[h] = struct{}{}
			}
		}
	}
	return sortedHandles(union)
}

// InteractionSubscribersFor returns federate handles subscribed to cls in
// fed, in sorted handle order.
func (m *Manager) InteractionSubscribersFor(
	_ context.Context,
	fed core.FederationName,
	cls core.InteractionClassHandle,
) []core.FederateHandle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return []core.FederateHandle{}
	}
	return sortedHandles(st.intSubs[cls])
}

// PublishersFor is the symmetric query for object class attributes.
// Used by the object registry to validate "does this federate publish?".
func (m *Manager) PublishersFor(
	_ context.Context,
	fed core.FederationName,
	cls core.ObjectClassHandle,
	attr core.AttributeHandle,
) []core.FederateHandle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return []core.FederateHandle{}
	}
	return sortedHandles(st.objPubs[objAttrKey{cls: cls, attr: attr}])
}

// InteractionPublishersFor is the symmetric query for interaction classes.
func (m *Manager) InteractionPublishersFor(
	_ context.Context,
	fed core.FederationName,
	cls core.InteractionClassHandle,
) []core.FederateHandle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return []core.FederateHandle{}
	}
	return sortedHandles(st.intPubs[cls])
}

// sortedHandles materializes a federate-handle set as a sorted slice.
// Returns an empty (non-nil) slice for nil/empty input — callers (and
// reflect.DeepEqual in tests) treat empty results uniformly.
func sortedHandles(set map[core.FederateHandle]struct{}) []core.FederateHandle {
	out := make([]core.FederateHandle, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	slices.Sort(out)
	return out
}
