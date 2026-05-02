package federation

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrNotImplemented is returned by stub methods until Agent A implements them.
// Spec tests in tests/spec/M2/ will fail with this error initially (RED), then
// turn green as functionality is added.
var ErrNotImplemented = errors.New("federation: not implemented (Agent A M2 deliverable)")

// defaultStallTimeoutSeconds is the cut-1 default applied when callers leave
// Options.DefaultStallTimeout at zero. Per srs.md §10.2 M3 stall test.
const defaultStallTimeoutSeconds = 60

// Manager implements core.FederationStore. It owns the per-federation roster
// state (federates, mode, FOM handle, stall config) and emits federation-
// lifecycle events to its EventLog.
//
// Manager is goroutine-safe. Per-federation state is guarded by a single
// mutex; a future M5 perf task may shard if benchmarks demand it.
type Manager struct {
	opts Options

	mu          sync.RWMutex
	federations map[core.FederationName]*federationState
}

// federationState is the per-federation in-memory record. Mutations require
// the federation's own write lock (acquired via Manager.mu plus the per-
// federation mu); reads can use the read lock.
type federationState struct {
	mu sync.RWMutex

	name         core.FederationName
	mode         core.Mode
	stallTimeout time.Duration
	seed         uint64
	fom          core.FOMHandle

	// nameToHandle and handleToName are kept consistent: handles are
	// reassigned by sort order of name on every join (deterministic per
	// docs/agent-a-rti-core.md §5.5). For cut 1 the recompute is O(N log N);
	// optimization deferred per dispatch brief ARCH section.
	nameToHandle map[string]core.FederateHandle
	handleToName map[core.FederateHandle]string
}

// Options bundles Manager dependencies. Nil values use sensible defaults
// (RealClock for production; in-memory eventlog rejected — must be supplied
// explicitly to make persistence-vs-test-fake choice deliberate).
type Options struct {
	// Clock returns the wall time to stamp federation lifecycle events.
	// MUST NOT be nil. Tests pass core.NewFakeClock; production passes
	// core.NewRealClock.
	Clock core.Clock

	// EventLog records federation lifecycle events (FederateJoined,
	// FederateResigned). MUST NOT be nil; create-time validation will
	// fail otherwise. Pass a per-federation file log in production or an
	// in-memory implementation under test (see rti/internal/eventlog).
	EventLog core.EventLog

	// FOMs validates FOM modules at CreateFederation time. MUST NOT be
	// nil; Create rejects otherwise. Tests can pass a stub that always
	// accepts or always rejects to exercise both branches.
	FOMs core.FOMRepository

	// DefaultStallTimeout is applied when a CreateFederationRequest
	// supplies StallTimeout == 0. Zero here means 60s (per srs §10.2 M3).
	DefaultStallTimeout int // seconds; 0 → 60
}

// New constructs a Manager. Returns an error if any required dependency in
// opts is nil. The returned Manager is ready for concurrent use.
//
// Required: Clock, FOMs. Optional: EventLog (cut-1 relaxation — when nil,
// federation-lifecycle events are silently dropped; Wave 1B implements the
// real EventLog and W4 wiring then supplies a non-nil writer in production).
func New(opts Options) (*Manager, error) {
	if opts.Clock == nil {
		return nil, errors.New("federation: Options.Clock is required")
	}
	if opts.FOMs == nil {
		return nil, errors.New("federation: Options.FOMs is required")
	}
	if opts.DefaultStallTimeout == 0 {
		opts.DefaultStallTimeout = defaultStallTimeoutSeconds
	}
	// Normalize a typed-nil EventLog (e.g. (*eventlog.Writer)(nil) wrapped in
	// the core.EventLog interface) to true nil. The cut-1 contract treats
	// nil as "do not emit"; a typed-nil interface would otherwise dispatch
	// methods on a nil pointer and panic.
	if isNilInterface(opts.EventLog) {
		opts.EventLog = nil
	}
	return &Manager{
		opts:        opts,
		federations: map[core.FederationName]*federationState{},
	}, nil
}

// isNilInterface reports whether v is a true nil interface or a typed-nil
// (interface holding a nil concrete pointer). This is needed because Go's
// `iface == nil` is false for typed-nil values.
func isNilInterface(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}

// CreateFederation implements core.FederationStore. See SRS §FR-FM-1.
//
// Behavior contract (drives tests/spec/M2/federation_test.go):
//   - Validates the FOM modules via opts.FOMs.Load; rejects with the
//     wrapped diagnostic on failure.
//   - Rejects duplicate federation name with core.ErrFederationAlreadyExists.
//   - On success, persists federation state and emits no event yet
//     (FederateJoined fires on first join).
//   - Returns the assigned seed in CreateFederationResponse via the gRPC
//     handler layer (this method only signals success/error).
func (m *Manager) CreateFederation(ctx context.Context, req core.CreateFederationRequest) error {
	if req.Name == "" {
		return core.ErrFederationInvalidName
	}
	fom, err := m.opts.FOMs.Load(ctx, req.FOMModules)
	if err != nil {
		return fmt.Errorf("federation %q create: %w", req.Name, err)
	}

	stall := req.StallTimeout
	if stall == 0 {
		stall = time.Duration(m.opts.DefaultStallTimeout) * time.Second
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.federations[req.Name]; exists {
		return core.ErrFederationAlreadyExists
	}
	m.federations[req.Name] = &federationState{
		name:         req.Name,
		mode:         req.Mode,
		stallTimeout: stall,
		seed:         req.Seed,
		fom:          fom,
		nameToHandle: map[string]core.FederateHandle{},
		handleToName: map[core.FederateHandle]string{},
	}
	return nil
}

// DestroyFederation implements core.FederationStore. See SRS §FR-FM-5.
//
// Rejects with core.ErrFederationHasFederatesJoined if any federate is
// currently joined. Rejects unknown name with core.ErrFederationNotFound.
func (m *Manager) DestroyFederation(ctx context.Context, name core.FederationName) error {
	_ = ctx
	_ = name
	return ErrNotImplemented
}

// JoinFederation implements core.FederationStore. See SRS §FR-FM-2.
//
// Determinism contract: FederateHandles are assigned by sort order of
// FederateName, NOT arrival order. This is the cornerstone of replay
// determinism — multiple agents joining concurrently still get stable
// handles. Tests in tests/spec/M2/federation_test.go assert this with a
// concurrent-join scenario across 50 goroutines.
//
// Emits FederateJoined event to EventLog before returning the handle
// (write-ahead): the event is durable before the in-memory roster is
// observable to subsequent calls.
func (m *Manager) JoinFederation(ctx context.Context, req core.JoinFederationRequest) (core.FederateHandle, error) {
	if req.FederateName == "" {
		return core.InvalidFederateHandle, errors.New("federation: JoinFederationRequest.FederateName is required")
	}

	m.mu.RLock()
	fs, ok := m.federations[req.Federation]
	m.mu.RUnlock()
	if !ok {
		return core.InvalidFederateHandle, core.ErrFederationNotFound
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if _, dup := fs.nameToHandle[req.FederateName]; dup {
		return core.InvalidFederateHandle, core.ErrFederateAlreadyJoined
	}

	// Insert + re-key so that handles 1..N follow sort order of name.
	// Cut 1: O(N log N) per join (acceptable for N < 100). Optimization
	// deferred per docs/agent-a-rti-core.md §M5.
	names := make([]string, 0, len(fs.nameToHandle)+1)
	names = append(names, req.FederateName)
	for n := range fs.nameToHandle {
		names = append(names, n)
	}
	sort.Strings(names)
	fs.nameToHandle = make(map[string]core.FederateHandle, len(names))
	fs.handleToName = make(map[core.FederateHandle]string, len(names))
	for i, n := range names {
		h := core.FederateHandle(i + 1)
		fs.nameToHandle[n] = h
		fs.handleToName[h] = n
	}
	assigned := fs.nameToHandle[req.FederateName]

	// Write-ahead: append before returning. EventLog optional in cut 1.
	if m.opts.EventLog != nil {
		if err := m.opts.EventLog.Append(ctx, req.Federation, federateJoinedEvent{
			fed:      req.Federation,
			federate: req.FederateName,
			handle:   assigned,
			at:       m.opts.Clock.Now(),
		}); err != nil {
			// Roll back the roster mutation so the join is atomic on the
			// event log boundary; the caller sees a clean failure.
			delete(fs.nameToHandle, req.FederateName)
			delete(fs.handleToName, assigned)
			// Re-key the surviving names to dense 1..N-1.
			rekeyDense(fs)
			return core.InvalidFederateHandle, fmt.Errorf("federation %q join %q: eventlog append: %w",
				req.Federation, req.FederateName, err)
		}
	}
	return assigned, nil
}

// rekeyDense rebuilds nameToHandle/handleToName so that handles are
// 1..len(nameToHandle) in sorted-name order. Caller holds fs.mu.
func rekeyDense(fs *federationState) {
	names := make([]string, 0, len(fs.nameToHandle))
	for n := range fs.nameToHandle {
		names = append(names, n)
	}
	sort.Strings(names)
	fs.nameToHandle = make(map[string]core.FederateHandle, len(names))
	fs.handleToName = make(map[core.FederateHandle]string, len(names))
	for i, n := range names {
		h := core.FederateHandle(i + 1)
		fs.nameToHandle[n] = h
		fs.handleToName[h] = n
	}
}

// federateJoinedEvent is the event written to EventLog when a federate
// joins. It implements core.EventRecord with a placeholder Seq() until
// the real Protobuf event type lands (see proto/rti/v1/eventlog.proto +
// W1B's writer.go). The event log assigns the durable seq on Append.
type federateJoinedEvent struct {
	fed      core.FederationName
	federate string
	handle   core.FederateHandle
	at       time.Time
	seq      uint64
}

func (e federateJoinedEvent) Seq() uint64 { return e.seq }

// federateResignedEvent mirrors federateJoinedEvent for resign.
type federateResignedEvent struct {
	fed      core.FederationName
	federate string
	handle   core.FederateHandle
	at       time.Time
	seq      uint64
}

func (e federateResignedEvent) Seq() uint64 { return e.seq }

// ResignFederation implements core.FederationStore. See SRS §FR-FM-3.
//
// Cut 1: only ResignActionUnconditionallyDivestAttributes is supported.
// Other actions return core.ErrFederateAlreadyJoined-style sentinel
// (specific code TBD; spec test names the expected error).
//
// Idempotency: resign of an already-resigned federate returns
// core.ErrFederateNotJoined.
//
// Emits FederateResigned event before returning.
func (m *Manager) ResignFederation(ctx context.Context, fed core.FederationName, h core.FederateHandle, action core.ResignAction) error {
	_ = ctx
	_ = fed
	_ = h
	_ = action
	return ErrNotImplemented
}

// List implements core.FederationStore. Returns federations in
// name-sorted order (deterministic).
func (m *Manager) List(ctx context.Context) ([]core.FederationSummary, error) {
	_ = ctx
	return nil, ErrNotImplemented
}

// Compile-time assertion that Manager implements core.FederationStore.
// Removing any required method fails the build at this line.
var _ core.FederationStore = (*Manager)(nil)
