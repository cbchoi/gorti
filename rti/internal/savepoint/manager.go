package savepoint

import (
	"context"
	"errors"
	"io"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrNotImplemented is returned by stub methods until Agent A implements
// them in M9.
var ErrNotImplemented = errors.New("savepoint: not implemented (Agent A M9 deliverable)")

// SaveState reports the current state of a save in progress.
type SaveState int

const (
	// StateIdle — no save in progress.
	StateIdle SaveState = iota
	// StateInitiated — requestFederationSave succeeded; awaiting per-federate
	// federateSaveComplete responses.
	StateInitiated
	// StateSaved — all federates reported complete; bundle written;
	// federationSaved emitted.
	StateSaved
	// StateNotSaved — at least one federate reported failure;
	// federationNotSaved emitted; bundle NOT written.
	StateNotSaved
)

// RestoreState reports the current state of a restore in progress.
type RestoreState int

const (
	RestoreIdle RestoreState = iota
	// RestoreLoading — bundle is being read + event log is replaying.
	RestoreLoading
	// RestoreInitiated — initiateFederateRestore broadcast; awaiting
	// per-federate federateRestoreComplete responses.
	RestoreInitiated
	// RestoreCompleted — all federates reported complete; federationRestored emitted.
	RestoreCompleted
	// RestoreFailed — bundle missing/corrupt OR a federate reported failure.
	RestoreFailed
)

// Options bundles Manager dependencies.
type Options struct {
	// Outbox delivers initiateFederateSave / federationSaved / etc.
	// MUST NOT be nil.
	Outbox core.Outbox

	// EventLog is the source for the per-federation log slice that's
	// bundled into the save artifact. MUST NOT be nil for save to work.
	EventLog core.EventLog

	// BundleStore is the persistence backend — typically a directory
	// path under which save bundles are written + read. The contract
	// allows any io-based implementation (including in-memory for tests).
	BundleStore Storage
}

// Storage is the persistence interface for save bundles. Production
// uses a filesystem-backed implementation; tests use in-memory.
type Storage interface {
	// Writer opens a writer for a new bundle keyed by (fed, label).
	// Returns ErrSaveBundleExists if (fed, label) already saved.
	Writer(fed core.FederationName, label string) (io.WriteCloser, error)
	// Reader opens an existing bundle for reading.
	// Returns ErrSaveBundleNotFound if no such (fed, label).
	Reader(fed core.FederationName, label string) (io.ReadCloser, error)
	// Exists reports whether a bundle exists.
	Exists(fed core.FederationName, label string) bool
}

// ErrSaveBundleExists / ErrSaveBundleNotFound are storage-layer
// sentinels separate from the M8/M11 core errors.
var (
	ErrSaveBundleExists   = errors.New("save bundle already exists for this label")
	ErrSaveBundleNotFound = errors.New("save bundle not found")
)

// Manager orchestrates the save/restore protocol per federation.
// Goroutine-safe.
//
// FROZEN-shape per docs/srs.md FR-SR-1..5.
type Manager struct {
	opts Options
}

// New constructs a Manager. Returns an error if any required Options
// field is nil.
func New(opts Options) (*Manager, error) {
	_ = opts
	return &Manager{opts: opts}, ErrNotImplemented
}

// --- Save protocol (FR-SR-1, FR-SR-2) -------------------------------------

// RequestFederationSave starts a save. The RTI broadcasts
// initiateFederateSave to all joined federates and transitions the
// federation to StateInitiated.
//
// The optional saveTime parameter pins the save point at a logical
// time; nil means "save now" at the current synchronization point.
//
// Errors:
//   - ErrSaveAlreadyInProgress if another save is in progress
//   - ErrFederationHalted
func (m *Manager) RequestFederationSave(
	ctx context.Context,
	fed core.FederationName,
	label string,
	saveTime *core.LogicalTime,
) error {
	_ = ctx
	_ = fed
	_ = label
	_ = saveTime
	return ErrNotImplemented
}

// FederateSaveComplete records a federate's successful save. When all
// joined federates have called this OR FederateSaveNotComplete, the
// Manager closes out the save: writes the bundle, emits federationSaved.
func (m *Manager) FederateSaveComplete(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
) error {
	_ = ctx
	_ = fed
	_ = h
	return ErrNotImplemented
}

// FederateSaveNotComplete records a federate's failed save. The save
// closes out as federationNotSaved (FR-SR-2); bundle is NOT written.
func (m *Manager) FederateSaveNotComplete(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
) error {
	_ = ctx
	_ = fed
	_ = h
	return ErrNotImplemented
}

// QuerySaveState returns the current save state for (fed, label).
func (m *Manager) QuerySaveState(fed core.FederationName, label string) SaveState {
	_ = fed
	_ = label
	return StateIdle
}

// --- Restore protocol (FR-SR-3) ------------------------------------------

// RequestFederationRestore starts a restore. The RTI loads the bundle,
// replays the event log to reconstruct state, then broadcasts
// initiateFederateRestore to all federates and transitions to
// RestoreInitiated.
//
// Errors:
//   - ErrSaveBundleNotFound
//   - ErrRestoreAlreadyInProgress
func (m *Manager) RequestFederationRestore(
	ctx context.Context,
	fed core.FederationName,
	label string,
) error {
	_ = ctx
	_ = fed
	_ = label
	return ErrNotImplemented
}

// FederateRestoreComplete records a federate's successful restore.
// When all federates have responded, the Manager emits federationRestored.
func (m *Manager) FederateRestoreComplete(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
) error {
	_ = ctx
	_ = fed
	_ = h
	return ErrNotImplemented
}

// QueryRestoreState returns the current restore state for (fed, label).
func (m *Manager) QueryRestoreState(fed core.FederationName, label string) RestoreState {
	_ = fed
	_ = label
	return RestoreIdle
}
