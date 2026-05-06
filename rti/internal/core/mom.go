package core

import "context"

// ManagementObjectModel owns the per-federation HLAfederation /
// HLAfederate MOM instance state (IEEE 1516-2010 §10) and exposes the
// hook surface that the rest of the RTI uses to keep MOM attributes
// in sync with federation lifecycle + per-federate counters
// (FR-MOM-1..3). Phase 1 of the research-platform refactor
// (docs/research-platform.md §5.3) carves this out as the
// service-level interface so alternative MOM implementations can plug
// in without forking the cmd/rtid composition root.
//
// Production impl: rti/internal/mom.Manager.
//
// Concurrency: implementations must be goroutine-safe — the hook
// surface is invoked from gRPC handlers (federation lifecycle),
// object.Registry fan-out (counters), and time.Manager state
// transitions concurrently.
//
// Methods currently consumed externally:
//   - FederationCreated / FederationDestroyed (gRPC federation
//     lifecycle hooks wired in cmd/rtid)
//   - FederateJoined    / FederateResigned    (likewise)
//   - IncrementUpdatesSent / IncrementInteractionsSent /
//     IncrementReflectionsReceived / IncrementInteractionsReceived
//     (method values handed to object.Options)
//
// Methods deliberately NOT exposed:
//   - TimeStateChanged: the time.Manager's OnTimeStateChanged hook
//     is not wired to MOM in production today (cut-4 follow-up).
//   - QueryFederateAttributes / QueryFederationAttributes: test +
//     introspection accessors only; the production manager's MOM
//     publishes attributes via standard pub/sub, so external callers
//     are tests holding a typed *mom.Manager reference.
//
// Researchers may extend the interface with any of the above when a
// real consumer arrives.
type ManagementObjectModel interface {
	// FederationCreated registers HLAfederation for a newly-created
	// federation, capturing the FOM module list.
	FederationCreated(
		ctx context.Context,
		fed FederationName,
		fomModules []FOMModule,
	) error

	// FederationDestroyed retires the HLAfederation instance and any
	// remaining HLAfederate snapshots for the federation.
	FederationDestroyed(
		ctx context.Context,
		fed FederationName,
	) error

	// FederateJoined registers HLAfederate for the joining federate.
	FederateJoined(
		ctx context.Context,
		fed FederationName,
		h FederateHandle,
		name string,
		federateType string,
	) error

	// FederateResigned removes the HLAfederate MOM instance.
	FederateResigned(
		ctx context.Context,
		fed FederationName,
		h FederateHandle,
	) error

	// IncrementInteractionsSent increments the per-federate
	// HLAinteractionsSent counter.
	IncrementInteractionsSent(fed FederationName, h FederateHandle)

	// IncrementInteractionsReceived increments the per-federate
	// HLAinteractionsReceived counter.
	IncrementInteractionsReceived(fed FederationName, h FederateHandle)

	// IncrementUpdatesSent increments the per-federate
	// HLAupdatesSent counter.
	IncrementUpdatesSent(fed FederationName, h FederateHandle)

	// IncrementReflectionsReceived increments the per-federate
	// HLAreflectionsReceived counter.
	IncrementReflectionsReceived(fed FederationName, h FederateHandle)

	// --- Read-only introspection (rtid-TUI Phase 1) ----------------------

	// Snapshot returns aggregate per-federate counters for the
	// AdminService handler. Read-only; cheap.
	Snapshot(fed FederationName) MOMSnapshot

	// QueryFederation returns the HLAfederation MOM object's
	// attribute snapshot for fed (M12 W3: cut-3 MomService gRPC
	// exposure). The second return is false when the federation is
	// not currently tracked (never created or already destroyed).
	//
	// This accessor was previously typed only on the concrete
	// *mom.Manager (test-introspection); it is part of the interface
	// from cut-3 onward so the gRPC handler's coordinator binding
	// can call it through the abstraction.
	QueryFederation(fed FederationName) (MOMFederationAttributes, bool)

	// QueryFederate returns one HLAfederate MOM object's attribute
	// snapshot for (fed, h). The second return is false when no
	// HLAfederate instance for h exists (federate never joined or
	// has resigned).
	QueryFederate(fed FederationName, h FederateHandle) (MOMFederateAttributes, bool)
}

// MOMFederationAttributes is the wire-shape snapshot of one
// HLAfederation MOM object's attributes (cut-3 MomService payload).
// Fields populated in cut-3:
//
//   - Name: HLAfederationName
//   - FederateHandles: HLAfederatesInFederation, sorted ascending
//   - FOMModuleNames: HLAFOMmoduleDesignatorList
//
// Forward-compatible: cut-4 may add LastSaveName / LastSaveTime /
// SyncPointsState / SaveState as the corresponding hook surfaces
// land. The proto MomService response holds reserved field tags for
// each.
type MOMFederationAttributes struct {
	Name            FederationName
	FederateHandles []FederateHandle
	FOMModuleNames  []string
}

// MOMFederateAttributes is the wire-shape snapshot of one HLAfederate
// MOM object's attributes (cut-3 MomService payload).
//
// Counter fields mirror MOMFederateCounters; the accessor returns
// counter values via atomic.LoadUint32 so concurrent IncrementX
// callers cannot tear individual reads. The struct is a plain value
// (not aliased to any internal state), safe to retain.
type MOMFederateAttributes struct {
	Handle               FederateHandle
	Name                 string
	Type                 string
	TimeRegulating       bool
	TimeConstrained      bool
	Lookahead            LogicalTime
	LogicalTime          LogicalTime
	InteractionsSent     uint32
	InteractionsReceived uint32
	UpdatesSent          uint32
	ReflectionsReceived  uint32
}

// MOMFederateCounters mirrors the per-federate counter set that
// mom.Manager maintains via the four IncrementX methods. The
// AdminService handler maps these onto FederateSnapshot.{updates_sent,
// interactions_sent, reflections_received, interactions_received}.
type MOMFederateCounters struct {
	Handle               FederateHandle
	UpdatesSent          uint32
	InteractionsSent     uint32
	ReflectionsReceived  uint32
	InteractionsReceived uint32
}

// MOMSnapshot is the federation-wide MOM rollup for the AdminService
// Snapshot RPC. PerFederate carries one entry per federate the MOM
// has seen via FederateJoined; the AdminService handler joins this
// against the federation manager's roster to build per-federate rows.
type MOMSnapshot struct {
	PerFederate map[FederateHandle]MOMFederateCounters
}
