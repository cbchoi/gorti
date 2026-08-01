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
var ErrNotImplemented = errors.New("declaration: not implemented ( M2 deliverable)")

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
	objPubs         map[objAttrKey]map[core.FederateHandle]struct{}
	objSubs         map[objAttrKey]map[core.FederateHandle]struct{}
	intPubs         map[core.InteractionClassHandle]map[core.FederateHandle]struct{}
	intSubs         map[core.InteractionClassHandle]map[core.FederateHandle]struct{}
	objSubSnapshots map[objAttrKey][]core.FederateHandle
	intSubSnapshots map[core.InteractionClassHandle][]core.FederateHandle
}

func newFederationState() *federationState {
	return &federationState{
		objPubs:         map[objAttrKey]map[core.FederateHandle]struct{}{},
		objSubs:         map[objAttrKey]map[core.FederateHandle]struct{}{},
		intPubs:         map[core.InteractionClassHandle]map[core.FederateHandle]struct{}{},
		intSubs:         map[core.InteractionClassHandle]map[core.FederateHandle]struct{}{},
		objSubSnapshots: map[objAttrKey][]core.FederateHandle{},
		intSubSnapshots: map[core.InteractionClassHandle][]core.FederateHandle{},
	}
}

// Manager holds per-federation pub/sub matrices and answers
// SubscribersFor / PublishersFor queries in deterministic handle order.
type Manager struct {
	mu  sync.RWMutex
	fed map[core.FederationName]*federationState

	// onSubscribeObjectClass — M36 optional post-subscribe hook.
	// Invoked AFTER a successful SubscribeObjectClassAttributes
	// recording, outside the manager mutex. cmd/rtid wires this to
	// mom.Manager.ObjectClassSubscribed so late subscribers to the
	// MOM object classes receive retroactive Discover/Reflect for
	// already-existing HLAfederation/HLAfederate instances.
	onSubscribeObjectClass func(
		ctx context.Context,
		fed core.FederationName,
		sub core.FederateHandle,
		cls core.ObjectClassHandle,
		attrs []core.AttributeHandle,
	)

	// advisoryOutbox — M37 optional outbox for the §5.10-§5.13
	// registration / interaction advisories. When nil (default), the
	// manager stays pure and emits nothing (pre-M37 behavior).
	advisoryOutbox core.Outbox
}

// SetAdvisoryOutbox wires the optional outbox for the §5.10-§5.13
// advisories (M37). Call during composition, before the server
// accepts RPCs; not synchronized against in-flight calls.
func (m *Manager) SetAdvisoryOutbox(outbox core.Outbox) {
	m.advisoryOutbox = outbox
}

// classHasSubscribersLocked reports whether ANY attribute of cls has at
// least one subscriber. Caller MUST hold m.mu.
func classHasSubscribersLocked(st *federationState, cls core.ObjectClassHandle) bool {
	for k, set := range st.objSubs {
		if k.cls == cls && len(set) > 0 {
			return true
		}
	}
	return false
}

// classPublishersLocked returns the sorted union of publishers across
// every attribute of cls. Caller MUST hold m.mu.
func classPublishersLocked(st *federationState, cls core.ObjectClassHandle) []core.FederateHandle {
	union := map[core.FederateHandle]struct{}{}
	for k, set := range st.objPubs {
		if k.cls != cls {
			continue
		}
		for h := range set {
			union[h] = struct{}{}
		}
	}
	return sortedHandles(union)
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

// OnFederateResign removes every publication and subscription owned by h.
func (m *Manager) OnFederateResign(fed core.FederationName, h core.FederateHandle) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.fed[fed]
	if st == nil {
		return
	}
	removeInteractions := func(sets map[core.InteractionClassHandle]map[core.FederateHandle]struct{}) {
		for key, set := range sets {
			delete(set, h)
			if len(set) == 0 {
				delete(sets, key)
			}
		}
	}
	removeInteractions(st.intPubs)
	removeInteractions(st.intSubs)
	for key, set := range st.objPubs {
		delete(set, h)
		if len(set) == 0 {
			delete(st.objPubs, key)
		}
	}
	for key, set := range st.objSubs {
		delete(set, h)
		if len(set) == 0 {
			delete(st.objSubs, key)
		}
	}
	clear(st.objSubSnapshots)
	clear(st.intSubSnapshots)
}

// OnFederationDestroyed removes all declaration state for fed.
func (m *Manager) OnFederationDestroyed(fed core.FederationName) {
	m.mu.Lock()
	delete(m.fed, fed)
	m.mu.Unlock()
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
//
// M36 DC-2 — whole-class form: an EMPTY attribute list means
// unpublishObjectClass (IEEE 1516.1-2010 §5.3). The wire exposes only
// the attribute-scoped RPC; the C++ DLC maps the §5.3 whole-class call
// to this RPC with an empty set (cppsdk RTIambassadorImpl
// unpublishObjectClass → unpublishObjectClassAttributes(cls, {})).
// Before this fix the empty list was a silent no-op, so publications
// survived a whole-class unpublish and a subsequent
// registerObjectInstance was NOT rejected with ObjectClassNotPublished
// as §6.8 requires (dm_unpublish_whole_vs_attrs fixture).
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
	if len(attrs) == 0 {
		// §5.3 whole-class unpublish: drop pub from EVERY attribute
		// publication of cls.
		for k, set := range st.objPubs {
			if k.cls != cls {
				continue
			}
			delete(set, pub)
			if len(set) == 0 {
				delete(st.objPubs, k)
			}
		}
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

// SetOnSubscribeObjectClass registers the optional post-subscribe hook
// (M36). Call during composition, before the server accepts RPCs; not
// synchronized against in-flight SubscribeObjectClassAttributes calls.
func (m *Manager) SetOnSubscribeObjectClass(fn func(
	ctx context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
)) {
	m.onSubscribeObjectClass = fn
}

// SubscribeObjectClassAttributes records federate `sub`'s subscription.
func (m *Manager) SubscribeObjectClassAttributes(
	ctx context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
) error {
	m.mu.Lock()
	st := m.stateLocked(fed)
	// §5.10 (M37) — detect the "class gains its FIRST
	// subscriber" flip before mutating; snapshot the publisher set
	// under the lock, emit after release.
	hadSubscribers := classHasSubscribersLocked(st, cls)
	for _, a := range attrs {
		k := objAttrKey{cls: cls, attr: a}
		set, ok := st.objSubs[k]
		if !ok {
			set = map[core.FederateHandle]struct{}{}
			st.objSubs[k] = set
		}
		set[sub] = struct{}{}
		delete(st.objSubSnapshots, k)
	}
	var startRecipients []core.FederateHandle
	if m.advisoryOutbox != nil && !hadSubscribers && classHasSubscribersLocked(st, cls) {
		startRecipients = classPublishersLocked(st, cls)
	}
	m.mu.Unlock()
	for _, h := range startRecipients {
		_ = m.advisoryOutbox.Send(ctx, fed, h, startRegistrationEvent(cls))
	}
	// M36 — fire the post-subscribe hook OUTSIDE the mutex (the MOM
	// retroactive fan-out sends Outbox events and takes its own locks).
	if m.onSubscribeObjectClass != nil {
		m.onSubscribeObjectClass(ctx, fed, sub, cls, attrs)
	}
	return nil
}

// UnsubscribeObjectClassAttributes is the symmetric remover.
func (m *Manager) UnsubscribeObjectClassAttributes(
	ctx context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
) error {
	m.mu.Lock()
	st, ok := m.fed[fed]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	hadSubscribers := classHasSubscribersLocked(st, cls)
	for _, a := range attrs {
		k := objAttrKey{cls: cls, attr: a}
		if set, ok := st.objSubs[k]; ok {
			delete(set, sub)
			if len(set) == 0 {
				delete(st.objSubs, k)
			}
		}
		delete(st.objSubSnapshots, k)
	}
	// §5.11 (M37) — the class lost its LAST subscriber.
	var stopRecipients []core.FederateHandle
	if m.advisoryOutbox != nil && hadSubscribers && !classHasSubscribersLocked(st, cls) {
		stopRecipients = classPublishersLocked(st, cls)
	}
	m.mu.Unlock()
	for _, h := range stopRecipients {
		_ = m.advisoryOutbox.Send(ctx, fed, h, stopRegistrationEvent(cls))
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
	ctx context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	cls core.InteractionClassHandle,
) error {
	m.mu.Lock()
	st := m.stateLocked(fed)
	set, ok := st.intSubs[cls]
	if !ok {
		set = map[core.FederateHandle]struct{}{}
		st.intSubs[cls] = set
	}
	wasEmpty := len(set) == 0
	set[sub] = struct{}{}
	delete(st.intSubSnapshots, cls)
	// §5.12 (M37) — the interaction class gained its FIRST
	// subscriber: tell each publisher to turn interactions on.
	var onRecipients []core.FederateHandle
	if m.advisoryOutbox != nil && wasEmpty {
		onRecipients = sortedHandles(st.intPubs[cls])
	}
	m.mu.Unlock()
	for _, h := range onRecipients {
		_ = m.advisoryOutbox.Send(ctx, fed, h, turnInteractionsOnEvent(cls))
	}
	return nil
}

// UnsubscribeInteractionClass is the symmetric remover.
func (m *Manager) UnsubscribeInteractionClass(
	ctx context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	cls core.InteractionClassHandle,
) error {
	m.mu.Lock()
	st, ok := m.fed[fed]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	var offRecipients []core.FederateHandle
	if set, ok := st.intSubs[cls]; ok {
		hadSubscriber := len(set) > 0
		delete(set, sub)
		if len(set) == 0 {
			delete(st.intSubs, cls)
			// §5.13 (M37) — the interaction class lost its
			// LAST subscriber: tell each publisher to turn
			// interactions off.
			if m.advisoryOutbox != nil && hadSubscriber {
				offRecipients = sortedHandles(st.intPubs[cls])
			}
		}
	}
	delete(st.intSubSnapshots, cls)
	m.mu.Unlock()
	for _, h := range offRecipients {
		_ = m.advisoryOutbox.Send(ctx, fed, h, turnInteractionsOffEvent(cls))
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

// ObjectSubscribersSnapshot returns a manager-owned immutable sorted snapshot
// for the common one-attribute routing query. Callers must not mutate the
// returned slice. Multi-attribute queries retain SubscribersFor semantics.
func (m *Manager) ObjectSubscribersSnapshot(
	ctx context.Context,
	fed core.FederationName,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
) []core.FederateHandle {
	if len(attrs) != 1 {
		return m.SubscribersFor(ctx, fed, cls, attrs)
	}
	key := objAttrKey{cls: cls, attr: attrs[0]}
	m.mu.RLock()
	st := m.fed[fed]
	if st != nil {
		if snapshot, ok := st.objSubSnapshots[key]; ok {
			m.mu.RUnlock()
			return snapshot
		}
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	st = m.fed[fed]
	if st == nil {
		return []core.FederateHandle{}
	}
	if snapshot, ok := st.objSubSnapshots[key]; ok {
		return snapshot
	}
	snapshot := sortedHandles(st.objSubs[key])
	st.objSubSnapshots[key] = snapshot
	return snapshot
}

// InteractionSubscribersSnapshot returns a manager-owned immutable sorted
// snapshot. Subscription changes and lifecycle teardown replace or discard the
// cached slice while in-flight readers may safely finish with the old value.
func (m *Manager) InteractionSubscribersSnapshot(
	_ context.Context,
	fed core.FederationName,
	cls core.InteractionClassHandle,
) []core.FederateHandle {
	m.mu.RLock()
	st := m.fed[fed]
	if st != nil {
		if snapshot, ok := st.intSubSnapshots[cls]; ok {
			m.mu.RUnlock()
			return snapshot
		}
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	st = m.fed[fed]
	if st == nil {
		return []core.FederateHandle{}
	}
	if snapshot, ok := st.intSubSnapshots[cls]; ok {
		return snapshot
	}
	snapshot := sortedHandles(st.intSubs[cls])
	st.intSubSnapshots[cls] = snapshot
	return snapshot
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

// PublishesObjectAttribute reports direct publisher membership without
// allocating and sorting the full publisher set.
func (m *Manager) PublishesObjectAttribute(
	_ context.Context,
	fed core.FederationName,
	cls core.ObjectClassHandle,
	attr core.AttributeHandle,
	publisher core.FederateHandle,
) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.fed[fed]
	if st == nil {
		return false
	}
	_, ok := st.objPubs[objAttrKey{cls: cls, attr: attr}][publisher]
	return ok
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

// PublishesInteraction reports whether one federate publishes cls without
// allocating and sorting the full publisher set. ObjectRegistry uses this on
// the interaction hot path; InteractionPublishersFor remains the snapshot API.
func (m *Manager) PublishesInteraction(
	_ context.Context,
	fed core.FederationName,
	cls core.InteractionClassHandle,
	publisher core.FederateHandle,
) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return false
	}
	_, ok = st.intPubs[cls][publisher]
	return ok
}

// Snapshot returns a point-in-time read of the per-federation pub/sub
// state. Read under the manager RLock; safe for concurrent callers.
//
// Phase 1 of the rtid-TUI plan (docs/rtid-tui.md): consumed by the
// AdminService Snapshot RPC. Return type is core.DeclarationSnapshot
// so the result threads through the core.DeclarationManagement
// interface unchanged. The return value is a fresh allocation;
// callers may retain it.
func (m *Manager) Snapshot(fed core.FederationName) core.DeclarationSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return core.DeclarationSnapshot{
			PerFederate: map[core.FederateHandle]core.DeclarationFederatePubSub{},
		}
	}

	type fedPubSubAcc struct {
		objPubs map[core.ObjectClassHandle]struct{}
		objSubs map[core.ObjectClassHandle]struct{}
		intPubs map[core.InteractionClassHandle]struct{}
		intSubs map[core.InteractionClassHandle]struct{}
	}
	acc := map[core.FederateHandle]*fedPubSubAcc{}
	getAcc := func(h core.FederateHandle) *fedPubSubAcc {
		a, ok := acc[h]
		if !ok {
			a = &fedPubSubAcc{
				objPubs: map[core.ObjectClassHandle]struct{}{},
				objSubs: map[core.ObjectClassHandle]struct{}{},
				intPubs: map[core.InteractionClassHandle]struct{}{},
				intSubs: map[core.InteractionClassHandle]struct{}{},
			}
			acc[h] = a
		}
		return a
	}

	pubClasses := map[core.ObjectClassHandle]struct{}{}
	for k, set := range st.objPubs {
		pubClasses[k.cls] = struct{}{}
		for h := range set {
			getAcc(h).objPubs[k.cls] = struct{}{}
		}
	}
	for k, set := range st.objSubs {
		for h := range set {
			getAcc(h).objSubs[k.cls] = struct{}{}
		}
	}
	for cls, set := range st.intPubs {
		for h := range set {
			getAcc(h).intPubs[cls] = struct{}{}
		}
	}
	for cls, set := range st.intSubs {
		for h := range set {
			getAcc(h).intSubs[cls] = struct{}{}
		}
	}

	out := core.DeclarationSnapshot{
		PerFederate: make(map[core.FederateHandle]core.DeclarationFederatePubSub, len(acc)),
	}

	pubList := make([]core.ObjectClassHandle, 0, len(pubClasses))
	for c := range pubClasses {
		pubList = append(pubList, c)
	}
	slices.Sort(pubList)
	out.PublishedObjectClasses = pubList

	for h, a := range acc {
		out.PerFederate[h] = core.DeclarationFederatePubSub{
			Handle:                       h,
			PublishedObjectClasses:       sortedObjClasses(a.objPubs),
			SubscribedObjectClasses:      sortedObjClasses(a.objSubs),
			PublishedInteractionClasses:  sortedIntClasses(a.intPubs),
			SubscribedInteractionClasses: sortedIntClasses(a.intSubs),
		}
	}
	return out
}

// sortedObjClasses materialises an object-class set as a sorted slice.
func sortedObjClasses(set map[core.ObjectClassHandle]struct{}) []core.ObjectClassHandle {
	out := make([]core.ObjectClassHandle, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}

// sortedIntClasses materialises an interaction-class set as a sorted slice.
func sortedIntClasses(set map[core.InteractionClassHandle]struct{}) []core.InteractionClassHandle {
	out := make([]core.InteractionClassHandle, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
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
