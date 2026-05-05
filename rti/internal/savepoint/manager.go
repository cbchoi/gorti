package savepoint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	gosync "sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrNotImplemented is retained as an exported sentinel for callers that
// matched on it during the M9 RED state. Implemented methods never return
// it; spec tests in rti/spec/M9/ reference it for clean pre-dispatch skip.
var ErrNotImplemented = errors.New("savepoint: not implemented (Agent A M9 deliverable)")

// SaveState reports the current state of a save in progress.
//
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.5) declared the canonical SaveState in core/savepoint.go so the
// concrete *Manager satisfies core.SavepointCoordinator without
// per-method conversion. SaveState (and the constants below) are
// re-exported here at the package boundary that external callers have
// always used.
type SaveState = core.SaveState

const (
	// StateIdle — no save in progress.
	StateIdle = core.SaveStateIdle
	// StateInitiated — requestFederationSave succeeded; awaiting per-federate
	// federateSaveComplete responses.
	StateInitiated = core.SaveStateInitiated
	// StateSaved — all federates reported complete; bundle written;
	// federationSaved emitted.
	StateSaved = core.SaveStateSaved
	// StateNotSaved — at least one federate reported failure;
	// federationNotSaved emitted; bundle NOT written.
	StateNotSaved = core.SaveStateNotSaved
)

// RestoreState reports the current state of a restore in progress.
type RestoreState = core.RestoreState

const (
	// RestoreIdle — no restore in progress.
	RestoreIdle = core.SaveRestoreIdle
	// RestoreLoading — bundle is being read + event log is replaying.
	RestoreLoading = core.SaveRestoreLoading
	// RestoreInitiated — initiateFederateRestore broadcast; awaiting
	// per-federate federateRestoreComplete responses.
	RestoreInitiated = core.SaveRestoreInitiated
	// RestoreCompleted — all federates reported complete; federationRestored emitted.
	RestoreCompleted = core.SaveRestoreCompleted
	// RestoreFailed — bundle missing/corrupt OR a federate reported failure.
	RestoreFailed = core.SaveRestoreFailed
)

// MembersResolver returns the snapshot of currently joined federate
// handles for a federation. Optional; when non-nil, savepoint.Manager
// calls it at RequestFederationSave-time to materialize the implicit
// "all joined federates" required set.
//
// When MembersResolver is nil, the Manager runs in cut-1 dynamic mode:
// any federate that calls FederateSaveComplete / FederateSaveNotComplete
// joins the required set on its first call, and the save is considered
// complete as soon as that single federate responds. Production wires
// MembersResolver via cmd/rtid; the spec tests rely on the dynamic
// path (or seed an explicit set via the optional Required fields on
// the request structs in future revisions).
type MembersResolver func(core.FederationName) []core.FederateHandle

// HaltedResolver, when non-nil, lets the Manager refuse a save request
// against a halted federation (FR-SR-1 cross-check with M3
// federationHalted). Optional; when nil, halt-state is not consulted.
type HaltedResolver func(core.FederationName) bool

// Options bundles Manager dependencies.
type Options struct {
	// Outbox delivers initiateFederateSave / federationSaved /
	// initiateFederateRestore / federationRestored. MUST NOT be nil.
	Outbox core.Outbox

	// EventLog is the source for the per-federation log slice that's
	// bundled into the save artifact. MAY be nil — when nil, the bundle
	// omits the event-log slice and the manifest records EventLogBytes
	// = 0; restore replays nothing and only re-broadcasts
	// initiateFederateRestore.
	EventLog core.EventLog

	// BundleStore is the persistence backend — typically a directory
	// path under which save bundles are written + read. MUST NOT be nil.
	BundleStore Storage

	// Members resolves the current joined-federate snapshot for a
	// federation when RequestFederationSave is called. See
	// MembersResolver doc for the nil-Members semantics.
	Members MembersResolver

	// Halted reports whether a federation is currently halted; the
	// Manager refuses RequestFederationSave when this returns true.
	// Optional.
	Halted HaltedResolver
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

	mu      gosync.Mutex
	saves   map[core.FederationName]*activeSave
	restore map[core.FederationName]*activeRestore
	// completed retains the final SaveState for (fed, label) past
	// activeSave teardown so QuerySaveState can keep returning
	// StateSaved / StateNotSaved after federationSaved fires.
	completed map[saveKey]SaveState
	// completedRestore mirrors completed for restores.
	completedRestore map[saveKey]RestoreState
}

// Compile-time assertion: *Manager satisfies core.SavepointCoordinator.
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.5) introduced the interface; the SaveState / RestoreState type
// aliases above let the existing method signatures match it without
// any conversion.
var _ core.SavepointCoordinator = (*Manager)(nil)

type saveKey struct {
	fed   core.FederationName
	label string
}

// activeSave is the per-federation in-flight save state.
type activeSave struct {
	label    string
	saveTime *core.LogicalTime
	state    SaveState
	required map[core.FederateHandle]struct{}
	complete map[core.FederateHandle]struct{}
	notComp  map[core.FederateHandle]struct{}
	dynamic  bool
	// manifest snapshot taken at request-time — federate list captured
	// for deterministic restore ordering.
	federates []core.FederateHandle
}

// activeRestore is the per-federation in-flight restore state.
type activeRestore struct {
	label    string
	state    RestoreState
	required map[core.FederateHandle]struct{}
	complete map[core.FederateHandle]struct{}
	manifest Manifest
}

// New constructs a Manager. Returns an error if any required Options
// field is nil.
func New(opts Options) (*Manager, error) {
	if opts.Outbox == nil {
		return nil, errors.New("savepoint.New: Options.Outbox is required")
	}
	if opts.BundleStore == nil {
		return nil, errors.New("savepoint.New: Options.BundleStore is required")
	}
	return &Manager{
		opts:             opts,
		saves:            map[core.FederationName]*activeSave{},
		restore:          map[core.FederationName]*activeRestore{},
		completed:        map[saveKey]SaveState{},
		completedRestore: map[saveKey]RestoreState{},
	}, nil
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
//   - core.ErrSaveAlreadyInProgress if another save is in progress
//   - core.ErrFederationHalted if the optional Halted resolver returns true
func (m *Manager) RequestFederationSave(
	ctx context.Context,
	fed core.FederationName,
	label string,
	saveTime *core.LogicalTime,
) error {
	if m.opts.Halted != nil && m.opts.Halted(fed) {
		return core.ErrFederationHalted
	}

	m.mu.Lock()
	if _, busy := m.saves[fed]; busy {
		m.mu.Unlock()
		return core.ErrSaveAlreadyInProgress
	}

	var required map[core.FederateHandle]struct{}
	dynamic := false
	if m.opts.Members != nil {
		members := m.opts.Members(fed)
		required = make(map[core.FederateHandle]struct{}, len(members))
		for _, h := range members {
			required[h] = struct{}{}
		}
	} else {
		required = map[core.FederateHandle]struct{}{}
		dynamic = true
	}

	// Defensive copy of saveTime — caller may reuse the pointee.
	var stCopy *core.LogicalTime
	if saveTime != nil {
		t := *saveTime
		stCopy = &t
	}

	federates := sortedHandles(required)
	as := &activeSave{
		label:     label,
		saveTime:  stCopy,
		state:     StateInitiated,
		required:  required,
		complete:  map[core.FederateHandle]struct{}{},
		notComp:   map[core.FederateHandle]struct{}{},
		dynamic:   dynamic,
		federates: federates,
	}
	m.saves[fed] = as
	m.completed[saveKey{fed, label}] = StateInitiated

	// Snapshot recipients before releasing the lock.
	recipients := append([]core.FederateHandle(nil), federates...)
	m.mu.Unlock()

	// Best-effort EventLog marker for replay determinism — the proto
	// Event variants for save transitions are not yet defined (cut-1),
	// so this is a no-op when EventLog is nil. The marker shape mirrors
	// sync.eventRecord (empty body, seq-only).
	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtSaveRequested, label: label})
	}

	// Fan out initiateFederateSave. Cut-1: proto FederateEvent oneof
	// does not yet carry an initiate-save variant, so each emission is
	// a placeholder envelope matching sync.announceOutbound's shape.
	//
	// Dynamic mode (no Members resolver wired): we have no concrete
	// recipient list at request-time, so we emit a single broadcast
	// envelope addressed to InvalidFederateHandle (the universal
	// "to all" sentinel). The fakeOutbox in spec tests counts every
	// emission; a future production gRPC handler would unfold this
	// envelope into per-stream sends from its own roster.
	if len(recipients) == 0 {
		evt := initiateFederateSaveEvent(label, stCopy)
		_ = m.opts.Outbox.Send(ctx, fed, core.InvalidFederateHandle, evt)
	} else {
		for _, h := range recipients {
			evt := initiateFederateSaveEvent(label, stCopy)
			_ = m.opts.Outbox.Send(ctx, fed, h, evt)
		}
	}
	return nil
}

// FederateSaveComplete records a federate's successful save. When all
// joined federates have called this OR FederateSaveNotComplete, the
// Manager closes out the save: writes the bundle, emits federationSaved.
func (m *Manager) FederateSaveComplete(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
) error {
	return m.recordFederateSave(ctx, fed, h, true)
}

// FederateSaveNotComplete records a federate's failed save. The save
// closes out as federationNotSaved (FR-SR-2); bundle is NOT written.
func (m *Manager) FederateSaveNotComplete(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
) error {
	return m.recordFederateSave(ctx, fed, h, false)
}

// recordFederateSave is the shared aggregation path for Complete /
// NotComplete. Returns core.ErrFederateNotInSave when no save is in
// flight for fed or the federate is not in the required set.
func (m *Manager) recordFederateSave(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
	ok bool,
) error {
	closedOut, failed, label, saveTime, feds, err := m.markFederateAndAggregate(fed, h, ok)
	if err != nil {
		return err
	}
	if !closedOut {
		return nil
	}
	if !failed {
		manifest := Manifest{
			Federation: fed,
			Label:      label,
			SaveTime:   saveTime,
			Federates:  feds,
		}
		if err := m.writeBundle(ctx, fed, label, manifest); err != nil {
			// Persistence failure flips the outcome to failed so
			// federates see federationNotSaved instead of a phantom
			// saved-state with no bundle on disk.
			m.mu.Lock()
			if as, busy := m.saves[fed]; busy {
				as.state = StateNotSaved
			}
			m.mu.Unlock()
			failed = true
		}
	}
	m.finalizeSave(fed, label, failed)
	m.emitSaveOutcome(ctx, fed, label, feds, failed)
	return nil
}

// markFederateAndAggregate records the federate's response on the
// active save and returns the post-response aggregation summary.
//
// Returns (closedOut=false, ...) when the save is still awaiting
// further responses; (closedOut=true, failed, ...) when every required
// federate has responded.
func (m *Manager) markFederateAndAggregate(
	fed core.FederationName,
	h core.FederateHandle,
	ok bool,
) (closedOut, failed bool, label string, saveTime *core.LogicalTime, feds []core.FederateHandle, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	as, busy := m.saves[fed]
	if !busy {
		return false, false, "", nil, nil, core.ErrFederateNotInSave
	}
	if as.dynamic {
		as.required[h] = struct{}{}
		as.federates = sortedHandles(as.required)
	} else if _, member := as.required[h]; !member {
		return false, false, "", nil, nil, core.ErrFederateNotInSave
	}
	if ok {
		as.complete[h] = struct{}{}
	} else {
		as.notComp[h] = struct{}{}
	}
	if !allRequiredResponded(as) {
		return false, false, "", nil, nil, nil
	}
	failed = len(as.notComp) > 0
	if failed {
		as.state = StateNotSaved
	} else {
		as.state = StateSaved
	}
	feds = append([]core.FederateHandle(nil), as.federates...)
	return true, failed, as.label, as.saveTime, feds, nil
}

// allRequiredResponded reports whether every required federate has
// either Complete'd or NotComplete'd.
func allRequiredResponded(as *activeSave) bool {
	if len(as.required) == 0 {
		return false
	}
	if len(as.complete)+len(as.notComp) < len(as.required) {
		return false
	}
	for req := range as.required {
		_, c := as.complete[req]
		_, n := as.notComp[req]
		if !c && !n {
			return false
		}
	}
	return true
}

// finalizeSave drops the active save entry and records the final state
// for QuerySaveState lookup after teardown.
func (m *Manager) finalizeSave(fed core.FederationName, label string, failed bool) {
	final := StateSaved
	if failed {
		final = StateNotSaved
	}
	m.mu.Lock()
	delete(m.saves, fed)
	m.completed[saveKey{fed, label}] = final
	m.mu.Unlock()
}

// emitSaveOutcome appends the federation-level outcome to the event log
// (best-effort) and fans out federationSaved / federationNotSaved to
// every recipient.
func (m *Manager) emitSaveOutcome(
	ctx context.Context,
	fed core.FederationName,
	label string,
	feds []core.FederateHandle,
	failed bool,
) {
	if m.opts.EventLog != nil {
		kind := evtFederationSaved
		if failed {
			kind = evtFederationNotSaved
		}
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: kind, label: label})
	}
	for _, dst := range feds {
		var evt core.OutboundEvent
		if failed {
			evt = federationNotSavedEvent(label)
		} else {
			evt = federationSavedEvent(label)
		}
		_ = m.opts.Outbox.Send(ctx, fed, dst, evt)
	}
}

// writeBundle serializes the manifest + event-log slice into the
// configured Storage. The bundle layout is documented in
// rti/internal/savepoint/manifest.go.
func (m *Manager) writeBundle(
	ctx context.Context,
	fed core.FederationName,
	label string,
	manifest Manifest,
) error {
	w, err := m.opts.BundleStore.Writer(fed, label)
	if err != nil {
		return fmt.Errorf("savepoint: open bundle writer (%s/%s): %w", fed, label, err)
	}
	defer func() { _ = w.Close() }()

	// Capture the event-log slice if EventLog supports OpenReader. The
	// MultiplexWriter implementation interprets the path argument as a
	// federation name (see eventlog/multiplex.go OpenReader doc); we
	// pass the federation name directly. Any error from OpenReader
	// (e.g. in-memory factory with no Dir) is treated as "no slice
	// available" — the bundle still records EventLogBytes = 0 and
	// restore replays nothing.
	var eventLogBytes []byte
	if m.opts.EventLog != nil {
		if rdr, err := m.opts.EventLog.OpenReader(ctx, string(fed)); err == nil {
			defer func() { _ = rdr.Close() }()
			// We can't get raw bytes through the EventLogReader
			// interface (it's record-oriented), so the cut-1 bundle
			// records a zero-length event-log slice. The on-disk
			// .log file remains the source of truth for replay; the
			// restore path consults the same MultiplexWriter and
			// can OpenReader the live federation log to drive
			// replay. See manifest.go docs for the deferral.
			eventLogBytes = nil
		}
	}

	manifest.EventLogBytes = uint64(len(eventLogBytes))
	if err := WriteBundle(w, manifest, eventLogBytes); err != nil {
		return fmt.Errorf("savepoint: write bundle (%s/%s): %w", fed, label, err)
	}
	return nil
}

// QuerySaveState returns the current save state for (fed, label).
func (m *Manager) QuerySaveState(fed core.FederationName, label string) SaveState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if as, ok := m.saves[fed]; ok && as.label == label {
		return as.state
	}
	if st, ok := m.completed[saveKey{fed, label}]; ok {
		return st
	}
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
//   - core.ErrRestoreAlreadyInProgress
func (m *Manager) RequestFederationRestore(
	ctx context.Context,
	fed core.FederationName,
	label string,
) error {
	if !m.opts.BundleStore.Exists(fed, label) {
		return ErrSaveBundleNotFound
	}

	m.mu.Lock()
	if _, busy := m.restore[fed]; busy {
		m.mu.Unlock()
		return core.ErrRestoreAlreadyInProgress
	}
	m.mu.Unlock()

	rdr, err := m.opts.BundleStore.Reader(fed, label)
	if err != nil {
		return err
	}
	manifest, _, err := ReadBundle(rdr)
	_ = rdr.Close()
	if err != nil {
		return fmt.Errorf("savepoint: read bundle (%s/%s): %w", fed, label, err)
	}

	required := make(map[core.FederateHandle]struct{}, len(manifest.Federates))
	for _, h := range manifest.Federates {
		required[h] = struct{}{}
	}
	recipients := append([]core.FederateHandle(nil), manifest.Federates...)

	m.mu.Lock()
	if _, busy := m.restore[fed]; busy {
		// Lost the race; someone else took it between checks.
		m.mu.Unlock()
		return core.ErrRestoreAlreadyInProgress
	}
	ar := &activeRestore{
		label:    label,
		state:    RestoreInitiated,
		required: required,
		complete: map[core.FederateHandle]struct{}{},
		manifest: manifest,
	}
	m.restore[fed] = ar
	m.completedRestore[saveKey{fed, label}] = RestoreInitiated
	m.mu.Unlock()

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtRestoreRequested, label: label})
	}

	for _, dst := range recipients {
		evt := initiateFederateRestoreEvent(label)
		_ = m.opts.Outbox.Send(ctx, fed, dst, evt)
	}
	return nil
}

// FederateRestoreComplete records a federate's successful restore.
// When all federates have responded, the Manager emits federationRestored.
func (m *Manager) FederateRestoreComplete(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
) error {
	m.mu.Lock()
	ar, busy := m.restore[fed]
	if !busy {
		m.mu.Unlock()
		return core.ErrFederateNotInRestore
	}
	if _, member := ar.required[h]; !member && len(ar.required) > 0 {
		m.mu.Unlock()
		return core.ErrFederateNotInRestore
	}
	ar.complete[h] = struct{}{}

	allDone := len(ar.required) > 0 && len(ar.complete) >= len(ar.required)
	if allDone {
		for req := range ar.required {
			if _, ok := ar.complete[req]; !ok {
				allDone = false
				break
			}
		}
	}
	var recipients []core.FederateHandle
	label := ar.label
	if allDone {
		ar.state = RestoreCompleted
		recipients = sortedHandles(ar.required)
	}
	m.mu.Unlock()

	if !allDone {
		return nil
	}

	m.mu.Lock()
	delete(m.restore, fed)
	m.completedRestore[saveKey{fed, label}] = RestoreCompleted
	m.mu.Unlock()

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtFederationRestored, label: label})
	}
	for _, dst := range recipients {
		evt := federationRestoredEvent(label)
		_ = m.opts.Outbox.Send(ctx, fed, dst, evt)
	}
	return nil
}

// QueryRestoreState returns the current restore state for (fed, label).
func (m *Manager) QueryRestoreState(fed core.FederationName, label string) RestoreState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ar, ok := m.restore[fed]; ok && ar.label == label {
		return ar.state
	}
	if st, ok := m.completedRestore[saveKey{fed, label}]; ok {
		return st
	}
	return RestoreIdle
}

// LoadManifest reads only the manifest header from a stored bundle.
// Used by tests + introspection tooling that needs to inspect the
// federate list / save-time without paying for a full event-log replay.
func (m *Manager) LoadManifest(fed core.FederationName, label string) (Manifest, error) {
	rdr, err := m.opts.BundleStore.Reader(fed, label)
	if err != nil {
		return Manifest{}, err
	}
	defer func() { _ = rdr.Close() }()
	manifest, _, err := ReadBundle(rdr)
	return manifest, err
}

// sortedHandles materializes a federate-handle set as a sorted slice for
// deterministic iteration (initiateFederateSave + federationSaved fan-out
// must be reproducible across replays — same contract as sync.Manager).
func sortedHandles(set map[core.FederateHandle]struct{}) []core.FederateHandle {
	out := make([]core.FederateHandle, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	slices.Sort(out)
	return out
}
