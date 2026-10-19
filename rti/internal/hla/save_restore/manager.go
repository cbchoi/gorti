package savepoint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	gosync "sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ErrNotImplemented is retained as an exported sentinel for callers that
// matched on it during the M9 RED state. Implemented methods never return
// it; spec tests in rti/spec/M9/ reference it for clean pre-dispatch skip.
var ErrNotImplemented = errors.New("savepoint: not implemented ( M9 deliverable)")

// ErrRestoreStateCommitted diagnoses failed terminal publication after import.
// The imported state remains applied; the terminal identity may be unknown.
var ErrRestoreStateCommitted = errors.New("savepoint: state committed; terminal callback reservation commit failed")

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
	// StateSaved — all federates reported complete; bundle written and the
	// successful coordinator decision committed for callback publication.
	StateSaved = core.SaveStateSaved
	// StateNotSaved — a participant, persistence or publication step failed.
	// An unproven bundle can remain after interruption or legacy fanout failure.
	StateNotSaved = core.SaveStateNotSaved
)

// RestoreState reports the current state of a restore in progress.
type RestoreState = core.RestoreState

const (
	// RestoreIdle — no restore in progress.
	RestoreIdle = core.SaveRestoreIdle
	// RestoreLoading means the bundle is being read and validated.
	RestoreLoading = core.SaveRestoreLoading
	// RestoreInitiated — initiateFederateRestore broadcast; awaiting
	// per-federate federateRestoreComplete responses.
	RestoreInitiated = core.SaveRestoreInitiated
	// RestoreCompleted — all federates reported complete; federationRestored emitted.
	RestoreCompleted = core.SaveRestoreCompleted
	// RestoreFailed means the protocol or publication failed; it does not imply
	// state rollback when ErrRestoreStateCommitted is returned.
	RestoreFailed = core.SaveRestoreFailed
)

// MembersResolver returns the snapshot of currently joined federate
// handles for a federation. Optional; when non-nil, savepoint.Manager
// calls it at RequestFederationSave-time to materialize the implicit
// "all joined federates" required set.
//
// When MembersResolver is nil, the Manager runs in cut-1 dynamic mode:
// any federate that calls FederateSaveBegun joins the required set; completion
// requires all participants that have begun to report an outcome. Production wires
// MembersResolver via cmd/rtid; the spec tests rely on the dynamic
// path (or seed an explicit set via the optional Required fields on
// the request structs in future revisions).
type MembersResolver func(core.FederationName) []core.FederateHandle

// HaltedResolver, when non-nil, lets the Manager refuse a save request
// against a halted federation (FR-SR-1 cross-check with M3
// federationHalted). Optional; when nil, halt-state is not consulted.
type HaltedResolver func(core.FederationName) bool

// ManagerSnapshotter is an adapter for service-owned checkpoint state. Marshal
// produces the bytes stored under its registered key. Production also requires
// PreparedSnapshotter so decoding cannot mutate live state before commit.
//
// Marshal MUST be byte-deterministic for a given in-memory state so
// the same federation produces byte-identical bundles across replays
// (NFR-DET-1). Unmarshal of nil/empty bytes MUST be a no-op so an
// absent manifest entry restores cleanly (e.g. an old M9-era bundle
// that pre-dates M13's per-manager snapshots).
type ManagerSnapshotter = core.CheckpointSnapshotter

// PreparedSnapshotter decodes a present manifest entry without mutating live
// state. Empty bytes represent an empty saved state, not an absent entry. The
// commit must not fail; it runs only after every snapshot is prepared.
type PreparedSnapshotter interface {
	PrepareRestore(fed core.FederationName, data []byte) (commit func(), err error)
}

// Options bundles Manager dependencies.
type OptionsOf[T core.LogicalTimeScalar] struct {
	// Outbox delivers initiateFederateSave / federationSaved /
	// initiateFederateRestore / federationRestored. MUST NOT be nil.
	Outbox core.Outbox

	// EventLog receives optional protocol markers. It is not a restore source;
	// this manager writes snapshot bundles with an empty event-log slice.
	EventLog core.EventLog

	// BundleStore is the persistence backend — typically a directory
	// path under which save bundles are written + read. MUST NOT be nil.
	BundleStore Storage

	// HistoryStore persists successful decisions independently of snapshots.
	// Optional for embedded compositions; when set, a ReservableOutbox is required.
	HistoryStore SaveHistoryStoreOf[T]

	// OnSaveCompleted observes a successful decision after terminal callback
	// publication. It runs under operation admission and must not reenter a
	// mutating save service. Reporting failures must not undo the saved decision.
	OnSaveCompleted func(context.Context, core.FederationName)

	// Members resolves the current joined-federate snapshot for a
	// federation when RequestFederationSave is called. See
	// MembersResolver doc for the nil-Members semantics.
	Members MembersResolver

	// Halted reports whether a federation is currently halted; the
	// Manager refuses RequestFederationSave when this returns true.
	// Optional.
	Halted HaltedResolver

	// ScheduledTime reserves a bounded logical-time checkpoint barrier. It must
	// return readiness and idempotent release functions. Callers hold checkpoint
	// admission before entering this dependency; it must not call back into us.
	ScheduledTime func(core.FederationName, T, []core.FederateHandle) (ready func() (bool, error), release func(), err error)
	// ResumeTime reevaluates retained requests after terminal save callbacks.
	// It runs under fresh checkpoint admission, outside the original RPC leases.
	ResumeTime func(context.Context, core.FederationName) error
	// ValidateSaveRequest checks an authenticated requester's exact time
	// promises before installing any scheduled save or replacing an old one.
	ValidateSaveRequest  func(core.FederationName, core.FederateHandle, T, []core.FederateHandle) error
	PrepareScheduledTime func(core.FederationName, T, []core.FederateHandle, bool) (core.ScheduledSaveReservation, error)
	PrepareImmediateTime func(core.FederationName, []core.FederateHandle, bool) (core.ScheduledSaveReservation, error)
	CheckpointLabels     core.CommittedCheckpointLabelResolver

	// PrepareRestoreTransition reserves same-execution identity rebinding and
	// callback epoch rotation. Commit follows prepared state commits and must be
	// infallible after reservation. Release must leave identities/queues intact.
	// Deliveries use current handles; the adapter routes them to rebound sessions.
	PrepareRestoreTransition func(context.Context, core.FederationName, []RestoreParticipant, []core.OutboxDelivery) (core.OutboxReservation, error)
	// The manifest-aware form takes precedence and can reserve fresh callback
	// epochs relative to persisted invocation identities before importing state.
	PrepareRestoreTransitionWithManifest func(context.Context, core.FederationName, ManifestOf[T], []RestoreParticipant, []core.OutboxDelivery) (core.OutboxReservation, error)
	// RebindRestore explicitly translates a validated saved execution into fresh
	// identities of the pinned current execution. It must not publish state.
	RebindRestore func(context.Context, core.FederationName, uint64, ManifestOf[T]) (ManifestOf[T], error)
	// ValidateClientState preflights every nonempty source checkpoint before
	// RebindRestore can reserve target handles. It must reject unknown formats
	// and malformed source identities without mutating state.
	ValidateClientState ClientStateValidator
	// ValidateRestoreCallbackRecovery reconciles source callback phases with
	// private SDK completed outcomes before snapshot preparation or target handle
	// allocation, for both same- and cross-generation restore. It must not mutate
	// state or infer a result for a running/unknown callback body.
	// It also runs on the assembled save manifest before opening a bundle writer.
	ValidateRestoreCallbackRecovery func(context.Context, core.FederationName, ManifestOf[T]) error
	// RebindClientState validates and translates a recognized private SDK format.
	// It must not mutate live SDK/RTI state or publish callbacks. Every nonempty
	// checkpoint requires this port during cross-generation restore; unknown or
	// malformed formats must return an error. Empty state bypasses the port.
	RebindClientState ClientStateRebinder
	// ValidateRestoreSnapshots checks references across all prepared managers
	// against the unpublished saved FOM (or the current FOM for legacy bundles).
	ValidateRestoreSnapshots func(core.FederationName, ManifestOf[T], core.FOMHandle) error

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
	// JoinedModules captures each saved participant's actual join baseline.
	JoinedModules func(context.Context, core.FederationName, core.FederateHandle) ([]core.FOMModule, error)

	// Generation and FOMSHA256 provide durable save-bundle provenance.
	// Generation enables strict v2 storage and exact-generation restore.
	// FOMSHA256 supplements that identity when available; restore validates it
	// before applying any manager snapshot.
	Generation func(core.FederationName) (uint64, bool)
	FOMSHA256  func(core.FederationName) (string, bool)
	// LogicalTimeImplementation resolves immutable creation-time time metadata.
	// Nil denotes the legacy Float64-only composition.
	LogicalTimeImplementation func(core.FederationName) (string, bool)

	// ManagerSnapshots is the keyed set of persistence adapters. An empty map
	// saves protocol metadata only; it cannot reconstruct RTI service state.
	ManagerSnapshots map[string]ManagerSnapshotter

	// RequiredSnapshots enables complete-state checkpoint validation. Every key
	// must be present and use a PreparedSnapshotter. Restore prepares state at
	// request time and commits only after every participant reports success.
	// The initial profile requires an unchanged roster; identity rebinding is
	// not implemented. An empty list retains legacy partial-bundle behavior.
	RequiredSnapshots []string
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

// StorageKey is the durable identity of a generation-aware save bundle.
// Generation zero is valid and remains distinct from legacy storage, which
// continues to use Storage's (federation, label) methods.
type StorageKey struct {
	Federation core.FederationName
	Generation uint64
	Label      string
}

// BundleWriter publishes a completed bundle on Close. Abort discards an
// incomplete write without making it visible to readers.
type BundleWriter interface {
	io.WriteCloser
	Abort() error
}

// GenerationStorage isolates bundles by exact federation generation. A
// Manager with Options.Generation configured requires this interface; legacy
// Storage methods are used only when no generation resolver is configured.
type GenerationStorage interface {
	WriterFor(key StorageKey) (BundleWriter, error)
	ReaderFor(key StorageKey) (io.ReadCloser, error)
	ExistsFor(key StorageKey) bool
}

// ErrSaveBundleExists / ErrSaveBundleNotFound are storage-layer
// sentinels separate from the M8/M11 core errors.
var (
	ErrSaveBundleExists       = errors.New("save bundle already exists for this label")
	ErrSaveBundleNotFound     = errors.New("save bundle not found")
	ErrSaveBundleIncompatible = errors.New("save bundle is incompatible with the live federation")
)

// Manager orchestrates the save/restore protocol per federation.
// Goroutine-safe.
//
// FROZEN-shape per docs/srs.md FR-SR-1..5.
type ManagerOf[T core.LogicalTimeScalar] struct {
	opts             OptionsOf[T]
	operations       gosync.Map // federation -> *sync.Mutex; never removed while callers may hold it
	admissionMu      gosync.Mutex
	admission        gosync.Map // federation -> *admissionEntry; includes holders and waiters
	pendingSchedules gosync.Map // federation -> *activeSave; release-path fast check
	pendingResumes   gosync.Map // federation -> *activeSave; terminal callback ordering

	mu      gosync.Mutex
	saves   map[core.FederationName]*activeSaveOf[T]
	restore map[core.FederationName]*activeRestoreOf[
	// completed retains the final SaveState for (fed, label) past
	// activeSave teardown so QuerySaveState can keep returning
	// StateSaved / StateNotSaved after federationSaved fires.
	T]

	completed map[saveKey]SaveState
	// completedRestore mirrors completed for restores.
	completedRestore map[saveKey]RestoreState
	history          map[SaveHistoryKey]SaveHistoryOf[T]
}

// Compile-time assertion: *Manager satisfies core.SavepointCoordinator.
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.5) introduced the interface; the SaveState / RestoreState type
// aliases above let the existing method signatures match it without
// any conversion.
var _ core.SavepointCoordinator = (*Manager)(nil)
var _ core.SavepointCoordinatorOf[int64] = (*ManagerOf[int64])(nil)

type saveKey struct {
	fed   core.FederationName
	label string
}

// activeSave is the per-federation in-flight save state.
type activeSaveOf[T core.LogicalTimeScalar] struct {
	callbackEntered map[core.FederateHandle]struct{}
	requester       core.FederateHandle
	participantTime core.ScheduledSaveReservation
	initiated       map[core.FederateHandle]struct{}
	label           string
	saveTime        *T
	state           SaveState
	required        map[core.FederateHandle]struct{}
	complete        map[core.FederateHandle]struct{}
	begun           map[core.FederateHandle]struct{}
	notComp         map[core.FederateHandle]struct{}
	dynamic         bool
	waiting         bool
	timeReady       func() (bool, error)
	releaseTime     func()
	generation      *uint64
	fomSHA256       string
	// manifest snapshot taken at request-time — federate list captured
	// for deterministic restore ordering.
	federates    []core.FederateHandle
	clientStates map[core.FederateHandle][]byte
}

// activeRestore is the per-federation in-flight restore state.
type activeRestoreOf[T core.LogicalTimeScalar] struct {
	callbackEntered  map[core.FederateHandle]struct{}
	label            string
	state            RestoreState
	required         map[core.FederateHandle]struct{}
	complete         map[core.FederateHandle]struct{}
	notComp          map[core.FederateHandle]struct{}
	postHandles      map[core.FederateHandle]core.FederateHandle
	manifest         ManifestOf[T]
	commit           func() error
	release          func()
	participants     []RestoreParticipant
	targetGeneration *uint64
	targetFOMSHA256  string
}

// New constructs a Manager. Returns an error if any required Options
// field is nil.
func NewOf[T core.LogicalTimeScalar](opts OptionsOf[T]) (*ManagerOf[T], error) {
	if opts.Outbox == nil {
		return nil, errors.New("savepoint.New: Options.Outbox is required")
	}
	if opts.BundleStore == nil {
		return nil, errors.New("savepoint.New: Options.BundleStore is required")
	}
	if opts.HistoryStore != nil {
		if _, ok := opts.Outbox.(core.ReservableOutbox); !ok {
			return nil, errors.New("savepoint.New: durable history requires a reservable outbox")
		}
	}
	if opts.Generation != nil {
		if _, ok := opts.BundleStore.(GenerationStorage); !ok {
			return nil, errors.New("savepoint.New: generation resolver requires generation-aware BundleStore")
		}
	}
	if err := validateSnapshotOptionsOf[T](opts); err != nil {
		return nil, err
	}
	opts.RequiredSnapshots = append([]string(nil), opts.RequiredSnapshots...)
	return &ManagerOf[T]{
		opts:             opts,
		saves:            map[core.FederationName]*activeSaveOf[T]{},
		restore:          map[core.FederationName]*activeRestoreOf[T]{},
		completed:        map[saveKey]SaveState{},
		completedRestore: map[saveKey]RestoreState{},
	}, nil
}

// OnFederationDestroyed drops active and completed save/restore protocol
// state. Persisted bundles remain owned by BundleStore and are not deleted.
func (m *ManagerOf[T]) OnFederationDestroyed(fed core.FederationName) {
	unlock := m.lockOperation(fed)
	defer unlock()
	m.mu.Lock()
	as := m.saves[fed]
	ar := m.restore[fed]
	delete(m.saves, fed)
	m.pendingSchedules.Delete(fed)
	delete(m.restore, fed)
	for key := range m.completed {
		if key.fed == fed {
			delete(m.completed, key)
		}
	}
	for key := range m.completedRestore {
		if key.fed == fed {
			delete(m.completedRestore, key)
		}
	}
	for key := range m.history {
		if key.Federation == fed {
			delete(m.history, key)
		}
	}
	m.mu.Unlock()
	m.releaseSaveTime(fed, as)
	m.pendingResumes.Delete(fed)
	releaseRestoreOf[T](ar)
}

// Serialize mutating protocol work, including persistence, without blocking
// status readers on disk I/O. Callbacks must not synchronously re-enter mutations.
func (m *ManagerOf[T]) lockOperation(fed core.FederationName) func() {
	value, _ := m.operations.LoadOrStore(fed, &gosync.Mutex{})
	mu := value.(*gosync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// --- Save protocol (FR-SR-1, FR-SR-2) -------------------------------------

// RequestFederationSave starts a save. The RTI broadcasts
// initiateFederateSave to all joined federates and transitions the
// federation to StateInitiated.
//
// nil saveTime requests an immediate save. A non-nil time reserves the configured
// bounded checkpoint barrier; ProgressScheduledSave starts callbacks when ready.
// This is not a complete implementation of IEEE timed-save service semantics.
//
// Errors:
//   - core.ErrSaveAlreadyInProgress if another save is in progress
//   - core.ErrFederationHalted if the optional Halted resolver returns true
func (m *ManagerOf[T]) RequestFederationSave(
	ctx context.Context,
	fed core.FederationName,
	label string,
	saveTime *T,
) error {
	return m.requestFederationSave(ctx, fed, core.InvalidFederateHandle, label, saveTime, false)
}

func (m *ManagerOf[T]) RequestFederationSaveFrom(ctx context.Context, fed core.FederationName, requester core.FederateHandle, label string, saveTime *T) error {
	return m.requestFederationSave(ctx, fed, requester, label, saveTime, true)
}

func (m *ManagerOf[T]) requestFederationSave(ctx context.Context, fed core.FederationName, requester core.FederateHandle, label string, saveTime *T, requestAware bool) error {
	unlock := m.lockOperation(fed)
	defer unlock()
	if err := m.validateSaveAvailability(ctx, fed); err != nil {
		return err
	}

	generation, fomSHA256, err := m.resolveProvenance(fed, false)
	if err != nil {
		return err
	}
	var members []core.FederateHandle
	if m.opts.Members != nil {
		members = m.opts.Members(fed)
	}
	if requestAware {
		if err := m.authorizeSaveRequest(fed, requester, saveTime, members); err != nil {
			return err
		}
	}

	previous, err := m.replaceableSave(fed)
	if err != nil {
		return err
	}
	timeReady, releaseTime, participantTime, err := m.reserveSaveTiming(ctx, fed, saveTime, members, previous != nil)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		if releaseTime != nil {
			releaseTime()
		}
		return err
	}

	as := m.newActiveSave(label, saveTime, members, participantTime)
	as.requester, as.timeReady, as.releaseTime = requester, timeReady, releaseTime
	as.generation, as.fomSHA256, as.participantTime = generation, fomSHA256, participantTime
	federates := as.federates
	var immediate core.OutboxReservation
	if previous != nil && saveTime == nil {
		immediate, err = m.reserveInitiation(ctx, fed, as, federates)
		if err != nil {
			if releaseTime != nil {
				releaseTime()
			}
			return err
		}
		defer immediate.Release()
	}
	m.installSave(fed, as, previous)
	return m.publishSaveInitiation(ctx, fed, as, immediate)
}

func (m *ManagerOf[T]) initiateSave(ctx context.Context, fed core.FederationName, as *activeSaveOf[T]) error {
	label, stCopy, recipients := as.label, as.saveTime, as.federates
	// Legacy dynamic compositions use the broadcast sentinel. Timed saves
	// require an explicit nonempty membership snapshot.
	if len(recipients) == 0 {
		evt := initiateFederateSaveEventOf[T](label, stCopy)
		if err := m.opts.Outbox.Send(ctx, fed, core.InvalidFederateHandle, evt); err != nil {
			m.finalizeSave(fed, label, true)
			return err
		}
	} else {
		for _, h := range recipients {
			evt := initiateFederateSaveEventOf[T](label, stCopy)
			if err := m.opts.Outbox.Send(ctx, fed, h, evt); err != nil {
				m.finalizeSave(fed, label, true)
				return errors.Join(err, m.emitSaveOutcome(ctx, fed, label, recipients, RTIUnableToSave))
			}
		}
	}
	return nil
}

// FederateSaveComplete records a federate's successful save. When all
// joined federates have called this OR FederateSaveNotComplete, the
// Manager closes out the save: writes the bundle, emits federationSaved.
func (m *ManagerOf[T]) FederateSaveComplete(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
) error {
	return m.recordFederateSave(ctx, fed, h, true, nil)
}

// FederateSaveNotComplete records a federate's failed save. The save
// closes out as federationNotSaved (FR-SR-2); bundle is NOT written.
func (m *ManagerOf[T]) FederateSaveNotComplete(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
) error {
	return m.recordFederateSave(ctx, fed, h, false, nil)
}

// recordFederateSave is the shared aggregation path for Complete /
// NotComplete. Returns core.ErrFederateNotInSave when no save is in
// flight for fed or the federate is not in the required set.
func (m *ManagerOf[T]) recordFederateSave(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
	ok bool,
	clientState []byte,
) error {
	unlock := m.lockOperation(fed)
	defer unlock()
	m.mu.Lock()
	as := m.saves[fed]
	m.mu.Unlock()
	if err := m.validateSaveReport(ctx, fed, as, h); err != nil {
		return err
	}
	closedOut, failed, label, saveTime, feds, generation, fomSHA256, err := m.markFederateAndAggregate(fed, h, ok, clientState)
	if err != nil {
		return err
	}
	if !closedOut {
		return nil
	}
	reason := SaveFailureNone
	var completion core.OutboxReservation
	var completionErr error
	if failed {
		reason = FederateReportedFailureDuringSave
	}
	if !failed {
		failed, reason, completion, completionErr = m.persistCompletedSave(ctx, fed, as, label, saveTime, feds, generation, fomSHA256)
		if completion != nil {
			defer completion.Release()
		}
	}
	if !failed && completion != nil {
		completionErr = m.commitSaveHistory(historyKey(fed, generation), SaveHistoryOf[T]{Known: true, HasSave: true, Name: label, Time: saveTime})
		if completionErr != nil {
			failed, reason = true, RTIUnableToSave
		}
	}
	if failed && completion != nil {
		completion.Release()
	}
	m.finalizeSave(fed, label, failed)
	if !failed && completion != nil {
		return m.publishSaveDecision(ctx, fed, label, completion)
	}
	return m.publishLegacySaveOutcome(ctx, fed, label, feds, generation, saveTime, reason, failed, completionErr)
}

// markFederateAndAggregate records the federate's response on the
// active save and returns the post-response aggregation summary.
//
// Returns (closedOut=false, ...) when the save is still awaiting
// further responses; (closedOut=true, failed, ...) when every required
// federate has responded.
func (m *ManagerOf[T]) markFederateAndAggregate(
	fed core.FederationName,
	h core.FederateHandle,
	ok bool,
	clientState []byte,
) (closedOut, failed bool, label string, saveTime *T, feds []core.FederateHandle, generation *uint64, fomSHA256 string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, busy := m.restore[fed]; busy {
		return false, false, "", nil, nil, nil, "", core.ErrRestoreAlreadyInProgress
	}
	as, busy := m.saves[fed]
	if !busy {
		return false, false, "", nil, nil, nil, "", core.ErrFederateNotInSave
	}
	if _, member := as.begun[h]; !member {
		return false, false, "", nil, nil, nil, "", core.ErrFederateNotInSave
	}
	if err := validateClientStateAdmissionOf[T](as, clientState); err != nil {
		return false, false, "", nil, nil, nil, "", err
	}
	delete(as.begun, h)
	if ok {
		as.complete[h] = struct{}{}
		if len(clientState) != 0 {
			if as.clientStates == nil {
				as.clientStates = make(map[core.FederateHandle][]byte)
			}
			as.clientStates[h] = append([]byte(nil), clientState...)
		}
	} else {
		as.notComp[h] = struct{}{}
	}
	if !allRequiredRespondedOf[T](as) {
		return false, false, "", nil, nil, nil, "", nil
	}
	failed = len(as.notComp) > 0
	// Keep INITIATED while persistence is in flight. SAVED is only observable
	// after a successfully published bundle, never merely after the last report.
	feds = append([]core.FederateHandle(nil), as.federates...)
	return true, failed, as.label, as.saveTime, feds, cloneGeneration(as.generation), as.fomSHA256, nil
}

// federateNamesFor resolves the federate name for each handle in feds
// (index-parallel result) from the current roster. Returns nil when no
// Roster resolver is wired — the manifest then omits federate_names
// and restore falls back to handle-based routing. A handle absent from
// the roster (should not happen while it is mid-save) yields an empty
// string, which restore treats as "no name known; route by handle".
// M36 DB-3.
func (m *ManagerOf[T]) federateNamesFor(fed core.FederationName, feds []core.FederateHandle) []string {
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
func allRequiredRespondedOf[T core.LogicalTimeScalar](as *activeSaveOf[T]) bool {
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
func (m *ManagerOf[T]) AbortSave(ctx context.Context, fed core.FederationName) error {
	unlock := m.lockOperation(fed)
	defer unlock()
	m.mu.Lock()
	as, ok := m.saves[fed]
	if !ok {
		m.mu.Unlock()
		return core.ErrSaveNotInProgress
	}
	label := as.label
	delete(m.saves, fed)
	m.pendingSchedules.Delete(fed)
	m.completed[saveKey{fed, label}] = StateNotSaved
	m.mu.Unlock()
	m.releaseSaveTime(fed, as)
	return m.emitSaveOutcome(ctx, fed, label, as.federates, SaveAborted)
}

// AbortRestore aborts an in-progress restore for fed. M24 W3 — IEEE
// 1516.1-2010 §4.30 abortFederationRestore. M17.25 (Cut-4) now also
// fans out federationNotRestored to every federate that received the
// initiateFederateRestore so they can clean up local state.
func (m *ManagerOf[T]) AbortRestore(ctx context.Context, fed core.FederationName) error {
	unlock := m.lockOperation(fed)
	defer unlock()
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
	releaseRestoreOf[T](ar)
	var deliveryErr error
	for _, dst := range recipients {
		evt := federationNotRestoredEvent(label, RestoreAborted)
		deliveryErr = errors.Join(deliveryErr, m.opts.Outbox.Send(ctx, fed, dst, evt))
	}
	return deliveryErr
}

// finalizeSave drops the active save entry and records the final state
// for QuerySaveState lookup after teardown.
func (m *ManagerOf[T]) finalizeSave(fed core.FederationName, label string, failed bool) {
	final := StateSaved
	if failed {
		final = StateNotSaved
	}
	m.mu.Lock()
	as := m.saves[fed]
	delete(m.saves, fed)
	m.pendingSchedules.Delete(fed)
	m.completed[saveKey{fed, label}] = final
	m.mu.Unlock()
	m.releaseSaveTime(fed, as)
}

// emitSaveOutcome appends the federation-level outcome to the event log
// (best-effort) and fans out federationSaved / federationNotSaved to
// every recipient.
func (m *ManagerOf[T]) emitSaveOutcome(
	ctx context.Context,
	fed core.FederationName,
	label string,
	feds []core.FederateHandle,
	reason SaveFailureReason,
) error {
	failed := reason != SaveFailureNone
	if m.opts.EventLog != nil {
		kind := evtFederationSaved
		if failed {
			kind = evtFederationNotSaved
		}
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: kind, label: label})
	}
	var deliveryErr error
	for _, dst := range feds {
		var evt core.OutboundEvent
		if failed {
			evt = federationNotSavedEvent(label, reason)
		} else {
			evt = federationSavedEvent(label)
		}
		deliveryErr = errors.Join(deliveryErr, m.opts.Outbox.Send(ctx, fed, dst, evt))
	}
	return deliveryErr
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
// Empty marshal results remain present: absent legacy entries and an explicitly
// saved empty state have different restore semantics.
func (m *ManagerOf[T]) collectManagerSnapshots(fed core.FederationName) (map[string][]byte, error) {
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
		out[key] = append([]byte{}, bytes...)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// applyManagerSnapshots prepares all production decoders before committing.
// Legacy implementations require reversible images and are applied first, with
// reverse rollback on failure. Missing/unknown entries retain legacy no-op
// behavior. Prepared commits must be infallible and independent of apply order.
func (m *ManagerOf[T]) applyManagerSnapshots(fed core.FederationName, snapshots map[string][]byte) error {
	if len(snapshots) == 0 || len(m.opts.ManagerSnapshots) == 0 {
		return nil
	}
	keys := make([]string, 0, len(snapshots))
	for k := range snapshots {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var commits []func()

	var legacy []reversible
	for _, key := range keys {
		snapper, ok := m.opts.ManagerSnapshots[key]
		if !ok || snapper == nil {
			continue
		}
		data := append([]byte(nil), snapshots[key]...)
		if prepared, ok := snapper.(PreparedSnapshotter); ok {
			commit, err := prepared.PrepareRestore(fed, data)
			if err != nil {
				return fmt.Errorf("savepoint: %s.PrepareRestore(%q): %w", key, fed, err)
			}
			if commit != nil {
				commits = append(commits, commit)
			}
			continue
		}
		if len(data) == 0 {
			return fmt.Errorf("savepoint: %s needs PrepareRestore for a present empty snapshot", key)
		}
		before, err := snapper.Marshal(fed)
		if err != nil {
			return fmt.Errorf("savepoint: capture rollback for %s: %w", key, err)
		}
		// Empty Unmarshal is explicitly a no-op, not a way to restore an
		// absent state. Such implementations must support preparation.
		if len(before) == 0 {
			return fmt.Errorf("savepoint: %s needs PrepareRestore for an empty rollback image", key)
		}
		legacy = append(legacy, reversible{key, snapper, append([]byte(nil), before...), data})
	}
	if err := applyLegacySnapshots(fed, legacy); err != nil {
		return err
	}
	for _, commit := range commits {
		commit()
	}
	return nil
}

// writeBundle serializes the manifest + event-log slice into the
// configured Storage. The bundle layout is documented in
// rti/internal/savepoint/manifest.go.
func (m *ManagerOf[T]) writeBundle(
	ctx context.Context,
	fed core.FederationName,
	label string,
	generation *uint64,
	manifest ManifestOf[T],
) error {
	if m.opts.ValidateRestoreCallbackRecovery != nil {
		if err := m.opts.ValidateRestoreCallbackRecovery(ctx, fed, cloneCrossGenerationManifestOf(manifest)); err != nil {
			return fmt.Errorf("%w: source callback recovery preflight: %w", ErrSaveBundleIncompatible, err)
		}
	}
	if m.opts.ValidateRestoreSnapshots != nil {
		if err := m.opts.ValidateRestoreSnapshots(fed, manifest, nil); err != nil {
			return fmt.Errorf("savepoint: inconsistent manager snapshots: %w", err)
		}
	}
	w, err := m.bundleWriter(fed, label, generation)
	if err != nil {
		return fmt.Errorf("savepoint: open bundle writer (%s/%s): %w", fed, label, err)
	}

	// Preserve bundle framing without suggesting that the separate audit log
	// can reconstruct state omitted from the registered snapshots.
	var eventLogBytes []byte

	manifest.EventLogBytes = uint64(len(eventLogBytes))
	if err := WriteBundleOf[T](w, manifest, eventLogBytes); err != nil {
		abortBundleWriter(w)
		return fmt.Errorf("savepoint: write bundle (%s/%s): %w", fed, label, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("savepoint: publish bundle (%s/%s): %w", fed, label, err)
	}
	return nil
}

// QuerySaveState returns the current save state for (fed, label).
func (m *ManagerOf[T]) QuerySaveState(fed core.FederationName, label string) SaveState {
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

// RequestFederationRestore loads and validates a bundle, then broadcasts
// initiateFederateRestore to all federates and transitions to
// RestoreInitiated.
//
// Errors:
//   - ErrSaveBundleNotFound
//   - core.ErrRestoreAlreadyInProgress
func (m *ManagerOf[T]) RequestFederationRestore(
	ctx context.Context,
	fed core.FederationName,
	label string,
) error {
	return m.requestFederationRestore(ctx, fed, label, nil, nil)
}

// RequestFederationRestoreBy is RequestFederationRestore carrying the
// REQUESTING federate's handle so the §4.25 requestFederationRestore
// Succeeded / Failed ack events can target it. The gRPC handler
// duck-types for this richer method (M37); the plain
// core.SavepointCoordinator method keeps its frozen shape and emits no
// ack.
func (m *ManagerOf[T]) RequestFederationRestoreBy(
	ctx context.Context,
	fed core.FederationName,
	requester core.FederateHandle,
	label string,
) error {
	return m.requestFederationRestore(ctx, fed, label, &requester, nil)
}

// requestRestoreFailed emits the §4.25 failure ack to the requester (when
// known) and passes the causing error through.
func (m *ManagerOf[T]) requestRestoreFailed(
	ctx context.Context,
	fed core.FederationName,
	requester *core.FederateHandle,
	label string,
	err error,
) error {
	if requester != nil {
		deliveryErr := m.opts.Outbox.Send(ctx, fed, *requester,
			requestFederationRestoreFailedEvent(label, err.Error()))
		if deliveryErr != nil {
			return errors.Join(err, deliveryErr)
		}
	}
	return err
}

func (m *ManagerOf[T]) requestFederationRestore(
	ctx context.Context,
	fed core.FederationName,
	label string,
	requester *core.FederateHandle,
	savedGeneration *uint64,
) error {
	ar, err := m.reserveRestoreRequest(fed, label)
	if err != nil {
		return err
	}
	unlock := m.lockOperation(fed)
	defer unlock()
	initiated := false
	m.mu.Lock()
	reserved := m.restore[fed] == ar
	m.mu.Unlock()
	if !reserved {
		return core.ErrRestoreNotInProgress
	}
	defer func() { m.releaseUninitiatedRestore(fed, ar, initiated) }()
	source, err := m.loadRestoreSource(ctx, fed, label, requester, savedGeneration)
	if err != nil {
		return err
	}
	manifest, generation, sourceGeneration, fomSHA256 := source.manifest, source.generation, source.sourceGeneration, source.fomSHA256
	sourceHandles := append([]core.FederateHandle(nil), manifest.Federates...)
	if generation != nil && sourceGeneration != nil && *generation != *sourceGeneration {
		if err := m.prepareCrossGeneration(ctx, fed, *generation, fomSHA256, &manifest, ar); err != nil {
			return m.requestRestoreFailed(ctx, fed, requester, label, err)
		}
	}
	if _, err := m.restoreRetractionHints(fed, manifest); err != nil {
		return m.requestRestoreFailed(ctx, fed, requester, label, err)
	}
	if len(m.opts.RequiredSnapshots) != 0 {
		ar.participants, err = m.restoreParticipants(fed, manifest)
		if err != nil {
			return m.requestRestoreFailed(ctx, fed, requester, label, err)
		}
	}

	recipients, required, postHandles, err := m.restoreRouting(fed, manifest)
	if err != nil {
		return m.requestRestoreFailed(ctx, fed, requester, label, err)
	}
	if len(m.opts.RequiredSnapshots) != 0 {
		ar.commit, ar.release, err = m.prepareCompleteManifest(fed, manifest)
	} else {
		err = m.applyManagerSnapshots(fed, manifest.ManagerSnapshots)
	}
	if err != nil {
		return m.requestRestoreFailed(ctx, fed, requester, label,
			fmt.Errorf("savepoint: prepare manager snapshots (%s/%s): %w", fed, label, err))
	}

	m.mu.Lock()
	ar.state = RestoreInitiated
	ar.required = required
	ar.complete = map[core.FederateHandle]struct{}{}
	ar.notComp = map[core.FederateHandle]struct{}{}
	ar.postHandles = postHandles
	ar.manifest = manifest
	m.completedRestore[saveKey{fed, label}] = RestoreInitiated
	initiated = true
	m.mu.Unlock()

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtRestoreRequested, label: label})
	}

	return m.notifyRestoreInitiated(ctx, fed, ar, requester, recipients, sourceHandles)
}

func (m *ManagerOf[T]) failRestoreNotification(ctx context.Context, fed core.FederationName, ar *activeRestoreOf[T], cause error) error {
	m.mu.Lock()
	delete(m.restore, fed)
	ar.commit = nil
	m.completedRestore[saveKey{fed, ar.label}] = RestoreFailed
	m.mu.Unlock()
	releaseRestoreOf[T](ar)
	for _, dst := range sortedHandles(ar.required) {
		cause = errors.Join(cause, m.opts.Outbox.Send(ctx, fed, dst,
			federationNotRestoredEvent(ar.label, RTIUnableToRestore)))
	}
	return cause
}

// resolveProvenance snapshots the live generation and FOM identity once for a
// save or restore request. A nil generation marks the legacy storage mode.
func (m *ManagerOf[T]) resolveProvenance(
	fed core.FederationName,
	requireFOM bool,
) (*uint64, string, error) {
	if m.opts.Generation == nil {
		return nil, "", nil
	}
	generation, ok := m.opts.Generation(fed)
	if !ok {
		return nil, "", core.ErrFederationNotFound
	}
	var fomSHA256 string
	if m.opts.FOMSHA256 != nil {
		var ok bool
		fomSHA256, ok = m.opts.FOMSHA256(fed)
		if !ok && requireFOM {
			return nil, "", core.ErrFederationNotFound
		}
	}
	return &generation, fomSHA256, nil
}

func cloneGeneration(generation *uint64) *uint64 {
	if generation == nil {
		return nil
	}
	value := *generation
	return &value
}

func (m *ManagerOf[T]) bundleWriter(
	fed core.FederationName,
	label string,
	generation *uint64,
) (io.WriteCloser, error) {
	if generation == nil {
		return m.opts.BundleStore.Writer(fed, label)
	}
	return m.opts.BundleStore.(GenerationStorage).WriterFor(StorageKey{
		Federation: fed,
		Generation: *generation,
		Label:      label,
	})
}

func (m *ManagerOf[T]) bundleReader(
	fed core.FederationName,
	label string,
	generation *uint64,
) (io.ReadCloser, error) {
	if generation == nil {
		return m.opts.BundleStore.Reader(fed, label)
	}
	return m.opts.BundleStore.(GenerationStorage).ReaderFor(StorageKey{
		Federation: fed,
		Generation: *generation,
		Label:      label,
	})
}

func (m *ManagerOf[T]) bundleExists(fed core.FederationName, label string, generation *uint64) bool {
	if generation == nil {
		return m.opts.BundleStore.Exists(fed, label)
	}
	return m.opts.BundleStore.(GenerationStorage).ExistsFor(StorageKey{
		Federation: fed,
		Generation: *generation,
		Label:      label,
	})
}

func abortBundleWriter(w io.WriteCloser) {
	if atomic, ok := w.(interface{ Abort() error }); ok {
		_ = atomic.Abort()
		return
	}
	_ = w.Close()
}

// validateRestoreProvenance runs before snapshot application. Generation-aware
// restores require a v2 manifest from the exact live generation. Matching FOM
// bytes never permit a cross-generation restore. Resolver-free configurations
// retain the legacy v1 behavior.
func (m *ManagerOf[T]) validateRestoreProvenance(
	fed core.FederationName,
	label string,
	generation *uint64,
	fomSHA256 string,
	manifest ManifestOf[T],
) error {
	if manifest.Federation != fed || manifest.Label != label {
		return fmt.Errorf("%w: requested %q/%q, bundle records %q/%q",
			ErrSaveBundleIncompatible, fed, label, manifest.Federation, manifest.Label)
	}
	if err := m.validateRestoreTimeImplementation(fed, manifest); err != nil {
		return err
	}
	_, hasSavedFOM := manifest.ManagerSnapshots["fom"]
	if hasSavedFOM {
		validator, ok := m.opts.ManagerSnapshots["fom"].(SnapshotDigestValidator)
		if !ok {
			return fmt.Errorf("%w: saved FOM digest validator missing", ErrSaveBundleIncompatible)
		}
		if err := validator.ValidateSnapshotDigest(manifest.ManagerSnapshots["fom"], manifest.FOMSHA256); err != nil {
			return fmt.Errorf("%w: saved FOM digest: %v", ErrSaveBundleIncompatible, err)
		}
	}
	if generation == nil {
		return nil
	}
	if manifest.Version != bundleFormatVersionV2 {
		return fmt.Errorf("%w: bundle format version %d is not generation-aware",
			ErrSaveBundleIncompatible, manifest.Version)
	}
	if manifest.FederationGeneration == nil {
		return fmt.Errorf("%w: bundle has no federation generation", ErrSaveBundleIncompatible)
	}
	if *manifest.FederationGeneration != *generation {
		return fmt.Errorf("%w: bundle generation %d != live generation %d",
			ErrSaveBundleIncompatible, *manifest.FederationGeneration, *generation)
	}
	if m.opts.FOMSHA256 != nil {
		if manifest.FOMSHA256 == "" {
			return fmt.Errorf("%w: bundle has no FOM digest", ErrSaveBundleIncompatible)
		}
		if !hasSavedFOM && !strings.EqualFold(manifest.FOMSHA256, fomSHA256) {
			return fmt.Errorf("%w: bundle FOM sha256 %s != live %s",
				ErrSaveBundleIncompatible, manifest.FOMSHA256, fomSHA256)
		}
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
func (m *ManagerOf[T]) FederateRestoreComplete(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
) error {
	return m.recordFederateRestore(ctx, fed, h, true)
}

// FederateRestoreNotComplete is a participant report, not a federation abort.
func (m *ManagerOf[T]) FederateRestoreNotComplete(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	return m.recordFederateRestore(ctx, fed, h, false)
}

func (m *ManagerOf[T]) recordFederateRestore(ctx context.Context, fed core.FederationName, h core.FederateHandle, successful bool) error {
	unlock := m.lockOperation(fed)
	defer unlock()
	ar, recipients, allDone, failed, err := m.markFederateRestore(fed, h, successful)
	if err != nil {
		return err
	}
	label := ar.label

	if !allDone {
		return nil
	}
	defer releaseRestoreOf[T](ar)
	var commitErr error
	var reason RestoreFailureReason
	var terminalReservation core.OutboxReservation
	terminalCommitFailed := false
	strict := len(m.opts.RequiredSnapshots) != 0
	failed, reason, terminalReservation, commitErr = m.prepareRestoreCompletion(ctx, fed, ar, recipients, failed)
	if terminalReservation != nil {
		defer terminalReservation.Release()
	}
	if !failed && ar.commit != nil {
		commitErr = m.commitPreparedRestore(ctx, fed, ar)
		if commitErr != nil {
			failed, reason = true, RTIUnableToRestore
		}
	}
	if terminalReservation != nil {
		if failed {
			// Return capacity before attempting failure notifications.
			terminalReservation.Release()
		} else if err := terminalReservation.Commit(); err != nil {
			// State has already changed. A reservation must not fail for capacity
			// or lifecycle reasons; an unexpected adapter error is not a rollback.
			terminalCommitFailed = true
			commitErr = fmt.Errorf("%w: %w", ErrRestoreStateCommitted, err)
		}
	}
	m.mu.Lock()
	ar.state = RestoreCompleted
	if failed || terminalCommitFailed {
		ar.state = RestoreFailed
	}
	delete(m.restore, fed)
	m.completedRestore[saveKey{fed, label}] = ar.state
	m.mu.Unlock()
	if terminalCommitFailed {
		// State is already imported and the terminal may have partly changed
		// identity. Do not fabricate completion or failure callbacks to old handles.
		return commitErr
	}

	return m.notifyRestoreCompleted(ctx, fed, ar, recipients, failed, strict, reason, commitErr)
}

// QueryRestoreState returns the current restore state for (fed, label).
func (m *ManagerOf[T]) QueryRestoreState(fed core.FederationName, label string) RestoreState {
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
func (m *ManagerOf[T]) Snapshot(fed core.FederationName) core.SavepointSnapshot {
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
// federate list and save time without applying any snapshot.
func (m *ManagerOf[T]) LoadManifest(fed core.FederationName, label string) (ManifestOf[T], error) {
	generation, _, err := m.resolveProvenance(fed, false)
	if err != nil {
		return ManifestOf[T]{}, err
	}
	rdr, err := m.bundleReader(fed, label, generation)
	if err != nil {
		return ManifestOf[T]{}, err
	}
	defer func() { _ = rdr.Close() }()
	manifest, _, err := ReadBundleOf[T](rdr)
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

func (m *ManagerOf[T]) authorizeSaveRequest(fed core.FederationName, requester core.FederateHandle, saveTime *T, members []core.FederateHandle) error {
	if requester == core.InvalidFederateHandle || !slices.Contains(members, requester) {
		return core.ErrFederateNotJoined
	}
	if saveTime != nil {
		if m.opts.ValidateSaveRequest == nil {
			return ErrScheduledSaveUnsupported
		}
		if err := m.opts.ValidateSaveRequest(fed, requester, *saveTime, members); err != nil {
			return err
		}
	}
	return nil
}

func (m *ManagerOf[T]) replaceableSave(fed core.FederationName) (*activeSaveOf[T], error) {
	m.mu.Lock()
	if _, busy := m.restore[fed]; busy {
		m.mu.Unlock()
		return nil, core.ErrRestoreAlreadyInProgress
	}
	previous := m.saves[fed]
	if previous != nil && (!previous.waiting || len(previous.initiated) != 0) {
		m.mu.Unlock()
		return nil, core.ErrSaveAlreadyInProgress
	}
	m.mu.Unlock()
	return previous, nil
}

func (m *ManagerOf[T]) reserveSaveTiming(ctx context.Context, fed core.FederationName, saveTime *T, members []core.FederateHandle, replacing bool) (func() (bool, error), func(), core.ScheduledSaveReservation, error) {
	var err error
	var timeReady func() (bool, error)
	var releaseTime func()
	var participantTime core.ScheduledSaveReservation
	if saveTime != nil {
		switch {
		case m.opts.PrepareScheduledTime != nil:
			participantTime, err = m.opts.PrepareScheduledTime(fed, *saveTime, members, replacing)
			if err == nil && participantTime == nil {
				err = ErrScheduledSaveUnsupported
			}
			if err == nil {
				releaseTime = participantTime.Release
			}
		case replacing:
			err = ErrScheduledSaveUnsupported
		default:
			timeReady, releaseTime, err = m.reserveSaveTime(ctx, fed, *saveTime, members)
		}
		if err != nil {
			return nil, nil, nil, err
		}
	}
	if saveTime == nil && m.opts.PrepareImmediateTime != nil {
		participantTime, err = m.opts.PrepareImmediateTime(fed, members, replacing)
		if err != nil {
			return nil, nil, nil, err
		}
		if participantTime == nil {
			return nil, nil, nil, ErrScheduledSaveUnsupported
		}
		releaseTime = participantTime.Release
	}
	return timeReady, releaseTime, participantTime, nil
}

func (m *ManagerOf[T]) newActiveSave(label string, saveTime *T, members []core.FederateHandle, participantTime core.ScheduledSaveReservation) *activeSaveOf[T] {
	var required map[core.FederateHandle]struct{}
	dynamic := false
	if m.opts.Members != nil {
		required = make(map[core.FederateHandle]struct{}, len(members))
		for _, h := range members {
			required[h] = struct{}{}
		}
	} else {
		required = map[core.FederateHandle]struct{}{}
		dynamic = true
	}

	// Defensive copy of saveTime — caller may reuse the pointee.
	var stCopy *T
	if saveTime != nil {
		t := *saveTime
		stCopy = &t
	}

	federates := sortedHandles(required)
	as := &activeSaveOf[T]{

		label:    label,
		saveTime: stCopy,
		state:    StateInitiated,
		required: required,
		complete: map[core.FederateHandle]struct{}{},
		begun:    map[core.FederateHandle]struct{}{},
		notComp:  map[core.FederateHandle]struct{}{},
		dynamic:  dynamic,
		waiting:  saveTime != nil,

		federates: federates,

		initiated: make(map[core.FederateHandle]struct{}),
	}
	if participantTime != nil && saveTime == nil {
		for _, h := range federates {
			as.initiated[h] = struct{}{}
		}
	}
	return as
}

func (m *ManagerOf[T]) installSave(fed core.FederationName, as, previous *activeSaveOf[T]) {
	if as.participantTime != nil {
		as.participantTime.Commit()
	}
	if previous != nil && previous.releaseTime != nil {
		previous.releaseTime()
		previous.releaseTime = nil
	}
	m.mu.Lock()
	if previous != nil {
		delete(m.completed, saveKey{fed, previous.label})
		m.pendingSchedules.Delete(fed)
	}
	m.saves[fed] = as
	m.completed[saveKey{fed, as.label}] = StateInitiated
	if as.waiting {
		m.pendingSchedules.Store(fed, as)
	}
	m.mu.Unlock()

}

type reversible struct {
	key           string
	snapper       ManagerSnapshotter
	before, after []byte
}

func applyLegacySnapshots(fed core.FederationName, legacy []reversible) error {
	for i, entry := range legacy {
		if err := entry.snapper.Unmarshal(fed, entry.after); err != nil {
			failure := fmt.Errorf("savepoint: %s.Unmarshal(%q): %w", entry.key, fed, err)
			// Include the failing implementation: it may mutate before returning.
			for j := i; j >= 0; j-- {
				if rollbackErr := legacy[j].snapper.Unmarshal(fed, legacy[j].before); rollbackErr != nil {
					failure = errors.Join(failure, fmt.Errorf("savepoint: rollback %s: %w", legacy[j].key, rollbackErr))
				}
			}
			return failure
		}
	}
	return nil
}

func (m *ManagerOf[T]) collectSaveContents(ctx context.Context, fed core.FederationName, feds []core.FederateHandle) (map[string][]byte, string, [][]core.FOMModule, error) {
	snapshots, snapErr := m.collectManagerSnapshots(fed)
	var timeImplementation string
	if snapErr == nil {
		timeImplementation, snapErr = m.timeImplementationFor(fed)
	}
	var modules [][]core.FOMModule
	if snapErr == nil && m.opts.JoinedModules != nil {
		modules = make([][]core.FOMModule, len(feds))
		for i, handle := range feds {
			var supplied []core.FOMModule
			supplied, snapErr = m.opts.JoinedModules(ctx, fed, handle)
			if snapErr != nil {
				break
			}
			modules[i] = cloneRestoreModules(supplied)
		}
	}
	return snapshots, timeImplementation, modules, snapErr
}

func (m *ManagerOf[T]) persistCompletedSave(ctx context.Context, fed core.FederationName, as *activeSaveOf[T], label string, saveTime *T, feds []core.FederateHandle, generation *uint64, fomSHA256 string) (failed bool, reason SaveFailureReason, completion core.OutboxReservation, completionErr error) {
	// Retain cleanup until the caller takes ownership, including a panicking adapter.
	handedOff := false
	defer func() {
		if !handedOff && completion != nil {
			completion.Release()
		}
	}()
	// M13 thread C: collect per-manager snapshots before sealing.
	// Errors from any single manager flip the save outcome to
	// failed (federationNotSaved) so federates do not believe the
	// bundle is intact when it isn't.
	snapshots, timeImplementation, modules, snapErr := m.collectSaveContents(ctx, fed, feds)
	if snapErr != nil {
		m.mu.Lock()
		if as, busy := m.saves[fed]; busy {
			as.state = StateNotSaved
		}
		m.mu.Unlock()
		failed = true
		reason = RTIUnableToSave
	}
	if !failed {
		completion, completionErr = m.reserveSaveTerminal(ctx, fed, label, feds)
		if completionErr != nil {
			failed, reason = true, RTIUnableToSave
		}
	}
	if !failed {
		manifest := ManifestOf[T]{
			Version:                       manifestVersion(generation),
			Federation:                    fed,
			FederationGeneration:          generation,
			FOMSHA256:                     fomSHA256,
			LogicalTimeImplementationName: timeImplementation,
			Label:                         label,
			SaveTime:                      saveTime,
			Federates:                     feds,
			FederateNames:                 m.federateNamesFor(fed, feds),
			FederateTypes:                 m.federateTypesFor(fed, feds),
			FederateModules:               modules,
			FederateClientStates:          clientStatesForOf[T](as, feds),
			ManagerSnapshots:              snapshots,
		}
		if err := m.writeBundle(ctx, fed, label, generation, manifest); err != nil {
			// Persistence failure flips the outcome to failed so
			// federates see federationNotSaved instead of a phantom
			// saved-state with no bundle on disk.
			m.mu.Lock()
			if as, busy := m.saves[fed]; busy {
				as.state = StateNotSaved
			}
			m.mu.Unlock()
			failed = true
			reason = RTIUnableToSave
		}
	}
	handedOff = true
	return failed, reason, completion, completionErr
}

func (m *ManagerOf[T]) publishSaveDecision(ctx context.Context, fed core.FederationName, label string, completion core.OutboxReservation) error {
	if err := completion.Commit(); err != nil {
		return fmt.Errorf("savepoint: successful save decision committed; callback reservation violated publication contract: %w", err)
	}
	if m.opts.OnSaveCompleted != nil {
		m.opts.OnSaveCompleted(ctx, fed)
	}
	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtFederationSaved, label: label})
	}
	return nil
}

type restoreSourceOf[T core.LogicalTimeScalar] struct {
	manifest                     ManifestOf[T]
	generation, sourceGeneration *uint64
	fomSHA256                    string
}

func (m *ManagerOf[T]) loadRestoreSource(ctx context.Context, fed core.FederationName, label string, requester *core.FederateHandle, savedGeneration *uint64) (restoreSourceOf[T], error) {
	fail := func(err error) (restoreSourceOf[T], error) {
		return restoreSourceOf[T]{}, m.requestRestoreFailed(ctx, fed, requester, label, err)
	}
	generation, fomSHA256, err := m.resolveProvenance(fed, true)
	if err != nil {
		return fail(err)
	}
	sourceGeneration, resolvedLabel, err := m.resolveRestoreSourceGeneration(ctx, fed, label, generation, savedGeneration)
	if err != nil {
		return fail(err)
	}
	if !m.bundleExists(fed, label, sourceGeneration) {
		return fail(ErrSaveBundleNotFound)
	}

	rdr, err := m.bundleReader(fed, label, sourceGeneration)
	if err != nil {
		return fail(err)
	}
	manifest, _, err := ReadBundleOf[T](rdr)
	_ = rdr.Close()
	if err != nil {
		return fail(
			fmt.Errorf("savepoint: read bundle (%s/%s): %w", fed, label, err))
	}
	if err := m.validateRestoreProvenance(fed, label, sourceGeneration, fomSHA256, manifest); err != nil {
		return fail(err)
	}
	if resolvedLabel && !strings.EqualFold(manifest.FOMSHA256, fomSHA256) {
		return fail(fmt.Errorf("%w: recreated-execution FDD differs from committed checkpoint", ErrSaveBundleIncompatible))
	}
	if err := validateClientStatesOf[T](manifest); err != nil {
		return fail(err)
	}
	if m.opts.ValidateRestoreCallbackRecovery != nil {
		if err := m.opts.ValidateRestoreCallbackRecovery(ctx, fed, cloneCrossGenerationManifestOf(manifest)); err != nil {
			return fail(fmt.Errorf("%w: callback recovery preflight: %w", ErrSaveBundleIncompatible, err))
		}
	}
	return restoreSourceOf[T]{manifest, generation, sourceGeneration, fomSHA256}, nil
}

func (m *ManagerOf[T]) resolveRestoreSourceGeneration(ctx context.Context, fed core.FederationName, label string, generation, savedGeneration *uint64) (*uint64, bool, error) {
	sourceGeneration := generation
	resolvedLabel := false
	if savedGeneration != nil {
		if generation == nil {
			return nil, false, ErrSaveBundleIncompatible
		}
		sourceGeneration = savedGeneration
	} else if generation != nil && m.opts.CheckpointLabels != nil {
		resolved, err := m.opts.CheckpointLabels.ResolveCommittedCheckpointLabel(ctx, fed, label, *generation)
		if err != nil {
			return nil, false, err
		}
		sourceGeneration = &resolved
		resolvedLabel = resolved != *generation
	}
	return sourceGeneration, resolvedLabel, nil
}

func (m *ManagerOf[T]) restoreRouting(fed core.FederationName, manifest ManifestOf[T]) ([]core.FederateHandle, map[core.FederateHandle]struct{}, map[core.FederateHandle]core.FederateHandle, error) {
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
	postHandles := make(map[core.FederateHandle]core.FederateHandle, len(recipients))
	for i, h := range recipients {
		if _, duplicate := required[h]; duplicate || h == core.InvalidFederateHandle {
			return nil, nil, nil, ErrSaveBundleIncompatible
		}
		required[h] = struct{}{}
		postHandles[h] = manifest.Federates[i]
	}
	if len(required) == 0 {
		return nil, nil, nil, ErrSaveBundleIncompatible
	}
	return recipients, required, postHandles, nil
}

func (m *ManagerOf[T]) notifyRestoreInitiated(ctx context.Context, fed core.FederationName, ar *activeRestoreOf[T], requester *core.FederateHandle, recipients, sourceHandles []core.FederateHandle) error {
	label, manifest := ar.label, ar.manifest
	// §4 state-machine ordering (M37):
	//   requestFederationRestoreSucceeded (requester only)
	//   → federationRestoreBegun (every joined federate)
	//   → initiateFederateRestore (each restore participant).
	if requester != nil {
		if err := m.opts.Outbox.Send(ctx, fed, *requester,
			requestFederationRestoreSucceededEvent(label)); err != nil {
			return m.failRestoreNotification(ctx, fed, ar, err)
		}
	}
	for _, dst := range restoreBegunRecipients(m.opts.Roster, fed, recipients) {
		if err := m.opts.Outbox.Send(ctx, fed, dst, federationRestoreBegunEvent()); err != nil {
			return m.failRestoreNotification(ctx, fed, ar, err)
		}
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
		evt := initiateFederateRestoreEvent(label, sourceHandles[i], name)
		if len(manifest.FederateClientStates) != 0 {
			evt.ClientState = append([]byte(nil), manifest.FederateClientStates[i]...)
		}
		if err := m.opts.Outbox.Send(ctx, fed, dst, evt); err != nil {
			return m.failRestoreNotification(ctx, fed, ar, err)
		}
	}
	return nil
}

func (m *ManagerOf[T]) reserveRestoreRequest(fed core.FederationName, label string) (*activeRestoreOf[T], error) {
	// Reserve before opening the bundle or touching any service state. A
	// competing request must fail without invoking a snapshotter at all.
	m.mu.Lock()
	if _, busy := m.saves[fed]; busy {
		m.mu.Unlock()
		return nil, core.ErrSaveAlreadyInProgress
	}
	if _, busy := m.restore[fed]; busy {
		m.mu.Unlock()
		return nil, core.ErrRestoreAlreadyInProgress
	}
	ar := &activeRestoreOf[T]{label: label, state: RestoreLoading}
	m.restore[fed] = ar
	m.mu.Unlock()
	return ar, nil
}

func (m *ManagerOf[T]) markFederateRestore(fed core.FederationName, h core.FederateHandle, successful bool) (*activeRestoreOf[T], []core.FederateHandle, bool, bool, error) {
	m.mu.Lock()
	if _, busy := m.saves[fed]; busy {
		m.mu.Unlock()
		return nil, nil, false, false, core.ErrSaveAlreadyInProgress
	}
	ar, busy := m.restore[fed]
	if !busy || ar.state != RestoreInitiated {
		m.mu.Unlock()
		return nil, nil, false, false, core.ErrFederateNotInRestore
	}
	if _, member := ar.required[h]; !member {
		m.mu.Unlock()
		return nil, nil, false, false, core.ErrFederateNotInRestore
	}
	_, complete := ar.complete[h]
	_, notComplete := ar.notComp[h]
	if complete || notComplete {
		m.mu.Unlock()
		return nil, nil, false, false, core.ErrFederateNotInRestore
	}
	if successful {
		ar.complete[h] = struct{}{}
	} else {
		ar.notComp[h] = struct{}{}
	}
	allDone := len(ar.required) > 0 && len(ar.complete)+len(ar.notComp) == len(ar.required)
	failed := len(ar.notComp) > 0
	var recipients []core.FederateHandle
	if allDone {
		recipients = sortedHandles(ar.required)
	}
	m.mu.Unlock()
	return ar, recipients, allDone, failed, nil
}

func (m *ManagerOf[T]) prepareRestoreCompletion(ctx context.Context, fed core.FederationName, ar *activeRestoreOf[T], recipients []core.FederateHandle, failed bool) (bool, RestoreFailureReason, core.OutboxReservation, error) {
	var commitErr error
	var terminalReservation core.OutboxReservation
	reason := FederateReportedFailureDuringRestore
	var retractionHints map[core.FederateHandle]uint64
	strict := len(m.opts.RequiredSnapshots) != 0
	// Resolve live provenance before the transition adapter may reserve membership
	// or delivery locks. Checkpoint admission keeps this validation stable.
	if !failed && ar.commit != nil {
		commitErr = ctx.Err()
		if commitErr == nil {
			commitErr = m.validateRestoreTarget(fed, ar)
		}
		if commitErr != nil {
			failed = true
			reason = RTIUnableToRestore
		}
	}
	if !failed && strict {
		retractionHints, commitErr = m.restoreRetractionHints(fed, ar.manifest)
		if commitErr != nil {
			failed = true
			reason = RTIUnableToRestore
		}
	}
	if !failed && strict {
		terminalReservation, commitErr = m.reservePreparedRestoreTerminal(ctx, fed, ar, recipients, retractionHints)
		if commitErr != nil {
			if terminalReservation != nil {
				terminalReservation.Release()
				terminalReservation = nil
			}
			failed = true
			reason = RTIUnableToRestore
		}
	}
	return failed, reason, terminalReservation, commitErr
}

func (m *ManagerOf[T]) reservePreparedRestoreTerminal(ctx context.Context, fed core.FederationName, ar *activeRestoreOf[T], recipients []core.FederateHandle, retractionHints map[core.FederateHandle]uint64) (core.OutboxReservation, error) {
	label := ar.label
	var terminalReservation core.OutboxReservation
	var commitErr error
	if m.opts.hasRestoreTransition() {
		current, err := m.restoreParticipants(fed, ar.manifest)
		if err != nil {
			commitErr = err
		} else if !reflect.DeepEqual(current, ar.participants) {
			commitErr = fmt.Errorf("%w: restore participants changed during protocol", ErrSaveBundleIncompatible)
		}
		if commitErr == nil {
			deliveries := make([]core.OutboxDelivery, 0, len(recipients))
			for _, recipient := range recipients {
				event := federationRestoredEvent(label)
				event.NextRetractionHandle = retractionHints[ar.postHandles[recipient]]
				deliveries = append(deliveries, core.OutboxDelivery{Recipient: recipient, Event: event})
			}
			participants := append([]RestoreParticipant(nil), ar.participants...)
			if m.opts.PrepareRestoreTransitionWithManifest != nil {
				terminalReservation, commitErr = m.opts.PrepareRestoreTransitionWithManifest(ctx, fed, ar.manifest, participants, deliveries)
			} else {
				terminalReservation, commitErr = m.opts.PrepareRestoreTransition(ctx, fed, participants, deliveries)
			}
			if commitErr == nil && terminalReservation == nil {
				commitErr = fmt.Errorf("savepoint: nil restore transition reservation")
			}
		}
	} else {
		terminalReservation, commitErr = m.reserveRestoreTerminal(ctx, fed, label, recipients, retractionHints)
	}
	return terminalReservation, commitErr
}

func (m *ManagerOf[T]) commitPreparedRestore(ctx context.Context, fed core.FederationName, ar *activeRestoreOf[T]) error {
	commitErr := ctx.Err()
	// Only an identity transition pins the roster. Avoid its reserved lock.
	if commitErr == nil && !m.opts.hasRestoreTransition() {
		commitErr = m.validateRestoreTarget(fed, ar)
	}
	if commitErr != nil {
		return commitErr
	}
	return ar.commit()
}

func (m *ManagerOf[T]) notifyRestoreCompleted(ctx context.Context, fed core.FederationName, ar *activeRestoreOf[T], recipients []core.FederateHandle, failed, strict bool, reason RestoreFailureReason, commitErr error) error {
	label := ar.label
	if m.opts.EventLog != nil && !failed {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtFederationRestored, label: label})
	}
	if !failed && strict {
		if commitErr == nil && m.opts.ResumeTime != nil {
			// A scheduled checkpoint can contain an original crossing request.
			// Resume only after the restored identity/epoch terminal is published
			// and the transport has released its old-handle membership lease.
			m.pendingResumes.Store(fed, &activeSaveOf[T]{generation: ar.manifest.FederationGeneration})
		}
		return commitErr
	}
	for _, dst := range recipients {
		var evt core.OutboundEvent = federationRestoredEvent(label)
		if failed {
			evt = federationNotRestoredEvent(label, reason)
		}
		commitErr = errors.Join(commitErr, m.opts.Outbox.Send(ctx, fed, dst, evt))
	}
	return commitErr
}

func (m *ManagerOf[T]) publishSaveInitiation(ctx context.Context, fed core.FederationName, as *activeSaveOf[T], immediate core.OutboxReservation) error {
	// Best-effort EventLog marker for replay determinism — the proto
	// Event variants for save transitions are not yet defined (cut-1),
	// so this is a no-op when EventLog is nil. The marker shape mirrors
	// sync.eventRecord (empty body, seq-only).
	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtSaveRequested, label: as.label})
	}
	if as.waiting {
		return nil
	}
	if immediate != nil {
		if err := immediate.Commit(); err != nil {
			immediate.Release()
			return m.failParticipantInitiation(ctx, fed, as, err)
		}
		return nil
	}
	return m.initiateSave(ctx, fed, as)
}

func (m *ManagerOf[T]) publishLegacySaveOutcome(ctx context.Context, fed core.FederationName, label string, feds []core.FederateHandle, generation *uint64, saveTime *T, reason SaveFailureReason, failed bool, completionErr error) error {
	deliveryErr := m.emitSaveOutcome(ctx, fed, label, feds, reason)
	if !failed && deliveryErr == nil {
		// Legacy non-reservable embeddings have only process-local evidence,
		// and only after every fallible Send succeeds.
		m.setSaveHistory(historyKey(fed, generation), SaveHistoryOf[T]{Known: true, HasSave: true, Name: label, Time: saveTime})
		if m.opts.OnSaveCompleted != nil {
			m.opts.OnSaveCompleted(ctx, fed)
		}
	} else if !failed {
		m.mu.Lock()
		m.completed[saveKey{fed, label}] = StateNotSaved
		m.mu.Unlock()
	}
	return errors.Join(completionErr, deliveryErr)
}

func (m *ManagerOf[T]) releaseUninitiatedRestore(fed core.FederationName, ar *activeRestoreOf[T], initiated bool) {
	if !initiated {
		releaseRestoreOf[T](ar)
		m.mu.Lock()
		if m.restore[fed] == ar {
			delete(m.restore, fed)
			m.completedRestore[saveKey{fed, ar.label}] = RestoreFailed
		}
		m.mu.Unlock()
	}
}

func (m *ManagerOf[T]) validateSaveAvailability(ctx context.Context, fed core.FederationName) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.opts.Halted != nil && m.opts.Halted(fed) {
		return core.ErrFederationHalted
	}
	return nil
}

func (m *ManagerOf[T]) validateSaveReport(ctx context.Context, fed core.FederationName, as *activeSaveOf[T], h core.FederateHandle) error {
	if as != nil && as.saveTime != nil && saveParticipantStarted(as, h) {
		if err := m.validateScheduledIdentity(fed, as); err != nil {
			return m.failScheduledSave(ctx, fed, as, err)
		}
	}
	return nil
}
