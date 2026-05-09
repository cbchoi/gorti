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

	// M19 Phase 1a (docs/m19-dds-adapter.md §4.2): data-plane
	// transport selection recorded at CreateFederation time. The
	// gRPC handler resolves UNSPECIFIED → GRPC before the manager
	// sees it, so a federationState always carries a definite mode
	// once persisted. ddsDomainID is only meaningful when
	// transportMode == TransportModeDDS — it's stamped into
	// JoinFederationResponse.dds_domain_id by the transport layer.
	transportMode core.TransportMode
	ddsDomainID   int32

	// nameToHandle and handleToName are kept consistent. Handles are
	// assigned by a per-federation monotonic counter (nextFederateHandle):
	// every successful Join increments the counter and assigns the new
	// value as the federate's handle. Existing handles are NEVER reassigned
	// — once a federate has a handle, that handle is stable for the
	// federate's lifetime. This matches industry RTI behavior (Portico,
	// Pitch, MAK) and the behavior an online algorithm can guarantee.
	//
	// Determinism: replay re-reads the FederateJoined events from the
	// EventLog in append order and reassigns the same handles in the same
	// order. The per-call value depends only on the prior log content, not
	// on goroutine scheduling.
	nextFederateHandle uint64
	nameToHandle       map[string]core.FederateHandle
	handleToName       map[core.FederateHandle]string
	// joinedAt records the wall-clock instant each federate completed
	// JoinFederation. Phase-3 of the rtid-TUI plan
	// (docs/rtid-tui.md) feeds this through Snapshot →
	// FederationRoster.Federates[i].JoinedAt → AdminService's
	// FederateSnapshot.join_unix_seconds for the drilldown view's
	// `age` column. Maintained as a sibling map (rather than enriching
	// the existing handleToName) so resign / lookup paths stay
	// allocation-free for the hot path that doesn't need join time.
	joinedAt map[core.FederateHandle]time.Time

	// federateType records the optional HLAfederateType designator the
	// federate declared on JoinFederation (M13 thread B —
	// docs/srs.md §10.4). Empty string means "type not declared".
	// Sibling map for the same reason as joinedAt.
	federateType map[core.FederateHandle]string
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

	// OnFederateJoined is an OPTIONAL post-success hook fired after a
	// successful JoinFederation (after the eventlog append). M11 wires
	// this to MOM.FederateJoined so HLAfederate / HLAfederation
	// snapshots reflect the new federate. The hook receives the
	// resolved federation name, the assigned handle, the federate
	// name, and the optional federate-type string the federate
	// declared on its JoinFederationRequest (M13 thread B —
	// docs/srs.md §10.4; empty string when unset). MUST NOT block.
	OnFederateJoined func(ctx context.Context, fed core.FederationName, h core.FederateHandle, federateName string, federateType string)

	// OnFederateResigned is the resign-side analogue. M11 wires this
	// to MOM.FederateResigned. Fires after the eventlog append + roster
	// mutation. MUST NOT block.
	OnFederateResigned func(ctx context.Context, fed core.FederationName, h core.FederateHandle)

	// OnFederateResigning — M24 W2. Fires BEFORE the roster mutation
	// (after action validation) so cmd/rtid can dispatch action-aware
	// cleanup (release ownership, delete owned objects, cancel pending
	// acquires) before the federate disappears from the roster.
	// OPTIONAL: when nil, ResignFederation skips action-aware cleanup
	// and falls back to the legacy "remove from roster + run
	// OnFederateResigned" path. MUST NOT block.
	OnFederateResigning func(ctx context.Context, fed core.FederationName, h core.FederateHandle, action core.ResignAction)
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

	// TASK-076: normalize unspecified mode to verbose. The CLI / gRPC
	// front door MAY supply ModeUnspecified when the caller didn't pick
	// a mode (e.g. a default-constructed proto request); we collapse
	// that to ModeVerbose so the persisted federation always exposes
	// a definite mode through List(). BestEffort and Verbose pass
	// through unchanged so explicit choices are honored.
	mode := req.Mode
	if mode == core.ModeUnspecified {
		mode = core.ModeVerbose
	}

	// M19 Phase 1a: normalize TransportModeUnspecified → GRPC so
	// the persisted federation always exposes a definite transport.
	// The transport/grpc handler MAY also normalize before the
	// manager sees the request; doing it again here is harmless and
	// keeps direct in-process callers safe.
	tm := req.TransportMode
	if tm == core.TransportModeUnspecified {
		tm = core.TransportModeGRPC
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.federations[req.Name]; exists {
		return core.ErrFederationAlreadyExists
	}
	m.federations[req.Name] = &federationState{
		name:               req.Name,
		mode:               mode,
		stallTimeout:       stall,
		seed:               req.Seed,
		fom:                fom,
		transportMode:      tm,
		ddsDomainID:        req.DDSDomainID,
		nextFederateHandle: 0, // first Join produces handle 1
		nameToHandle:       map[string]core.FederateHandle{},
		handleToName:       map[core.FederateHandle]string{},
		joinedAt:           map[core.FederateHandle]time.Time{},
		federateType:       map[core.FederateHandle]string{},
	}
	return nil
}

// TransportFor returns the data-plane transport mode + DDS domain ID
// recorded at CreateFederation time. Returns (TransportModeUnspecified,
// 0, false) when no such federation exists. The transport layer's
// JoinFederation handler consults this to populate
// JoinFederationResponse.transport_mode + dds_domain_id so federates
// can pick the right wire path.
func (m *Manager) TransportFor(fed core.FederationName) (core.TransportMode, int32, bool) {
	m.mu.RLock()
	fs, ok := m.federations[fed]
	m.mu.RUnlock()
	if !ok {
		return core.TransportModeUnspecified, 0, false
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.transportMode, fs.ddsDomainID, true
}

// ModeFor returns the operating mode of a federation. Returns
// (ModeUnspecified, false) if no such federation exists. Used by the
// object registry's update path to resolve best-effort RO delivery.
func (m *Manager) ModeFor(fed core.FederationName) (core.Mode, bool) {
	m.mu.RLock()
	fs, ok := m.federations[fed]
	m.mu.RUnlock()
	if !ok {
		return core.ModeUnspecified, false
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.mode, true
}

// DestroyFederation implements core.FederationStore. See SRS §FR-FM-5.
//
// Rejects with core.ErrFederationHasFederatesJoined if any federate is
// currently joined. Rejects unknown name with core.ErrFederationNotFound.
func (m *Manager) DestroyFederation(_ context.Context, name core.FederationName) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	fs, ok := m.federations[name]
	if !ok {
		return core.ErrFederationNotFound
	}
	fs.mu.RLock()
	joinedCount := len(fs.handleToName)
	fs.mu.RUnlock()
	if joinedCount > 0 {
		return core.ErrFederationHasFederatesJoined
	}
	delete(m.federations, name)
	return nil
}

// JoinFederation implements core.FederationStore. See SRS §FR-FM-2.
//
// Determinism contract: FederateHandles are assigned by a per-federation
// monotonic counter — handle = nextFederateHandle++ for each successful
// join. Existing handles are NEVER reassigned: once a federate is at
// handle N, it stays at N for its lifetime in this federation. Resigned
// handles are NOT reused (the counter only goes up).
//
// This matches industry RTI behavior (Portico, Pitch, MAK) and is the
// only assignment scheme an online algorithm can guarantee — sort-order
// re-keying would require future knowledge of which names will arrive.
//
// Replay determinism (NFR-DET-1) is preserved by the EventLog: replay
// reads FederateJoined events in their durable order and reassigns the
// same handle to the same name. Concurrent goroutines serialize on the
// per-federation lock, so the channel that orders the inputs to
// JoinFederation is the channel that orders handle assignment.
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

	// Monotonic counter: every successful join gets the next value, never
	// reused. Existing federates are untouched — their handles stay stable
	// across other joins/resigns.
	fs.nextFederateHandle++
	assigned := core.FederateHandle(fs.nextFederateHandle)
	fs.nameToHandle[req.FederateName] = assigned
	fs.handleToName[assigned] = req.FederateName
	// Phase-3 rtid-TUI: stamp the wall-clock join time so the
	// AdminService Snapshot path can surface it as the drilldown view's
	// `age` column. Sourced from the same Clock the eventlog's
	// federateJoinedEvent.at uses below — both stamps therefore advance
	// in lock-step, including under FakeClock in tests.
	joinTime := m.opts.Clock.Now()
	fs.joinedAt[assigned] = joinTime
	// M13 thread B: stash the optional HLAfederateType string the
	// federate declared. Empty when unset; the MOM hook still receives
	// it (its own no-op when empty).
	if fs.federateType != nil {
		fs.federateType[assigned] = req.FederateType
	}

	// Write-ahead: append before returning. EventLog optional in cut 1.
	if m.opts.EventLog != nil {
		if err := m.opts.EventLog.Append(ctx, req.Federation, federateJoinedEvent{
			fed:      req.Federation,
			federate: req.FederateName,
			handle:   assigned,
			at:       joinTime,
		}); err != nil {
			// Roll back the roster mutation so the join is atomic on the
			// event log boundary; the caller sees a clean failure. The
			// counter is intentionally NOT decremented — handles are
			// never reused, so the next join skips this slot. This keeps
			// the eventlog's seq monotonic with the live roster's
			// nextFederateHandle even on append failure recovery.
			delete(fs.nameToHandle, req.FederateName)
			delete(fs.handleToName, assigned)
			delete(fs.joinedAt, assigned)
			delete(fs.federateType, assigned)
			return core.InvalidFederateHandle, fmt.Errorf("federation %q join %q: eventlog append: %w",
				req.Federation, req.FederateName, err)
		}
	}
	if m.opts.OnFederateJoined != nil {
		m.opts.OnFederateJoined(ctx, req.Federation, assigned, req.FederateName, req.FederateType)
	}
	return assigned, nil
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
// M24 W2: all 6 IEEE 1516.1-2010 §4.10 ResignAction values accepted
// (was 1). UNSPECIFIED returns ErrResignActionUnsupported
// (InvalidArgument at the wire). Per-action cleanup (release
// ownership, delete owned objects, cancel pending acquires) is
// dispatched via the OnFederateResigning hook BEFORE the roster
// mutation so subsequent reads against ownership / object see the
// resigned-federate-gone state consistently.
//
// Idempotency: resign of an already-resigned (or never-joined) federate
// returns core.ErrFederateNotJoined. Resign on an unknown federation
// returns core.ErrFederationNotFound.
//
// Emits FederateResigned event to EventLog BEFORE the roster mutation
// (write-ahead): on Append failure the resign is rejected and the roster
// is unchanged so the eventlog and in-memory state stay consistent.
func (m *Manager) ResignFederation(ctx context.Context, fed core.FederationName, h core.FederateHandle, action core.ResignAction) error {
	if action == core.ResignActionUnspecified {
		return core.ErrResignActionUnsupported
	}

	m.mu.RLock()
	fs, ok := m.federations[fed]
	m.mu.RUnlock()
	if !ok {
		return core.ErrFederationNotFound
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	name, joined := fs.handleToName[h]
	if !joined {
		return core.ErrFederateNotJoined
	}

	// M24 W2 — action-aware cleanup runs BEFORE the roster mutation.
	if m.opts.OnFederateResigning != nil {
		m.opts.OnFederateResigning(ctx, fed, h, action)
	}

	if m.opts.EventLog != nil {
		if err := m.opts.EventLog.Append(ctx, fed, federateResignedEvent{
			fed:      fed,
			federate: name,
			handle:   h,
			at:       m.opts.Clock.Now(),
		}); err != nil {
			return fmt.Errorf("federation %q resign %q: eventlog append: %w", fed, name, err)
		}
	}

	// Resign deletes the slot; the monotonic counter is NOT rolled back, so
	// resigned handles are never reused. This preserves replay determinism:
	// the FederateJoined event for handle h is durable, and replay sees
	// exactly the same handle-to-name binding even after the resign.
	delete(fs.nameToHandle, name)
	delete(fs.handleToName, h)
	delete(fs.joinedAt, h)
	delete(fs.federateType, h)
	if m.opts.OnFederateResigned != nil {
		m.opts.OnFederateResigned(ctx, fed, h)
	}
	return nil
}

// List implements core.FederationStore. Returns federations in
// name-sorted order (deterministic).
func (m *Manager) List(_ context.Context) ([]core.FederationSummary, error) {
	m.mu.RLock()
	names := make([]string, 0, len(m.federations))
	for n := range m.federations {
		names = append(names, string(n))
	}
	m.mu.RUnlock()
	sort.Strings(names)

	out := make([]core.FederationSummary, 0, len(names))
	for _, n := range names {
		m.mu.RLock()
		fs, ok := m.federations[core.FederationName(n)]
		m.mu.RUnlock()
		if !ok {
			// Concurrent destroy raced with List: skip silently.
			continue
		}
		fs.mu.RLock()
		out = append(out, core.FederationSummary{
			Name:            fs.name,
			Mode:            fs.mode,
			FederatesJoined: uint32(len(fs.handleToName)),
		})
		fs.mu.RUnlock()
	}
	return out, nil
}

// ListMembers returns every joined federate's (handle, name, type)
// for fed in handle-ascending order. Combines MembersOf +
// FederateTypeOf + the per-federate name lookup. M24 W3.
// Implements core.FederationStore.ListMembers.
func (m *Manager) ListMembers(fed core.FederationName) []core.FederationMember {
	m.mu.RLock()
	fs, ok := m.federations[fed]
	m.mu.RUnlock()
	if !ok {
		return []core.FederationMember{}
	}
	fs.mu.RLock()
	out := make([]core.FederationMember, 0, len(fs.handleToName))
	for h, name := range fs.handleToName {
		out = append(out, core.FederationMember{
			Handle:       h,
			Name:         name,
			FederateType: fs.federateType[h],
		})
	}
	fs.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Handle < out[j].Handle })
	return out
}

// MembersOf returns the snapshot of currently-joined federate handles
// for fed, sorted ascending. Returns an empty (non-nil) slice when no
// such federation exists, or when the federation has zero joined
// federates. M13 deliverable (docs/srs.md §10.4) — the production
// rtid wires this into sync.Options.MembersResolver and
// savepoint.Options.MembersResolver so the implicit "all joined
// federates" required-set resolution lands at request-time without
// dynamic-mode aggregation.
//
// Read under the manager RLock + per-federation RLock; cheap.
// Returned slice is freshly allocated and safe for the caller to retain.
func (m *Manager) MembersOf(fed core.FederationName) []core.FederateHandle {
	m.mu.RLock()
	fs, ok := m.federations[fed]
	m.mu.RUnlock()
	if !ok {
		return []core.FederateHandle{}
	}
	fs.mu.RLock()
	out := make([]core.FederateHandle, 0, len(fs.handleToName))
	for h := range fs.handleToName {
		out = append(out, h)
	}
	fs.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// FederateTypeOf returns the HLAfederateType string the federate
// declared on JoinFederation, plus a presence flag. Returns ("", false)
// for an unknown federation or unknown handle. M13 thread B
// (docs/srs.md §10.4): consumed by the cmd/rtid Snapshot path so the
// FederationRoster.Federates entries carry the type forward to the
// AdminService FederateSnapshot. An empty stored value still returns
// ("", true) — the federate is known but did not declare a type.
func (m *Manager) FederateTypeOf(fed core.FederationName, h core.FederateHandle) (string, bool) {
	m.mu.RLock()
	fs, ok := m.federations[fed]
	m.mu.RUnlock()
	if !ok {
		return "", false
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if _, joined := fs.handleToName[h]; !joined {
		return "", false
	}
	return fs.federateType[h], true
}

// Snapshot returns the federation roster for the AdminService
// handler. Phase 1 of the rtid-TUI plan (docs/rtid-tui.md): consumed
// to build per-federation FederationSnapshot entries with the
// per-federate (handle, name) seed.
//
// Federations are sorted by name; each roster's Federates slice is
// sorted by handle. Read under the manager RLock + per-federation
// RLock; cheap.
func (m *Manager) Snapshot() []core.FederationRoster {
	m.mu.RLock()
	names := make([]string, 0, len(m.federations))
	for n := range m.federations {
		names = append(names, string(n))
	}
	m.mu.RUnlock()
	sort.Strings(names)

	out := make([]core.FederationRoster, 0, len(names))
	for _, n := range names {
		m.mu.RLock()
		fs, ok := m.federations[core.FederationName(n)]
		m.mu.RUnlock()
		if !ok {
			continue
		}
		fs.mu.RLock()
		roster := core.FederationRoster{
			Name:          fs.name,
			Mode:          fs.mode,
			Federates:     make([]core.FederateInfo, 0, len(fs.handleToName)),
			TransportMode: fs.transportMode,
			DDSDomainID:   fs.ddsDomainID,
		}
		for h, fname := range fs.handleToName {
			roster.Federates = append(roster.Federates, core.FederateInfo{
				Handle:   h,
				Name:     fname,
				JoinedAt: fs.joinedAt[h], // zero for legacy entries; AdminService elides
				Type:     fs.federateType[h],
			})
		}
		fs.mu.RUnlock()
		sort.Slice(roster.Federates, func(i, j int) bool {
			return roster.Federates[i].Handle < roster.Federates[j].Handle
		})
		out = append(out, roster)
	}
	return out
}

// Compile-time assertion that Manager implements core.FederationStore.
// Removing any required method fails the build at this line.
var _ core.FederationStore = (*Manager)(nil)
