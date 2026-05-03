package sync

import (
	"context"
	"errors"
	"slices"
	gosync "sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrNotImplemented is retained as an exported sentinel for callers that
// matched on it during the M8 RED state. Implemented methods never return
// it; spec tests in rti/spec/M8/ use it to skip cleanly during pre-dispatch.
var ErrNotImplemented = errors.New("sync: not implemented (Agent A M8 deliverable)")

// SyncPointState reports the current state of a sync point in a
// federation. Returned by Manager.QueryState; mostly for tests + MOM
// (M11) to introspect state without reaching into internal storage.
//
// The "Sync" prefix on this type intentionally repeats the package
// name — the type is frozen-shape per docs/srs.md FR-SYN-1..4 + the
// orchestrator-frozen stub signature; renaming would break the M8
// contract.
//
//nolint:revive // frozen exported name; see doc above
type SyncPointState int

const (
	StateUnknown SyncPointState = iota
	// StateAnnounced — point registered; announce sent to all federates;
	// awaiting their achieved confirmations.
	StateAnnounced
	// StateAchieved — every required federate has called
	// synchronizationPointAchieved(label).
	StateAchieved
)

// MembersResolver returns the snapshot of currently joined federate
// handles for a federation. Optional; when non-nil, sync.Manager calls
// it at Register-time to materialize the implicit "all joined federates"
// required set when the caller passes nil requiredFederates.
//
// When MembersResolver is nil and the caller passes nil
// requiredFederates, the sync point is treated as "any federate that
// calls Achieve counts toward completion" — the federate's first
// Achieve call adds it to the required set, and the point is
// considered achieved as soon as that single federate calls Achieve.
// This is a documented cut-1 simplification for tests + early
// integration; production wires MembersResolver via cmd/rtid.
type MembersResolver func(core.FederationName) []core.FederateHandle

// Options bundles Manager dependencies. Required: Outbox, EventLog (for
// determinism — sync-point transitions are recorded so replay reproduces
// announce/achieve order byte-identically).
//
// EventLog is OPTIONAL in cut 1: when nil, sync transitions are not
// persisted. The proto Event types do not currently carry sync-point
// variants (FROZEN per orchestrator); production-grade WAL for sync
// transitions is tracked as the M8 W2 follow-up gap (FR-SYN-4 replay
// determinism). The fixtures' permissiveEventLog accepts any record so
// non-nil EventLog still receives Append calls.
type Options struct {
	Outbox   core.Outbox
	EventLog core.EventLog

	// Members resolves the current joined-federate snapshot for a
	// federation when Register is called with nil requiredFederates.
	// See MembersResolver doc for the nil-Members semantics.
	Members MembersResolver
}

// Manager owns per-federation sync-point state. Goroutine-safe.
//
// FROZEN-shape per docs/srs.md FR-SYN-1..4. Bodies implemented in M8 W1.
type Manager struct {
	opts Options

	mu  gosync.RWMutex
	fed map[core.FederationName]*federationState
}

// federationState holds all sync points for one federation.
type federationState struct {
	// points indexed by label.
	points map[string]*syncPoint
}

// syncPoint is the per-(federation, label) record.
type syncPoint struct {
	tag      []byte
	state    SyncPointState
	required map[core.FederateHandle]struct{} // nil set means "any" mode
	achieved map[core.FederateHandle]struct{}
	// dynamic is true when required was nil at Register-time AND
	// no MembersResolver was wired. In that case the required set is
	// grown lazily by Achieve calls; the sync point completes as soon
	// as the first federate Achieves.
	dynamic bool
}

// New constructs a Manager. Returns an error if Outbox is nil. EventLog
// is optional in cut 1 (see Options doc).
func New(opts Options) (*Manager, error) {
	if opts.Outbox == nil {
		return nil, errors.New("sync.New: Options.Outbox is required")
	}
	return &Manager{
		opts: opts,
		fed:  map[core.FederationName]*federationState{},
	}, nil
}

// stateForLocked returns (creating if needed) the per-federation state.
// Caller MUST hold m.mu (write lock).
func (m *Manager) stateForLocked(fed core.FederationName) *federationState {
	st, ok := m.fed[fed]
	if !ok {
		st = &federationState{points: map[string]*syncPoint{}}
		m.fed[fed] = st
	}
	return st
}

// Register implements IEEE 1516.1-2010 §4.6 — registerFederationSynchronizationPoint.
//
// label is the sync-point identifier. tag is opaque user data echoed in
// the announce callback. requiredFederates is the optional explicit set;
// when nil, the implicit set is derived per Options.Members semantics
// (see MembersResolver doc).
//
// On success, an announceSynchronizationPoint event is emitted via
// Outbox to every required federate (or, in dynamic mode, deferred
// until federates show up via Achieve).
//
// Errors:
//   - core.ErrSyncPointAlreadyRegistered if label already exists in fed
func (m *Manager) Register(
	ctx context.Context,
	fed core.FederationName,
	label string,
	tag []byte,
	requiredFederates []core.FederateHandle,
) error {
	m.mu.Lock()
	st := m.stateForLocked(fed)
	if _, exists := st.points[label]; exists {
		m.mu.Unlock()
		return core.ErrSyncPointAlreadyRegistered
	}

	var required map[core.FederateHandle]struct{}
	dynamic := false
	switch {
	case requiredFederates != nil:
		required = make(map[core.FederateHandle]struct{}, len(requiredFederates))
		for _, h := range requiredFederates {
			required[h] = struct{}{}
		}
	case m.opts.Members != nil:
		members := m.opts.Members(fed)
		required = make(map[core.FederateHandle]struct{}, len(members))
		for _, h := range members {
			required[h] = struct{}{}
		}
	default:
		required = map[core.FederateHandle]struct{}{}
		dynamic = true
	}

	// Defensive copy of tag — caller may mutate the slice.
	var tagCopy []byte
	if len(tag) > 0 {
		tagCopy = append([]byte(nil), tag...)
	}

	sp := &syncPoint{
		tag:      tagCopy,
		state:    StateAnnounced,
		required: required,
		achieved: map[core.FederateHandle]struct{}{},
		dynamic:  dynamic,
	}
	st.points[label] = sp

	// Snapshot the recipient list under the lock; release before
	// emitting Outbox / EventLog to avoid holding the manager mutex
	// across I/O.
	recipients := sortedHandles(required)
	m.mu.Unlock()

	// Best-effort EventLog append (cut-1: proto Event variants for sync
	// transitions are not yet defined; the call is a no-op when
	// EventLog is nil).
	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtRegistered, label: label})
	}

	// Fan out announceSynchronizationPoint. Cut-1: the proto
	// FederateEvent oneof does not yet carry an announce variant, so
	// every emission is a placeholder envelope. The test fixtures'
	// fakeOutbox simply counts calls; production wiring (gRPC handler
	// + proto extension) is M8 W2 follow-up.
	for _, h := range recipients {
		evt := announceEvent(label, tagCopy)
		_ = m.opts.Outbox.Send(ctx, fed, h, evt)
	}
	return nil
}

// Achieve implements IEEE 1516.1-2010 §4.7 —
// synchronizationPointAchieved. Called by a federate that has reached
// the sync point. When all required federates have called Achieve, the
// RTI emits federationSynchronized to all of them.
//
// Errors:
//   - core.ErrSyncPointNotRegistered if label doesn't exist
//   - core.ErrSyncPointAlreadyAchieved if this federate has already
//     achieved
func (m *Manager) Achieve(
	ctx context.Context,
	fed core.FederationName,
	h core.FederateHandle,
	label string,
) error {
	m.mu.Lock()
	st, ok := m.fed[fed]
	if !ok {
		m.mu.Unlock()
		return core.ErrSyncPointNotRegistered
	}
	sp, ok := st.points[label]
	if !ok {
		m.mu.Unlock()
		return core.ErrSyncPointNotRegistered
	}
	if _, already := sp.achieved[h]; already {
		m.mu.Unlock()
		return core.ErrSyncPointAlreadyAchieved
	}
	if sp.dynamic {
		// "any federate that calls Achieve counts" mode: the federate
		// joins the required set on its first Achieve.
		sp.required[h] = struct{}{}
	}
	sp.achieved[h] = struct{}{}

	allAchieved := len(sp.required) > 0 && len(sp.achieved) >= len(sp.required)
	if allAchieved {
		// Verify every required federate has actually achieved (the
		// sets may diverge if Achieve is called by a non-required
		// federate; guarded here because we only insert achieved
		// entries via this path).
		for req := range sp.required {
			if _, ok := sp.achieved[req]; !ok {
				allAchieved = false
				break
			}
		}
	}
	var recipients []core.FederateHandle
	if allAchieved {
		sp.state = StateAchieved
		recipients = sortedHandles(sp.required)
	}
	m.mu.Unlock()

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtAchieved, label: label, federate: h})
	}

	if allAchieved {
		if m.opts.EventLog != nil {
			_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtSynchronized, label: label})
		}
		for _, dst := range recipients {
			evt := synchronizedEvent(label)
			_ = m.opts.Outbox.Send(ctx, fed, dst, evt)
		}
	}
	return nil
}

// QueryState returns the state of (fed, label). StateUnknown if no such
// sync point exists. Used by tests + MOM (M11).
func (m *Manager) QueryState(fed core.FederationName, label string) SyncPointState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return StateUnknown
	}
	sp, ok := st.points[label]
	if !ok {
		return StateUnknown
	}
	return sp.state
}

// sortedHandles materializes a federate-handle set as a sorted slice
// for deterministic iteration order (announce + synchronized fan-out
// must be reproducible across replays).
func sortedHandles(set map[core.FederateHandle]struct{}) []core.FederateHandle {
	out := make([]core.FederateHandle, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	slices.Sort(out)
	return out
}
