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
}
