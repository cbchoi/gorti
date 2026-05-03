package time

import (
	"context"
	"errors"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrNotImplemented is returned by stub methods until Agent A implements them.
// Spec tests in rti/spec/M3/ will fail with this error initially (RED), then
// turn green as functionality is added.
var ErrNotImplemented = errors.New("time: not implemented (Agent A M3 deliverable)")

// DefaultStallTimeout is the cut-1 stall timeout per docs/srs.md M3 exit
// criterion (60 seconds). Federations may override per-federation via
// CreateFederationRequest.StallTimeout.
const DefaultStallTimeout = 60 * stdtime.Second

// Manager implements core.TimeManager. It owns per-federation,
// per-federate time-management state: regulation flag + lookahead,
// constrained flag, current time, outstanding NER request, last-activity
// timestamp for stall detection.
//
// Manager is goroutine-safe. Per-federation state is guarded by a single
// mutex; a future M5 perf task may shard if benchmarks demand it.
type Manager struct {
	opts Options
}

// Options bundles Manager dependencies.
//
// Required: Clock, Outbox. Optional: EventLog (cut-1 relaxation — when
// nil, time-management events are silently dropped; production wires the
// real log via cmd/rtid). StallTimeout defaults to DefaultStallTimeout
// (60s) when zero.
type Options struct {
	// Clock supplies the wall time used by stall detection. MUST NOT be
	// nil. Tests pass core.NewFakeClock; production wires
	// core.NewRealClock through cmd/rtid.
	Clock core.Clock

	// Outbox delivers TimeAdvanceGrant + FederationHalted events to
	// federates. MUST NOT be nil.
	Outbox core.Outbox

	// EventLog records TimeAdvanceRequested + TimeAdvanceGranted +
	// FederationHalted events. Optional in cut-1; nil silently drops.
	EventLog core.EventLog

	// StallTimeout applies when no federation-specific override is set.
	// Zero → DefaultStallTimeout (60s). Per-federation overrides come
	// through CreateFederationRequest.StallTimeout via the federation
	// manager's wiring.
	StallTimeout stdtime.Duration
}

// New constructs a Manager. Returns an error if any required Options
// field is nil. The returned Manager is ready for concurrent use.
func New(opts Options) (*Manager, error) {
	return &Manager{opts: opts}, ErrNotImplemented
}

// EnableRegulation implements core.TimeManager. See SRS §FR-TM-1.
//
// Marks the federate as regulating with the given lookahead. Emits no
// event yet (TimeAdvanceRequested fires on the first NER call).
//
// Errors:
//   - core.ErrTimeAlreadyRegulating if already regulating
//   - core.ErrTimeInvalidLookahead if lookahead < 0 or NaN
func (m *Manager) EnableRegulation(ctx context.Context, fed core.FederationName, h core.FederateHandle, lookahead core.LogicalTime) error {
	_ = ctx
	_ = fed
	_ = h
	_ = lookahead
	return ErrNotImplemented
}

// DisableRegulation implements core.TimeManager. See SRS §FR-TM-1.
//
// Removes the federate from the regulating set; subsequent LBTS
// calculations exclude its contribution.
//
// Errors:
//   - core.ErrTimeNotRegulating if not currently regulating
func (m *Manager) DisableRegulation(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	_ = ctx
	_ = fed
	_ = h
	return ErrNotImplemented
}

// EnableConstrained implements core.TimeManager. Constrained federates
// receive TimeAdvanceGrant only when LBTS reaches their requested time.
//
// Errors:
//   - core.ErrTimeAlreadyConstrained if already constrained
func (m *Manager) EnableConstrained(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	_ = ctx
	_ = fed
	_ = h
	return ErrNotImplemented
}

// DisableConstrained implements core.TimeManager.
//
// Errors:
//   - core.ErrTimeNotConstrained if not currently constrained
func (m *Manager) DisableConstrained(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	_ = ctx
	_ = fed
	_ = h
	return ErrNotImplemented
}

// NextMessageRequest implements core.TimeManager. See SRS §FR-TM-2.
//
// Records the federate's request to advance to logical time t.
// Recomputes LBTS; if LBTS ≥ t, emits TimeAdvanceGrant via Outbox
// (write-ahead through EventLog if non-nil).
//
// Errors:
//   - core.ErrTimeNotRegulating if not regulating (only regulating
//     federates may NER per HLA semantics — actually, both regulating
//     AND constrained federates may NER; the spec test will exercise
//     both branches and Agent A reconciles).
//   - core.ErrTimeRequestInPast if t < currentTime + lookahead (lookahead
//     enforcement; TASK-044).
func (m *Manager) NextMessageRequest(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime) error {
	_ = ctx
	_ = fed
	_ = h
	_ = t
	return ErrNotImplemented
}

// CheckStalls is called by the rtid main loop (or directly by tests with
// FakeClock) to detect federates whose last NER timestamp is older than
// the federation's StallTimeout. On detection, emits FederationHalted to
// EventLog + Outbox naming the stalled federate.
//
// This is a NEW method (not part of core.TimeManager). It exists because
// stall detection is intrinsically time-based and the cleanest way to
// keep tests deterministic is to make stall checks explicit poll calls.
// Production wires a goroutine that calls this every second; tests call
// it after Clock.Advance.
//
// Returns the count of federates halted in this poll (0 = no stalls).
func (m *Manager) CheckStalls(ctx context.Context) int {
	_ = ctx
	return 0
}

// Compile-time assertion that Manager implements core.TimeManager.
// Removing any required method fails the build at this line.
var _ core.TimeManager = (*Manager)(nil)
