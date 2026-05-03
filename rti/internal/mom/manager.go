package mom

import (
	"context"
	"errors"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrNotImplemented is returned by stub methods until Agent A
// implements them in M11.
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
type Manager struct {
	opts Options
}

// New constructs a Manager. Returns an error if any required Options
// field is nil.
func New(opts Options) (*Manager, error) {
	_ = opts
	return &Manager{opts: opts}, ErrNotImplemented
}

// FederationCreated registers the HLAfederation MOM object instance for
// a newly-created federation. Wired from cmd/rtid into
// federationService.OnCreateFederationSuccess (the same hook M6 W1C
// added for fomRepository.RememberFor).
func (m *Manager) FederationCreated(
	ctx context.Context,
	fed core.FederationName,
	fomModules []core.FOMModule,
) error {
	_ = ctx
	_ = fed
	_ = fomModules
	return ErrNotImplemented
}

// FederationDestroyed removes the HLAfederation MOM instance.
func (m *Manager) FederationDestroyed(
	ctx context.Context,
	fed core.FederationName,
) error {
	_ = ctx
	_ = fed
	return ErrNotImplemented
}

// FederateJoined registers an HLAfederate MOM object for the new
// federate AND updates HLAfederation.HLAfederatesInFederation.
func (m *Manager) FederateJoined(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
	name string,
	federateType string,
) error {
	_ = ctx
	_ = fed
	_ = h
	_ = name
	_ = federateType
	return ErrNotImplemented
}

// FederateResigned removes the HLAfederate MOM object AND updates
// HLAfederation.HLAfederatesInFederation.
func (m *Manager) FederateResigned(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
) error {
	_ = ctx
	_ = fed
	_ = h
	return ErrNotImplemented
}

// TimeStateChanged updates HLAfederate.HLAtimeRegulating /
// HLAtimeConstrained / HLAlookahead. Wired from time.Manager's
// EnableRegulation / DisableRegulation / EnableConstrained /
// DisableConstrained code paths.
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
	_ = fed
	_ = h
	_ = regulating
	_ = constrained
	_ = lookahead
	_ = logicalTime
	return ErrNotImplemented
}

// IncrementInteractionsSent / Received / UpdatesSent / ReflectionsReceived
// are called by the object/declaration code paths to maintain per-federate
// counters on the HLAfederate MOM instance. Cut-1 simplification: these
// are best-effort metrics, not strictly atomic with the underlying event.
func (m *Manager) IncrementInteractionsSent(fed core.FederationName, h core.FederateHandle) {
	_ = fed
	_ = h
}
func (m *Manager) IncrementInteractionsReceived(fed core.FederationName, h core.FederateHandle) {
	_ = fed
	_ = h
}
func (m *Manager) IncrementUpdatesSent(fed core.FederationName, h core.FederateHandle) {
	_ = fed
	_ = h
}
func (m *Manager) IncrementReflectionsReceived(fed core.FederationName, h core.FederateHandle) {
	_ = fed
	_ = h
}

// QueryFederateAttributes is a test/introspection accessor returning a
// snapshot of an HLAfederate MOM instance's attribute values. The MOM
// runtime publishes these via the standard pub/sub APIs in production;
// this accessor lets spec tests verify state without subscribing.
type FederateAttributes struct {
	Handle                 core.FederateHandle
	Name                   string
	Type                   string
	TimeRegulating         bool
	TimeConstrained        bool
	Lookahead              core.LogicalTime
	LogicalTime            core.LogicalTime
	InteractionsSent       uint32
	InteractionsReceived   uint32
	UpdatesSent            uint32
	ReflectionsReceived    uint32
}

func (m *Manager) QueryFederateAttributes(fed core.FederationName, h core.FederateHandle) (FederateAttributes, bool) {
	_ = fed
	_ = h
	return FederateAttributes{}, false
}

// QueryFederationAttributes is the corresponding accessor for the
// HLAfederation instance.
type FederationAttributes struct {
	Name              core.FederationName
	FederateHandles   []core.FederateHandle
	FOMModuleNames    []string
}

func (m *Manager) QueryFederationAttributes(fed core.FederationName) (FederationAttributes, bool) {
	_ = fed
	return FederationAttributes{}, false
}
