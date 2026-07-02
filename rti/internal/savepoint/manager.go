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

// ManagerSnapshotter is implemented by per-service managers (sync,
// ownership, mom, ddm) that participate in M13's structured save
// manifest (docs/srs.md §10.4). On save, the savepoint Manager calls
// Marshal(fed) and bundles the bytes into manifest.ManagerSnapshots
// under the registered key. On restore, it calls Unmarshal(fed,
// bytes) before kicking off the event-log slice replay so the
// per-manager state is reconstructed from structured bytes rather
// than (only) replay-derived events.
//
// Marshal MUST be byte-deterministic for a given in-memory state so
// the same federation produces byte-identical bundles across replays
// (NFR-DET-1). Unmarshal of nil/empty bytes MUST be a no-op so an
// absent manifest entry restores cleanly (e.g. an old M9-era bundle
// that pre-dates M13's per-manager snapshots).
type ManagerSnapshotter interface {
	Marshal(fed core.FederationName) ([]byte, error)
	Unmarshal(fed core.FederationName, data []byte) error
}

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

	// Roster resolves the current joined (handle, name) roster for a
	// federation. Optional. M36 DB-3: when wired, save bundles capture
	// each participant's federate NAME alongside its handle, and
	// restore routes initiateFederateRestore by matching those saved
	// names against the CURRENT roster — IEEE 1516.1 §4.27 identifies
	// restore participants by name, and gorti never reuses handles
	// across resign + rejoin. When nil (or when a bundle carries no
	// names), restore falls back to the pre-M36 handle-based routing.
	// Production wires federation.Manager.ListMembers via cmd/rtid.
	Roster func(core.FederationName) []core.FederationMember

	// ManagerSnapshots is the keyed set of per-manager Marshalers /
	// Unmarshalers (M13 thread C). Production cmd/rtid wires the
	// four cut-2 service-group managers (sync, ownership, mom, ddm)
	// under the keys defined as ManagerSnapshotKey* in manifest.go.
	// nil/empty map means "no structured snapshots" — the bundle
	// reverts to the cut-1 event-log-only path (still functional,
	// just less efficient on restore).
	ManagerSnapshots map[string]ManagerSnapshotter
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
		// M13 thread C: collect per-manager snapshots before sealing.
		// Errors from any single manager flip the save outcome to
		// failed (federationNotSaved) so federates do not believe the
		// bundle is intact when it isn't.
		snapshots, snapErr := m.collectManagerSnapshots(fed)
		if snapErr != nil {
			m.mu.Lock()
			if as, busy := m.saves[fed]; busy {
				as.state = StateNotSaved
			}
			m.mu.Unlock()
			failed = true
		}
		if !failed {
			manifest := Manifest{
				Federation:       fed,
				Label:            label,
				SaveTime:         saveTime,
				Federates:        feds,
				FederateNames:    m.federateNamesFor(fed, feds),
				ManagerSnapshots: snapshots,
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

// federateNamesFor resolves the federate name for each handle in feds
// (index-parallel result) from the current roster. Returns nil when no
// Roster resolver is wired — the manifest then omits federate_names
// and restore falls back to handle-based routing. A handle absent from
// the roster (should not happen while it is mid-save) yields an empty
// string, which restore treats as "no name known; route by handle".
// M36 DB-3.
func (m *Manager) federateNamesFor(fed core.FederationName, feds []core.FederateHandle) []string {
	if m.opts.Roster == nil {
		return nil
	}
	byHandle := map[core.FederateHandle]string{}
	for _, mem := range m.opts.Roster(fed) {
		byHandle[mem.Handle] = mem.Name
	}
	names := make([]string, len(feds))
	for i, h := range feds {
		names[i] = byHandle[h]
	}
	return names
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

// AbortSave aborts an in-progress save for fed. M24 W3 — IEEE 1516.1-2010
// §4.28 abortFederationSave. Returns ErrSaveNotInProgress when no save
// is active. The completed map records the label as StateNotSaved so
// QuerySaveState reads back consistently.
func (m *Manager) AbortSave(_ context.Context, fed core.FederationName) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	as, ok := m.saves[fed]
	if !ok {
		return core.ErrSaveNotInProgress
	}
	label := as.label
	delete(m.saves, fed)
	m.completed[saveKey{fed, label}] = StateNotSaved
	return nil
}

// AbortRestore aborts an in-progress restore for fed. M24 W3 — IEEE
// 1516.1-2010 §4.30 abortFederationRestore. M17.25 (Cut-4) now also
// fans out federationNotRestored to every federate that received the
// initiateFederateRestore so they can clean up local state.
func (m *Manager) AbortRestore(ctx context.Context, fed core.FederationName) error {
	m.mu.Lock()
	ar, ok := m.restore[fed]
	if !ok {
		m.mu.Unlock()
		return core.ErrRestoreNotInProgress
	}
	label := ar.label
	recipients := sortedHandles(ar.required)
	delete(m.restore, fed)
	m.completedRestore[saveKey{fed, label}] = RestoreFailed
	m.mu.Unlock()

	for _, dst := range recipients {
		evt := federationNotRestoredEvent(label)
		_ = m.opts.Outbox.Send(ctx, fed, dst, evt)
	}
	return nil
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

// collectManagerSnapshots walks each registered ManagerSnapshotter
// in deterministic key order, calling Marshal(fed) on each, and
// returns the resulting key→bytes map. Any single Marshal error
// aborts the collection and propagates up so the save can be flipped
// to NotSaved. M13 thread C (docs/srs.md §10.4).
//
// Returns nil when no snapshotters are registered (cut-1 fallback —
// the bundle then carries only the manifest header + event-log
// slice, identical to a pre-M13 bundle).
//
// Empty marshal results (e.g. unknown federation, empty manager
// state) are dropped from the map so the manifest stays minimal.
func (m *Manager) collectManagerSnapshots(fed core.FederationName) (map[string][]byte, error) {
	if len(m.opts.ManagerSnapshots) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(m.opts.ManagerSnapshots))
	for k := range m.opts.ManagerSnapshots {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := map[string][]byte{}
	for _, key := range keys {
		snapper := m.opts.ManagerSnapshots[key]
		if snapper == nil {
			continue
		}
		bytes, err := snapper.Marshal(fed)
		if err != nil {
			return nil, fmt.Errorf("savepoint: %s.Marshal(%q): %w", key, fed, err)
		}
		if len(bytes) == 0 {
			continue
		}
		out[key] = bytes
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// applyManagerSnapshots reverses collectManagerSnapshots: for each
// key in manifest.ManagerSnapshots, dispatches to the matching
// registered ManagerSnapshotter's Unmarshal. Keys without a
// registered handler are silently ignored — a future cut may add
// new manager keys, and rolling restores against a partially-upgraded
// rtid should not crash. M13 thread C (docs/srs.md §10.4).
func (m *Manager) applyManagerSnapshots(fed core.FederationName, snapshots map[string][]byte) error {
	if len(snapshots) == 0 || len(m.opts.ManagerSnapshots) == 0 {
		return nil
	}
	keys := make([]string, 0, len(snapshots))
	for k := range snapshots {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, key := range keys {
		snapper, ok := m.opts.ManagerSnapshots[key]
		if !ok || snapper == nil {
			continue
		}
		if err := snapper.Unmarshal(fed, snapshots[key]); err != nil {
			return fmt.Errorf("savepoint: %s.Unmarshal(%q): %w", key, fed, err)
		}
	}
	return nil
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
	return m.requestFederationRestore(ctx, fed, label, nil)
}

// RequestFederationRestoreBy is RequestFederationRestore carrying the
// REQUESTING federate's handle so the §4.25 requestFederationRestore
// Succeeded / Failed ack events can target it. The gRPC handler
// duck-types for this richer method (M37 Agent EA); the plain
// core.SavepointCoordinator method keeps its frozen shape and emits no
// ack.
func (m *Manager) RequestFederationRestoreBy(
	ctx context.Context,
	fed core.FederationName,
	requester core.FederateHandle,
	label string,
) error {
	return m.requestFederationRestore(ctx, fed, label, &requester)
}

// requestRestoreFailed emits the §4.25 failure ack to the requester (when
// known) and passes the causing error through.
func (m *Manager) requestRestoreFailed(
	ctx context.Context,
	fed core.FederationName,
	requester *core.FederateHandle,
	label string,
	err error,
) error {
	if requester != nil {
		_ = m.opts.Outbox.Send(ctx, fed, *requester,
			requestFederationRestoreFailedEvent(label, err.Error()))
	}
	return err
}

func (m *Manager) requestFederationRestore(
	ctx context.Context,
	fed core.FederationName,
	label string,
	requester *core.FederateHandle,
) error {
	if !m.opts.BundleStore.Exists(fed, label) {
		return m.requestRestoreFailed(ctx, fed, requester, label, ErrSaveBundleNotFound)
	}

	m.mu.Lock()
	if _, busy := m.restore[fed]; busy {
		m.mu.Unlock()
		return m.requestRestoreFailed(ctx, fed, requester, label, core.ErrRestoreAlreadyInProgress)
	}
	m.mu.Unlock()

	rdr, err := m.opts.BundleStore.Reader(fed, label)
	if err != nil {
		return m.requestRestoreFailed(ctx, fed, requester, label, err)
	}
	manifest, _, err := ReadBundle(rdr)
	_ = rdr.Close()
	if err != nil {
		return m.requestRestoreFailed(ctx, fed, requester, label,
			fmt.Errorf("savepoint: read bundle (%s/%s): %w", fed, label, err))
	}

	// M13 thread C: apply the per-manager state snapshots from the
	// manifest BEFORE the event-log slice replay runs. The snapshots
	// reconstruct the cut-2 service-group state (sync points,
	// ownership, MOM, DDM regions) without requiring a full replay;
	// the event-log slice is then replayed on top so any post-save
	// cut-3 events that lack a snapshot representation still land.
	// Old (pre-M13) bundles arrive with manifest.ManagerSnapshots
	// nil — applyManagerSnapshots is then a no-op and the restore
	// falls back to the cut-1 event-log-only path.
	if err := m.applyManagerSnapshots(fed, manifest.ManagerSnapshots); err != nil {
		return m.requestRestoreFailed(ctx, fed, requester, label,
			fmt.Errorf("savepoint: apply manager snapshots (%s/%s): %w", fed, label, err))
	}

	// M36 DB-3: route by federate NAME (IEEE 1516.1 §4.27). A saved
	// participant that resigned and rejoined carries the same name but
	// a fresh handle; the saved names are matched against the current
	// roster to find each participant's CURRENT handle. recipients is
	// index-parallel to manifest.Federates so the §4.13 payload can
	// still carry the handle the federate had at save time. Names that
	// are empty / unresolvable — and bundles or configurations without
	// name data at all — fall back to the saved handle.
	recipients := append([]core.FederateHandle(nil), manifest.Federates...)
	if m.opts.Roster != nil && len(manifest.FederateNames) == len(manifest.Federates) {
		byName := map[string]core.FederateHandle{}
		for _, mem := range m.opts.Roster(fed) {
			byName[mem.Name] = mem.Handle
		}
		for i, name := range manifest.FederateNames {
			if cur, ok := byName[name]; ok && name != "" {
				recipients[i] = cur
			}
		}
	}
	required := make(map[core.FederateHandle]struct{}, len(recipients))
	for _, h := range recipients {
		required[h] = struct{}{}
	}

	m.mu.Lock()
	if _, busy := m.restore[fed]; busy {
		// Lost the race; someone else took it between checks.
		m.mu.Unlock()
		return m.requestRestoreFailed(ctx, fed, requester, label, core.ErrRestoreAlreadyInProgress)
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

	// §4 state-machine ordering (M37 Agent EA):
	//   requestFederationRestoreSucceeded (requester only)
	//   → federationRestoreBegun (every joined federate)
	//   → initiateFederateRestore (each restore participant).
	if requester != nil {
		_ = m.opts.Outbox.Send(ctx, fed, *requester,
			requestFederationRestoreSucceededEvent(label))
	}
	for _, dst := range restoreBegunRecipients(m.opts.Roster, fed, recipients) {
		_ = m.opts.Outbox.Send(ctx, fed, dst, federationRestoreBegunEvent())
	}

	for i, dst := range recipients {
		// Payload carries the handle (and name, when the bundle has
		// one) this federate had AT SAVE TIME (§4.13/§4.26 semantics —
		// see stream.proto InitiateFederateRestore); the envelope
		// destination is the current handle. The two only differ for
		// participants remapped by name above.
		name := ""
		if len(manifest.FederateNames) == len(manifest.Federates) {
			name = manifest.FederateNames[i]
		}
		evt := initiateFederateRestoreEvent(label, manifest.Federates[i], name)
		_ = m.opts.Outbox.Send(ctx, fed, dst, evt)
	}
	return nil
}

// restoreBegunRecipients resolves "every joined federate" for the §4.26
// federationRestoreBegun broadcast: the live roster when wired, else the
// restore participant set (sorted for determinism).
func restoreBegunRecipients(
	roster func(core.FederationName) []core.FederationMember,
	fed core.FederationName,
	participants []core.FederateHandle,
) []core.FederateHandle {
	var out []core.FederateHandle
	if roster != nil {
		for _, mem := range roster(fed) {
			out = append(out, mem.Handle)
		}
	} else {
		seen := map[core.FederateHandle]struct{}{}
		for _, h := range participants {
			if _, dup := seen[h]; dup {
				continue
			}
			seen[h] = struct{}{}
			out = append(out, h)
		}
	}
	slices.Sort(out)
	return out
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

// Snapshot returns the federation's save / restore status for the
// AdminService handler. When no save or restore is in progress the
// state fields are StateIdle / RestoreIdle and the labels are empty.
// Read under the manager mutex; cheap.
//
// Phase 1 of the rtid-TUI plan (docs/rtid-tui.md): consumed by the
// drill-down view's "Save state: IDLE" line.
func (m *Manager) Snapshot(fed core.FederationName) core.SavepointSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := core.SavepointSnapshot{
		SaveState:    StateIdle,
		RestoreState: RestoreIdle,
	}
	if as, ok := m.saves[fed]; ok {
		out.SaveLabel = as.label
		out.SaveState = as.state
	}
	if ar, ok := m.restore[fed]; ok {
		out.RestoreLabel = ar.label
		out.RestoreState = ar.state
	}
	return out
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
