package mom

import (
	"context"
	"errors"
	gosync "sync"
	"sync/atomic"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrNotImplemented is retained as an exported sentinel for the
// frozen spec-test contract — callers (tests) match on it to skip
// cleanly during pre-dispatch RED. Implemented methods never return
// it; the spec tests in rti/spec/M11/ now exercise the real bodies.
var ErrNotImplemented = errors.New("mom: not implemented (Agent A M11 deliverable)")

// Standard MOM class names per IEEE 1516-2010 §10. The RTI registers
// instances of these classes (one HLAfederation per active federation;
// one HLAfederate per joined federate) and maintains their attributes
// in real time.
const (
	ClassHLAfederation = "HLAobjectRoot.HLAmanager.HLAfederation"
	ClassHLAfederate   = "HLAobjectRoot.HLAmanager.HLAfederate"
)

// Standard MOM attribute names. Cut 2 (M11) populates the read-only
// subset; cut 3 adds the writable ones (control via interactions).
//
// On HLAfederation:
//   - HLAfederationName (string)
//   - HLAfederatesInFederation (list of HLAfederateHandle)
//   - HLAlastSaveName, HLAlastSaveTime (deferred to M9)
//   - HLAFOMmoduleDesignatorList (list of strings)
//
// On HLAfederate:
//   - HLAfederateHandle (uint32)
//   - HLAfederateName (string)
//   - HLAfederateType (string)
//   - HLAtimeRegulating (bool)
//   - HLAtimeConstrained (bool)
//   - HLAlogicalTime (LogicalTime)
//   - HLAlookahead (LogicalTime)
//   - HLAGALT (LogicalTime; greatest available logical time = LBTS-related)
//   - HLAobjectInstancesThatCanBeDeleted (uint32)
//   - HLAreflectionsReceived (uint32)
//   - HLAupdatesSent (uint32)
//   - HLAinteractionsReceived (uint32)
//   - HLAinteractionsSent (uint32)

// Options bundles Manager dependencies.
type Options struct {
	// Outbox delivers MOM attribute updates to subscribed federates.
	// MUST NOT be nil.
	Outbox core.Outbox

	// EventLog records MOM-instance lifecycle (register / update /
	// remove). Optional — when nil, MOM events are silently dropped.
	EventLog core.EventLog
}

// Manager owns per-federation MOM-instance state. Goroutine-safe.
//
// FROZEN-shape per docs/srs.md FR-MOM-1..3.
//
// Cut-1 simplification (documented in docs/reports/M11/agent-a.md):
// the Query accessors are the authoritative gate for spec tests; real
// federate-side subscription via the standard pub/sub APIs requires the
// object.Registry to be aware of MOM classes as subscribable, which is
// out of scope for M11 cut-1. The Manager records the canonical
// snapshot; the subscriber fan-out is a follow-up (M11 W2 or M12).
type Manager struct {
	opts Options

	mu  gosync.RWMutex
	fed map[core.FederationName]*momState
}

// Compile-time assertion: *Manager satisfies core.ManagementObjectModel.
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.3) introduced the interface; production keeps using *Manager.
var _ core.ManagementObjectModel = (*Manager)(nil)

// New constructs a Manager. Returns an error if Outbox is nil. EventLog
// is optional in cut 1 (a nil log silently drops MOM events).
func New(opts Options) (*Manager, error) {
	if opts.Outbox == nil {
		return nil, errors.New("mom.New: Options.Outbox is required")
	}
	return &Manager{
		opts: opts,
		fed:  map[core.FederationName]*momState{},
	}, nil
}

// FederationCreated registers the HLAfederation MOM object instance for
// a newly-created federation. Wired from cmd/rtid into
// federationService.OnCreateFederationSuccess (the same hook M6 W1C
// added for fomRepository.RememberFor).
//
// Idempotent: re-registering the same federation overwrites the prior
// FOM module list but preserves any per-federate snapshots already
// registered for the name (defensive — cmd/rtid is not expected to fire
// this hook twice for the same name).
func (m *Manager) FederationCreated(
	ctx context.Context,
	fed core.FederationName,
	fomModules []core.FOMModule,
) error {
	_ = ctx
	if fed == "" {
		return errors.New("mom: FederationCreated requires non-empty federation name")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok {
		st = newMOMState()
		m.fed[fed] = st
	}
	st.federation.name = fed
	st.federation.fomModuleNames = fomModuleNamesFor(fomModules)
	return nil
}

// FederationDestroyed removes the HLAfederation MOM instance and any
// remaining HLAfederate snapshots for the federation. No-op when the
// federation is unknown; destroy of an unknown federation is treated
// as benign idempotent because the federation may have been destroyed
// before any MOM hook fired (the gRPC handler only calls MOM hooks on
// success, so this guard is mostly defensive).
func (m *Manager) FederationDestroyed(
	ctx context.Context,
	fed core.FederationName,
) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.fed, fed)
	return nil
}

// FederateJoined registers an HLAfederate MOM object for the new
// federate AND updates HLAfederation.HLAfederatesInFederation.
//
// If the federation snapshot does not yet exist (e.g. the
// FederationCreated hook was not wired or fired out-of-order), this
// method lazily creates one with empty FOM-module list — the join
// hook is the higher-priority ground truth for "this federation is
// active and has at least one federate".
func (m *Manager) FederateJoined(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
	name string,
	federateType string,
) error {
	_ = ctx
	if h == core.InvalidFederateHandle {
		return errors.New("mom: FederateJoined requires non-zero handle")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok {
		st = newMOMState()
		st.federation.name = fed
		m.fed[fed] = st
	}
	// Last-writer-wins: re-joining a handle (defensive — the federation
	// manager forbids handle reuse so this is rare) updates the name +
	// type and resets counters. Counters reset is acceptable cut-1
	// behavior — a clean-slate snapshot is more useful than carrying
	// stale numbers from a stale lifetime.
	st.federates[h] = &federateSnapshot{
		handle:       h,
		name:         name,
		federateType: federateType,
	}
	st.addFederateHandle(h)
	return nil
}

// FederateResigned removes the HLAfederate MOM object AND updates
// HLAfederation.HLAfederatesInFederation. No-op when the federation
// or the federate is unknown — the caller is the gRPC ResignFederation
// handler, which only fires this hook on success, so the no-op path is
// purely defensive.
func (m *Manager) FederateResigned(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil
	}
	delete(st.federates, h)
	st.removeFederateHandle(h)
	return nil
}

// TimeStateChanged updates HLAfederate.HLAtimeRegulating /
// HLAtimeConstrained / HLAlookahead / HLAlogicalTime. Wired from
// time.Manager's EnableRegulation / DisableRegulation /
// EnableConstrained / DisableConstrained code paths.
//
// No-op when the federate is unknown to the MOM (e.g. the join hook
// was never wired); time-state is purely a derived view of state the
// time.Manager already owns, so a missing MOM record does not impair
// federation correctness.
func (m *Manager) TimeStateChanged(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
	regulating bool,
	constrained bool,
	lookahead core.LogicalTime,
	logicalTime core.LogicalTime,
) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil
	}
	fs, ok := st.federates[h]
	if !ok {
		return nil
	}
	fs.timeRegulating = regulating
	fs.timeConstrained = constrained
	fs.lookahead = lookahead
	fs.logicalTime = logicalTime
	return nil
}

// IncrementInteractionsSent / Received / UpdatesSent / ReflectionsReceived
// are called by the object/declaration code paths to maintain per-federate
// counters on the HLAfederate MOM instance. Cut-1 simplification: these
// are best-effort metrics, not strictly atomic with the underlying event.
//
// Lookups take the manager RLock; the increment itself is an atomic op
// on the snapshot's counter field so concurrent increments from the
// dispatcher fan-out do not contend on the manager mutex.
func (m *Manager) IncrementInteractionsSent(fed core.FederationName, h core.FederateHandle) {
	if fs := m.lookupFederate(fed, h); fs != nil {
		atomic.AddUint32(&fs.interactionsSent, 1)
	}
}

func (m *Manager) IncrementInteractionsReceived(fed core.FederationName, h core.FederateHandle) {
	if fs := m.lookupFederate(fed, h); fs != nil {
		atomic.AddUint32(&fs.interactionsReceived, 1)
	}
}

func (m *Manager) IncrementUpdatesSent(fed core.FederationName, h core.FederateHandle) {
	if fs := m.lookupFederate(fed, h); fs != nil {
		atomic.AddUint32(&fs.updatesSent, 1)
	}
}

func (m *Manager) IncrementReflectionsReceived(fed core.FederationName, h core.FederateHandle) {
	if fs := m.lookupFederate(fed, h); fs != nil {
		atomic.AddUint32(&fs.reflectionsReceived, 1)
	}
}

// lookupFederate returns the live federate snapshot pointer (NOT a
// copy) under the manager RLock. The returned pointer is safe to use
// for atomic counter increments because federate snapshots are only
// removed under the write lock, and the increment uses atomic ops on
// the uint32 fields directly.
func (m *Manager) lookupFederate(fed core.FederationName, h core.FederateHandle) *federateSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return nil
	}
	return st.federates[h]
}

// QueryFederateAttributes is a test/introspection accessor returning a
// snapshot of an HLAfederate MOM instance's attribute values. The MOM
// runtime publishes these via the standard pub/sub APIs in production;
// this accessor lets spec tests verify state without subscribing.
type FederateAttributes struct {
	Handle               core.FederateHandle
	Name                 string
	Type                 string
	TimeRegulating       bool
	TimeConstrained      bool
	Lookahead            core.LogicalTime
	LogicalTime          core.LogicalTime
	InteractionsSent     uint32
	InteractionsReceived uint32
	UpdatesSent          uint32
	ReflectionsReceived  uint32
}

func (m *Manager) QueryFederateAttributes(fed core.FederationName, h core.FederateHandle) (FederateAttributes, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return FederateAttributes{}, false
	}
	fs, ok := st.federates[h]
	if !ok {
		return FederateAttributes{}, false
	}
	return fs.snapshot(), true
}

// QueryFederationAttributes is the corresponding accessor for the
// HLAfederation instance.
type FederationAttributes struct {
	Name            core.FederationName
	FederateHandles []core.FederateHandle
	FOMModuleNames  []string
}

// Snapshot returns aggregate per-federate counters for the
// AdminService handler. Phase 1 of the rtid-TUI plan
// (docs/rtid-tui.md): consumed to populate FederateSnapshot.{updates_sent,
// interactions_sent, reflections_received, interactions_received}.
//
// Counter reads use atomic.LoadUint32 so concurrent Increment* calls
// don't tear; the manager RLock guards the map walk only.
func (m *Manager) Snapshot(fed core.FederationName) core.MOMSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return core.MOMSnapshot{
			PerFederate: map[core.FederateHandle]core.MOMFederateCounters{},
		}
	}
	out := core.MOMSnapshot{
		PerFederate: make(map[core.FederateHandle]core.MOMFederateCounters, len(st.federates)),
	}
	for h, fs := range st.federates {
		out.PerFederate[h] = core.MOMFederateCounters{
			Handle:               h,
			UpdatesSent:          atomic.LoadUint32(&fs.updatesSent),
			InteractionsSent:     atomic.LoadUint32(&fs.interactionsSent),
			ReflectionsReceived:  atomic.LoadUint32(&fs.reflectionsReceived),
			InteractionsReceived: atomic.LoadUint32(&fs.interactionsReceived),
		}
	}
	return out
}

func (m *Manager) QueryFederationAttributes(fed core.FederationName) (FederationAttributes, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return FederationAttributes{}, false
	}
	return st.snapshotFederation(), true
}

// QueryFederation is the core.ManagementObjectModel form of
// QueryFederationAttributes (M12 W3 cut-3). The two methods return the
// same data; QueryFederation copies into the package-free
// core.MOMFederationAttributes shape so the gRPC handler can bind to
// the interface (coordinator pattern) rather than the concrete
// *mom.Manager. The pre-existing QueryFederationAttributes is kept
// for backwards compatibility with rti/internal/mom unit tests that
// already match on its return type.
func (m *Manager) QueryFederation(fed core.FederationName) (core.MOMFederationAttributes, bool) {
	attrs, ok := m.QueryFederationAttributes(fed)
	if !ok {
		return core.MOMFederationAttributes{}, false
	}
	return core.MOMFederationAttributes{
		Name:            attrs.Name,
		FederateHandles: attrs.FederateHandles,
		FOMModuleNames:  attrs.FOMModuleNames,
	}, true
}

// QueryFederate is the core.ManagementObjectModel form of
// QueryFederateAttributes (M12 W3 cut-3). See QueryFederation for the
// rationale on the parallel typed-method pair.
func (m *Manager) QueryFederate(fed core.FederationName, h core.FederateHandle) (core.MOMFederateAttributes, bool) {
	attrs, ok := m.QueryFederateAttributes(fed, h)
	if !ok {
		return core.MOMFederateAttributes{}, false
	}
	return core.MOMFederateAttributes{
		Handle:               attrs.Handle,
		Name:                 attrs.Name,
		Type:                 attrs.Type,
		TimeRegulating:       attrs.TimeRegulating,
		TimeConstrained:      attrs.TimeConstrained,
		Lookahead:            attrs.Lookahead,
		LogicalTime:          attrs.LogicalTime,
		InteractionsSent:     attrs.InteractionsSent,
		InteractionsReceived: attrs.InteractionsReceived,
		UpdatesSent:          attrs.UpdatesSent,
		ReflectionsReceived:  attrs.ReflectionsReceived,
	}, true
}

// --- M20.4 §10 HLAsetSwitches setters --------------------------------------

// SetAutoProvideSwitch updates the federation-wide HLAautoProvide
// switch. No-op when the federation is unknown to the MOM (consistent
// with TimeStateChanged's missing-federation policy).
func (m *Manager) SetAutoProvideSwitch(fed core.FederationName, v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok {
		return
	}
	st.federation.autoProvideSwitch = v
}

// AutoProvideSwitch returns the current federation-wide HLAautoProvide
// state, defaulting to false when the federation is unknown.
func (m *Manager) AutoProvideSwitch(fed core.FederationName) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return false
	}
	return st.federation.autoProvideSwitch
}

// SetConveyRegionDesignatorSetsSwitch updates the per-federate switch.
// No-op when the (federation, federate) pair is unknown.
func (m *Manager) SetConveyRegionDesignatorSetsSwitch(
	fed core.FederationName, h core.FederateHandle, v bool,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok {
		return
	}
	fs, ok := st.federates[h]
	if !ok {
		return
	}
	fs.conveyRegionDesignatorSetsSwitch = v
}

// ConveyRegionDesignatorSetsSwitch returns the per-federate switch
// value, defaulting to false when the federate is unknown.
func (m *Manager) ConveyRegionDesignatorSetsSwitch(
	fed core.FederationName, h core.FederateHandle,
) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return false
	}
	fs, ok := st.federates[h]
	if !ok {
		return false
	}
	return fs.conveyRegionDesignatorSetsSwitch
}

// SetConveyProducingFederateSwitch + accessor — symmetric pair.
func (m *Manager) SetConveyProducingFederateSwitch(
	fed core.FederationName, h core.FederateHandle, v bool,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.fed[fed]
	if !ok {
		return
	}
	fs, ok := st.federates[h]
	if !ok {
		return
	}
	fs.conveyProducingFederateSwitch = v
}

func (m *Manager) ConveyProducingFederateSwitch(
	fed core.FederationName, h core.FederateHandle,
) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return false
	}
	fs, ok := st.federates[h]
	if !ok {
		return false
	}
	return fs.conveyProducingFederateSwitch
}
