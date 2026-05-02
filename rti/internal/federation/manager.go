package federation

import (
	"context"
	"errors"
	"fmt"
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
	return &Manager{
		opts:        opts,
		federations: map[core.FederationName]*federationState{},
	}, nil
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
// Emits FederateJoined event to EventLog before returning the handle.
func (m *Manager) JoinFederation(ctx context.Context, req core.JoinFederationRequest) (core.FederateHandle, error) {
	_ = ctx
	_ = req
	return core.InvalidFederateHandle, ErrNotImplemented
}

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
