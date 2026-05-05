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

	// OnTimeStateChanged is an OPTIONAL post-success hook invoked
	// whenever a federate's time-management state changes
	// (Enable/DisableRegulation, Enable/DisableConstrained). M11 wires
	// this to MOM.TimeStateChanged so HLAtimeRegulating /
	// HLAtimeConstrained / HLAlookahead reflect the live state.
	//
	// Cut-1: the lookahead snapshot is read from the manager's stateStore
	// AFTER the transition; logical time is reported as 0 because the
	// time.Manager does not yet track per-federate logical time
	// independently of grants (TASK roadmap: M7 NER landing carries the
	// per-federate currentTime field that this hook will eventually
	// surface).
	//
	// MUST NOT block; called synchronously after the state mutation.
	OnTimeStateChanged func(
		ctx context.Context,
		fed core.FederationName,
		h core.FederateHandle,
		regulating bool,
		constrained bool,
		lookahead core.LogicalTime,
		logicalTime core.LogicalTime,
	)

	// LBTSStrategy is the OPTIONAL algorithm hook for LBTS computation.
	// Nil → the package default (defaultLBTS, which delegates to the
	// exported LBTS function). See strategy.go for the interface and
	// docs/research-platform.md §6.1 for the design context.
	//
	// Phase 2a swap-point: production wires nil and gets unchanged
	// behavior; researchers wire an alternative impl through this slot.
	LBTSStrategy LBTSStrategy

	// GrantStrategy is the OPTIONAL algorithm hook for the time-advance
	// grant decision. Nil → the package default (defaultGrant, which
	// delegates to the unexported decideGrant function). See
	// strategy.go for the interface.
	//
	// Phase 2a swap-point: production wires nil and gets unchanged
	// behavior; researchers wire an alternative impl through this slot.
	GrantStrategy GrantStrategy
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
	// Strategy slots default to the package-default impls. nil → default
	// preserves existing call-site behavior; researchers override via
	// Options. See strategy.go.
	if opts.LBTSStrategy == nil {
		opts.LBTSStrategy = defaultLBTS{}
	}
	if opts.GrantStrategy == nil {
		opts.GrantStrategy = defaultGrant{}
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
	if extOf(m).isHalted(fed) {
		return core.ErrFederationHalted
	}
	if err := m.states.enableRegulation(fed, h, lookahead); err != nil {
		return err
	}
	m.fireTimeStateChanged(ctx, fed, h)
	return nil
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
	if extOf(m).isHalted(fed) {
		return core.ErrFederationHalted
	}
	if err := m.states.disableRegulation(fed, h); err != nil {
		return err
	}
	m.fireTimeStateChanged(ctx, fed, h)
	return nil
}

// EnableConstrained implements core.TimeManager. Constrained federates
// receive TimeAdvanceGrant only when LBTS reaches their requested time.
//
// Errors:
//   - core.ErrFederationHalted if the federation is in the halted
//     terminal state
//   - core.ErrTimeAlreadyConstrained if already constrained
func (m *Manager) EnableConstrained(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	if extOf(m).isHalted(fed) {
		return core.ErrFederationHalted
	}
	if err := m.states.enableConstrained(fed, h); err != nil {
		return err
	}
	m.fireTimeStateChanged(ctx, fed, h)
	return nil
}

// DisableConstrained implements core.TimeManager.
//
// Errors:
//   - core.ErrFederationHalted if the federation is in the halted
//     terminal state
//   - core.ErrTimeNotConstrained if not currently constrained
func (m *Manager) DisableConstrained(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	if extOf(m).isHalted(fed) {
		return core.ErrFederationHalted
	}
	if err := m.states.disableConstrained(fed, h); err != nil {
		return err
	}
	m.fireTimeStateChanged(ctx, fed, h)
	return nil
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
// Implementation delegates to dispatchAdvance with ModeNMRA; per-mode
// semantics live in advance.go::decideGrant. Returns ErrNotImplemented
// only via legacy code paths the dispatcher does not exercise.
func (m *Manager) NextMessageRequestAvailable(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime) error {
	return m.dispatchAdvance(ctx, fed, h, t, ModeNMRA)
}

// TimeAdvanceRequest implements core.TimeManager. See SRS §FR-TM-2 (M7).
//
// IEEE 1516.1-2010 §8.10: federate requests advance to t. Grant fires at
// min(t, LBTS) whenever LBTS produces forward progress: a full grant at
// t when LBTS > t (strict), an incremental grant at LBTS when LBTS < t
// and LBTS > currentTime. Pending always clears on grant — TAR is a
// "one request → one grant" primitive (the federate re-requests if it
// has not yet reached t).
//
// Implementation delegates to dispatchAdvance with ModeTAR.
func (m *Manager) TimeAdvanceRequest(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime) error {
	return m.dispatchAdvance(ctx, fed, h, t, ModeTAR)
}

// TimeAdvanceRequestAvailable implements core.TimeManager. See SRS §FR-TM-2 (M7).
//
// IEEE 1516.1-2010 §8.11: same as TAR but the grant time may EQUAL LBTS
// (not strictly less). The full-grant predicate is LBTS >= t; the
// incremental-grant path is identical.
//
// Implementation delegates to dispatchAdvance with ModeTARA.
func (m *Manager) TimeAdvanceRequestAvailable(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime) error {
	return m.dispatchAdvance(ctx, fed, h, t, ModeTARA)
}

// FlushQueueRequest implements core.TimeManager. See SRS §FR-TM-2 (M7).
//
// IEEE 1516.1-2010 §8.13: federate requests the RTI to flush its TSO
// queue up to t. The grant fires when the queue is drained.
//
// CUT-2 SIMPLIFICATION: the cut-1/cut-2 codebase has no real TSO queue
// (TSO message buffering is a cut-3 deliverable, see SRS §10.x for the
// roadmap). FQR therefore behaves like TAR with the inclusive-LBTS
// predicate (the queue is always trivially "empty" so the grant fires
// at min(t, LBTS) immediately). Federates that depend on FQR for
// reliable queue draining will get a degraded but correct grant in
// cut-2; cut-3 introduces the real drain semantics. The mode is recorded
// distinctly (ModeFQR) so cut-3 can extend without API churn.
//
// Implementation delegates to dispatchAdvance with ModeFQR.
func (m *Manager) FlushQueueRequest(ctx context.Context, fed core.FederationName, h core.FederateHandle, t core.LogicalTime) error {
	return m.dispatchAdvance(ctx, fed, h, t, ModeFQR)
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

// fireTimeStateChanged invokes the OnTimeStateChanged hook (when wired)
// with the federate's current regulating / constrained / lookahead
// snapshot. Logical time is reported as 0 in cut-1 because the
// time.Manager does not track per-federate currentTime independently of
// the grant pipeline; M11 cut-1 simplification documented in the agent
// report.
func (m *Manager) fireTimeStateChanged(ctx context.Context, fed core.FederationName, h core.FederateHandle) {
	if m.opts.OnTimeStateChanged == nil {
		return
	}
	snap := m.states.snapshot(fed, h)
	m.opts.OnTimeStateChanged(ctx, fed, h, snap.regulating, snap.constrained, snap.lookahead, 0)
}

// Snapshot returns the per-federation time-management view for the
// AdminService handler. Phase 1 of the rtid-TUI plan
// (docs/rtid-tui.md): consumed to populate the LBTS + per-federate
// {current_time, pending_request_time, lookahead, regulating,
// constrained} columns.
//
// Read order: stateStore (for regulating / constrained / lookahead),
// then the nerStore extension (for currentTime / pendingNER /
// requestedTime). Each lock is acquired briefly and released; the
// resulting snapshot is consistent per-federate but not strictly
// atomic across the two stores — sufficient for a read-only TUI.
func (m *Manager) Snapshot(fed core.FederationName) core.TimeSnapshot {
	// Step 1: collect every federate that has any state recorded for
	// fed, plus its regulation flags + lookahead.
	type rawState struct {
		regulating  bool
		constrained bool
		lookahead   core.LogicalTime
	}
	collected := map[core.FederateHandle]rawState{}
	m.states.mu.Lock()
	for k, st := range m.states.states {
		if k.fed != fed {
			continue
		}
		collected[k.h] = rawState{
			regulating:  st.regulating,
			constrained: st.constrained,
			lookahead:   st.lookahead,
		}
	}
	m.states.mu.Unlock()

	// Step 2: enrich with currentTime + pending state from the
	// extension store, and add any federate present only there.
	ext := extOf(m)
	ext.mu.Lock()
	defer ext.mu.Unlock()
	for k, ns := range ext.states {
		if k.fed != fed {
			continue
		}
		if _, ok := collected[k.h]; !ok {
			collected[k.h] = rawState{}
		}
		_ = ns
	}

	handles := make([]core.FederateHandle, 0, len(collected))
	for h := range collected {
		handles = append(handles, h)
	}
	// sort ascending (deterministic).
	for i := 1; i < len(handles); i++ {
		for j := i; j > 0 && handles[j-1] > handles[j]; j-- {
			handles[j-1], handles[j] = handles[j], handles[j-1]
		}
	}

	out := core.TimeSnapshot{
		Federates: make([]core.TimeFederateState, 0, len(handles)),
	}
	for _, h := range handles {
		raw := collected[h]
		fs := core.TimeFederateState{
			Handle:      h,
			Lookahead:   raw.lookahead,
			Regulating:  raw.regulating,
			Constrained: raw.constrained,
		}
		if ns := ext.states[federateKey{fed: fed, h: h}]; ns != nil {
			fs.CurrentTime = ns.currentTime
			fs.HasPendingRequest = ns.pendingNER
			fs.PendingRequestTime = ns.requestedTime
		}
		out.Federates = append(out.Federates, fs)
	}

	// Step 3: compute LBTS using the same regulator-snapshot logic as
	// nextMessageRequest. We can't call regulatingSnapshot inside the
	// ext.mu critical section (it takes ext.mu itself), so build the
	// regulator slice inline from `collected` + the already-loaded
	// pending state.
	var regSet []RegulatingFederate
	for _, h := range handles {
		raw := collected[h]
		if !raw.regulating {
			continue
		}
		floor := core.LogicalTime(0)
		if ns := ext.states[federateKey{fed: fed, h: h}]; ns != nil {
			floor = ns.currentTime
			if ns.pendingNER && float64(ns.requestedTime) > float64(floor) {
				floor = ns.requestedTime
			}
		}
		regSet = append(regSet, RegulatingFederate{
			Handle:    h,
			Time:      floor,
			Lookahead: raw.lookahead,
		})
	}
	if m.opts.LBTSStrategy != nil {
		out.LBTS = m.opts.LBTSStrategy.LBTS(regSet)
	} else {
		out.LBTS = LBTS(regSet)
	}
	return out
}

// Compile-time assertion that Manager implements core.TimeManager.
// Removing any required method fails the build at this line.
var _ core.TimeManager = (*Manager)(nil)
