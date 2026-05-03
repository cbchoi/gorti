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
	opts   Options
	states *stateStore
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
//
// EventLog is optional in cut 1: when nil, time-management events are
// silently dropped (production wires the real log via cmd/rtid; tests
// pass a permissive in-memory log).
//
// StallTimeout defaults to DefaultStallTimeout (60s) when zero.
func New(opts Options) (*Manager, error) {
	if opts.Clock == nil {
		return nil, errors.New("time.New: Options.Clock is required")
	}
	if opts.Outbox == nil {
		return nil, errors.New("time.New: Options.Outbox is required")
	}
	if opts.StallTimeout == 0 {
		opts.StallTimeout = DefaultStallTimeout
	}
	return &Manager{
		opts:   opts,
		states: newStateStore(),
	}, nil
}

// EnableRegulation implements core.TimeManager. See SRS §FR-TM-1.
//
// Marks the federate as regulating with the given lookahead. Emits no
// event yet (TimeAdvanceRequested fires on the first NER call).
//
// Errors:
//   - core.ErrFederationHalted if the federation is in the halted
//     terminal state (set by CheckStalls)
//   - core.ErrTimeAlreadyRegulating if already regulating
//   - core.ErrTimeInvalidLookahead if lookahead < 0 or NaN
func (m *Manager) EnableRegulation(ctx context.Context, fed core.FederationName, h core.FederateHandle, lookahead core.LogicalTime) error {
	_ = ctx
	if extOf(m).isHalted(fed) {
		return core.ErrFederationHalted
	}
	return m.states.enableRegulation(fed, h, lookahead)
}

// DisableRegulation implements core.TimeManager. See SRS §FR-TM-1.
//
// Removes the federate from the regulating set; subsequent LBTS
// calculations exclude its contribution.
//
// Errors:
//   - core.ErrFederationHalted if the federation is in the halted
//     terminal state
//   - core.ErrTimeNotRegulating if not currently regulating
func (m *Manager) DisableRegulation(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	_ = ctx
	if extOf(m).isHalted(fed) {
		return core.ErrFederationHalted
	}
	return m.states.disableRegulation(fed, h)
}

// EnableConstrained implements core.TimeManager. Constrained federates
// receive TimeAdvanceGrant only when LBTS reaches their requested time.
//
// Errors:
//   - core.ErrFederationHalted if the federation is in the halted
//     terminal state
//   - core.ErrTimeAlreadyConstrained if already constrained
func (m *Manager) EnableConstrained(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	_ = ctx
	if extOf(m).isHalted(fed) {
		return core.ErrFederationHalted
	}
	return m.states.enableConstrained(fed, h)
}

// DisableConstrained implements core.TimeManager.
//
// Errors:
//   - core.ErrFederationHalted if the federation is in the halted
//     terminal state
//   - core.ErrTimeNotConstrained if not currently constrained
func (m *Manager) DisableConstrained(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	_ = ctx
	if extOf(m).isHalted(fed) {
		return core.ErrFederationHalted
	}
	return m.states.disableConstrained(fed, h)
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
	return m.nextMessageRequest(ctx, fed, h, t)
}

// NextMessageRequestAvailable implements core.TimeManager. See SRS §FR-TM-2 (M7).
//
// IEEE 1516.1-2010 §8.12: same as NER but the grant time may EQUAL LBTS
// (not strictly less). This allows the federate to receive messages
// time-stamped exactly at LBTS.
//
// FROZEN-shape: Agent A implements per the M7 wave model. Until then,
// returns ErrNotImplemented so spec tests in rti/spec/M7/ fail RED for
// the right reason.
func (m *Manager) NextMessageRequestAvailable(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime) error {
	_ = ctx
	_ = fed
	_ = h
	_ = t
	return ErrNotImplemented
}

// TimeAdvanceRequest implements core.TimeManager. See SRS §FR-TM-2 (M7).
//
// IEEE 1516.1-2010 §8.10: federate requests advance to t. Grant fires at
// min(t, LBTS) — strictly less than LBTS. Federate must process all
// TSO messages whose timestamp is ≤ grant time before the next request.
//
// FROZEN-shape: Agent A implements per the M7 wave model.
func (m *Manager) TimeAdvanceRequest(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime) error {
	_ = ctx
	_ = fed
	_ = h
	_ = t
	return ErrNotImplemented
}

// TimeAdvanceRequestAvailable implements core.TimeManager. See SRS §FR-TM-2 (M7).
//
// IEEE 1516.1-2010 §8.11: same as TAR but the grant time may EQUAL LBTS.
//
// FROZEN-shape: Agent A implements per the M7 wave model.
func (m *Manager) TimeAdvanceRequestAvailable(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime) error {
	_ = ctx
	_ = fed
	_ = h
	_ = t
	return ErrNotImplemented
}

// FlushQueueRequest implements core.TimeManager. See SRS §FR-TM-2 (M7).
//
// IEEE 1516.1-2010 §8.13: federate requests the RTI to flush its TSO
// queue up to t. The grant fires when the queue is drained. Useful for
// federates that need to "catch up" without advancing.
//
// FROZEN-shape: Agent A implements per the M7 wave model.
func (m *Manager) FlushQueueRequest(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime) error {
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
// Returns the count of federations halted in this poll (0 = no stalls).
func (m *Manager) CheckStalls(ctx context.Context) int {
	return m.checkStalls(ctx)
}

// Compile-time assertion that Manager implements core.TimeManager.
// Removing any required method fails the build at this line.
var _ core.TimeManager = (*Manager)(nil)
