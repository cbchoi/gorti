package core

import "context"

// SaveState reports the current state of a federation save in
// progress (IEEE 1516.1-2010 §4.8..§4.11). The savepoint package
// aliases its own SaveState to this type so the production
// *savepoint.Manager satisfies SavepointCoordinator without per-method
// conversion.
type SaveState int

// SaveState constants. The savepoint package re-exports these under
// shorter spellings (StateIdle, StateInitiated, ...).
const (
	// SaveStateIdle — no save in progress.
	SaveStateIdle SaveState = iota
	// SaveStateInitiated — requestFederationSave succeeded; awaiting
	// per-federate federateSaveComplete responses.
	SaveStateInitiated
	// SaveStateSaved — all federates reported complete; bundle written;
	// federationSaved emitted.
	SaveStateSaved
	// SaveStateNotSaved — at least one federate reported failure;
	// federationNotSaved emitted; bundle NOT written.
	SaveStateNotSaved
)

// RestoreState reports the current state of a federation restore in
// progress.
type RestoreState int

// RestoreState constants. The savepoint package re-exports these under
// shorter spellings (RestoreIdle, RestoreLoading, ...).
const (
	// SaveRestoreIdle — no restore in progress.
	SaveRestoreIdle RestoreState = iota
	// SaveRestoreLoading — bundle is being read + event log is replaying.
	SaveRestoreLoading
	// SaveRestoreInitiated — initiateFederateRestore broadcast; awaiting
	// per-federate federateRestoreComplete responses.
	SaveRestoreInitiated
	// SaveRestoreCompleted — all federates reported complete;
	// federationRestored emitted.
	SaveRestoreCompleted
	// SaveRestoreFailed — bundle missing/corrupt OR a federate reported
	// failure.
	SaveRestoreFailed
)

// SavepointCoordinator orchestrates the federation save / restore
// protocol (IEEE 1516.1-2010 §4.8..§4.15). Phase 1 of the
// research-platform refactor (docs/research-platform.md §5.5) carves
// this out as the service-level interface so alternative
// implementations (binary manifests, snapshot-based bundles, per-
// manager diff snapshots — see Phase 2 §6.4) can plug in without
// forking the gRPC handler.
//
// Production impl: rti/internal/savepoint.Manager.
//
// Concurrency: implementations must be goroutine-safe.
//
// Methods deliberately NOT exposed (no current external consumer; cut-4
// follow-up if/when wired):
//   - LoadManifest: test + introspection accessor only.
type SavepointCoordinator interface {
	// RequestFederationSave starts a save. Broadcasts
	// initiateFederateSave to all joined federates.
	RequestFederationSave(
		ctx context.Context,
		fed FederationName,
		label string,
		saveTime *LogicalTime,
	) error

	// FederateSaveComplete records a federate's successful save. When
	// every required federate has responded, the manager closes out the
	// save (writes bundle, emits federationSaved).
	FederateSaveComplete(
		ctx context.Context,
		fed FederationName,
		h FederateHandle,
	) error

	// FederateSaveNotComplete records a federate's failed save. The
	// save closes out as federationNotSaved; bundle is NOT written.
	FederateSaveNotComplete(
		ctx context.Context,
		fed FederationName,
		h FederateHandle,
	) error

	// QuerySaveState returns the current save state for (fed, label).
	QuerySaveState(fed FederationName, label string) SaveState

	// RequestFederationRestore starts a restore. Loads the bundle,
	// replays the event log, broadcasts initiateFederateRestore.
	RequestFederationRestore(
		ctx context.Context,
		fed FederationName,
		label string,
	) error

	// FederateRestoreComplete records a federate's successful restore.
	// When every required federate has responded, the manager emits
	// federationRestored.
	FederateRestoreComplete(
		ctx context.Context,
		fed FederationName,
		h FederateHandle,
	) error

	// QueryRestoreState returns the current restore state for
	// (fed, label).
	QueryRestoreState(fed FederationName, label string) RestoreState

	// --- Read-only introspection (rtid-TUI Phase 1) ----------------------

	// Snapshot returns the current save / restore state for a
	// federation for the AdminService handler. When no save or restore
	// is in progress the returned states are SaveStateIdle /
	// SaveRestoreIdle respectively.
	Snapshot(fed FederationName) SavepointSnapshot
}

// SavepointSnapshot is the federation-wide save/restore status for
// the AdminService Snapshot RPC.
type SavepointSnapshot struct {
	// SaveLabel is the label of the save in progress; empty when
	// SaveState is SaveStateIdle.
	SaveLabel string
	SaveState SaveState

	// RestoreLabel is the label of the restore in progress; empty
	// when RestoreState is SaveRestoreIdle.
	RestoreLabel string
	RestoreState RestoreState
}
