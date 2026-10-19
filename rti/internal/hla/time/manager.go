package time

import (
	"context"
	"errors"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ErrNotImplemented is returned by methods that are still stubs.
// Spec tests in rti/spec/M3/ will fail with this error initially (RED), then
// turn green as functionality is added.
var ErrNotImplemented = errors.New("time: not implemented ( M3 deliverable)")

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
type ManagerOf[T core.LogicalTimeScalar] struct {
	opts          OptionsOf[T]
	states        *stateStoreOf[T]
	retractions   retractionIDStoreOf[T]
	deletions     core.DeletionLifecycle
	messageFilter core.QueuedMessageFilter
}

// Options bundles Manager dependencies.
//
// Required: Clock, Outbox. Optional: EventLog (cut-1 relaxation — when
// nil, time-management events are silently dropped; production wires the
// real log via cmd/rtid). StallTimeout defaults to DefaultStallTimeout
// (60s) when zero.
type OptionsOf[T core.LogicalTimeScalar] struct {
	// Clock supplies time for stall detection and elapsed state accounting.
	// Production readings retain time.Time's monotonic component. MUST NOT be
	// nil. Tests pass core.NewManualClock; production wires
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
		lookahead T,
		logicalTime T,
	)

	// LBTSStrategy is the OPTIONAL algorithm hook for LBTS computation.
	// Nil → the package default (defaultLBTS, which delegates to the
	// exported LBTS function). See strategy.go for the interface and
	// docs/research-platform.md §6.1 for the design context.
	//
	// Phase 2a swap-point: production wires nil and gets unchanged
	// behavior; researchers wire an alternative impl through this slot.
	LBTSStrategy LBTSStrategyOf[T]

	// GrantStrategy is the OPTIONAL algorithm hook for the time-advance
	// grant decision. Nil → the package default (defaultGrant, which
	// delegates to the unexported decideGrant function). See
	// strategy.go for the interface.
	//
	// Phase 2a swap-point: production wires nil and gets unchanged
	// behavior; researchers wire an alternative impl through this slot.
	GrantStrategy GrantStrategyOf[T]
}

// New constructs a Manager. Returns an error if any required Options
// field is nil. The returned Manager is ready for concurrent use.
//
// EventLog is optional in cut 1: when nil, time-management events are
// silently dropped (production wires the real log via cmd/rtid; tests
// pass a permissive in-memory log).
//
// StallTimeout defaults to DefaultStallTimeout (60s) when zero.
func NewOf[T core.LogicalTimeScalar](opts OptionsOf[T]) (*ManagerOf[T], error) {
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
		opts.LBTSStrategy = defaultLBTSOf[T]{}
	}
	if opts.GrantStrategy == nil {
		opts.GrantStrategy = defaultGrantOf[T]{}
	}
	return &ManagerOf[T]{
		opts:   opts,
		states: newStateStoreOf[T](),
	}, nil
}

// EnableRegulation accepts an asynchronous 8.2 request. The federate becomes
// regulating only when TimeRegulationEnabled is admitted at its safe time.
func (m *ManagerOf[T]) EnableRegulation(ctx context.Context, fed core.FederationName, h core.FederateHandle, lookahead T) error {
	return m.requestEnable(ctx, fed, h, true, lookahead)
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
func (m *ManagerOf[T]) DisableRegulation(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	ext := extOfOf[T](m)
	lease := ext.evaluatorLock(fed)
	lease.Lock()
	if ext.isHalted(fed) {
		lease.Unlock()
		return core.ErrFederationHalted
	}
	if err := m.states.disableRegulation(fed, h); err != nil {
		lease.Unlock()
		return err
	}
	lease.Unlock()
	m.fireTimeStateChanged(ctx, fed, h)
	// M36 DB-2: shrinking the regulating set can only raise LBTS —
	// re-evaluate peers' pending advance requests (same liveness gap
	// as OnFederateResign).
	return m.tryGrantPending(ctx, fed)
}

// OnFederateResign drops any pending advance-request state for the
// resigning federate. M21 TASK-204c — see docs/M21_DISPATCH_PLAN.md §2.4.2.
//
// Wired from federation.Manager's OnFederateResigned hook so the time
// manager's nerStore stays consistent with the federation roster: a
// resigned federate's pending NER/TAR/TARA/NMRA/FQR is cleared, plus
// its regulating/constrained/lookahead state in the stateStore.
//
// Safe to call for federates that never registered any time state —
// the cleanup is idempotent (delete on map miss is a no-op).
//
// M36 DB-2: after the cleanup the pending grants of the SURVIVING
// federates are re-evaluated. Removing a regulator can only raise
// LBTS, so a peer's outstanding NER/TAR/... that was blocked on the
// resigning federate becomes grantable the moment it leaves — without
// this re-run the peer hangs forever (traced via tm_tso_ordering /
// parity-CE). Errors from the grant loop are best-effort here (the
// resign itself already succeeded and this hook returns nothing);
// outbox failures surface through the stream layer.
func (m *ManagerOf[T]) OnFederateResign(ctx context.Context, fed core.FederationName, h core.FederateHandle) {
	// Drop nerStore entry (pending request + currentTime).
	ext := extOfOf[T](m)
	lease := ext.evaluatorLock(fed)
	lease.Lock()
	m.states.mu.Lock()
	ext.mu.Lock()
	delete(ext.states, federateKey{fed: fed, h: h})
	ext.mu.Unlock()
	// Drop stateStore entry (regulating/constrained/lookahead).
	delete(m.states.states, federateKey{fed: fed, h: h})
	m.states.mu.Unlock()
	m.ForgetRetractionRecipient(fed, h)
	lease.Unlock()
	// Re-evaluate pending grants against the post-resign LBTS.
	_ = m.tryGrantPending(ctx, fed)
}

// OnFederationDestroyed removes all time, halt, buffer, and evaluator state
// owned by a federation. It is idempotent and runs after the roster is empty.
func (m *ManagerOf[T]) OnFederationDestroyed(fed core.FederationName) {
	m.retractions.mu.Lock()
	delete(m.retractions.federations, fed)
	m.retractions.mu.Unlock()
	ext := extOfOf[T](m)
	ext.mu.Lock()
	delete(ext.stallTimeouts, fed)
	delete(ext.scheduledSaves, fed)
	delete(ext.deliveryRetries, fed)
	for key := range ext.states {
		if key.fed == fed {
			delete(ext.states, key)
		}
	}
	ext.mu.Unlock()
	m.states.mu.Lock()
	for key := range m.states.states {
		if key.fed == fed {
			delete(m.states.states, key)
		}
	}
	m.states.mu.Unlock()
	ext.haltedMu.Lock()
	delete(ext.halted, fed)
	ext.haltedMu.Unlock()
	ext.evalMu.Lock()
	delete(ext.evalLocks, fed)
	ext.evalMu.Unlock()
}

// ModifyLookahead rejects an advancing federate. Increases take effect
// immediately; decreases take effect as grants advance logical time (8.19).
func (m *ManagerOf[T]) ModifyLookahead(ctx context.Context, fed core.FederationName, h core.FederateHandle, lookahead T) error {
	ext := extOfOf[T](m)
	if ext.isHalted(fed) {
		return core.ErrFederationHalted
	}
	if !validLookaheadOf[T](lookahead) {
		return core.ErrTimeInvalidLookahead
	}
	lease := ext.evaluatorLock(fed)
	lease.Lock()
	m.states.mu.Lock()
	ext.mu.Lock()
	fs := m.states.getLocked(fed, h)
	ns := ext.getLocked(fed, h)
	var err error
	switch {
	case fs == nil || !fs.regulating:
		err = core.ErrTimeNotRegulating
	case ns != nil && ns.pendingNER:
		err = core.ErrTimeAdvancingState
	case lookahead >= fs.lookahead:
		fs.lookahead = lookahead
		fs.lookaheadTarget = 0
		fs.reducingLookahead = false
		fs.lookaheadOriginTime, fs.lookaheadOriginValue = 0, 0
	default:
		if !fs.reducingLookahead {
			fs.lookaheadOriginValue = fs.lookahead
			fs.lookaheadOriginTime = 0
			if ns != nil {
				fs.lookaheadOriginTime = ns.currentTime
			}
		}
		fs.lookaheadTarget = lookahead
		fs.reducingLookahead = true
	}
	ext.mu.Unlock()
	m.states.mu.Unlock()
	lease.Unlock()
	if err != nil {
		return err
	}
	m.fireTimeStateChanged(ctx, fed, h)
	return m.tryGrantPending(ctx, fed)
}

// EnableConstrained accepts an asynchronous 8.5 request. It remains pending
// while another regulator can still send below this federate's current time.
func (m *ManagerOf[T]) EnableConstrained(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	return m.requestEnable(ctx, fed, h, false, 0)
}

// DisableConstrained implements core.TimeManager.
//
// Errors:
//   - core.ErrFederationHalted if the federation is in the halted
//     terminal state
//   - core.ErrTimeNotConstrained if not currently constrained
func (m *ManagerOf[T]) DisableConstrained(ctx context.Context, fed core.FederationName, h core.FederateHandle) error {
	if extOfOf[T](m).isHalted(fed) {
		return core.ErrFederationHalted
	}
	lease := extOfOf[T](m).evaluatorLock(fed)
	lease.Lock()
	previous := m.states.snapshot(fed, h)
	if err := m.states.disableConstrained(fed, h); err != nil {
		lease.Unlock()
		return err
	}
	err := m.drainAllBufferedTSO(ctx, fed, h)
	if errors.Is(err, core.ErrFederateOverflow) {
		m.markDeliveryRetry(fed)
		err = nil
	} else if err != nil {
		// Keep the operation retryable; unsent callbacks were restored by drain.
		m.states.mu.Lock()
		*m.states.getOrCreateLocked(fed, h) = previous
		m.states.mu.Unlock()
	}
	lease.Unlock()
	m.finishDeletionDelivery(fed)
	m.fireTimeStateChanged(ctx, fed, h)
	if err != nil {
		return err
	}
	// Removing constraint can raise a pending next-message sender's bound and
	// release both peer enables and its own outstanding advance request.
	err = m.tryGrantPending(ctx, fed)
	if errors.Is(err, core.ErrFederateOverflow) {
		return nil
	}
	return err
}

// NextMessageRequest implements the 8.10 next-message service. Its grant
// target may be earlier than t when an incoming TSO message determines it.
// The default policy requires a bound strictly above the grant target.
func (m *ManagerOf[T]) NextMessageRequest(ctx context.Context, fed core.FederationName, h core.FederateHandle, t T) error {
	return m.nextMessageRequest(ctx, fed, h, t)
}

// NextMessageRequestAvailable implements 8.11. It uses a next-message
// target with an inclusive bound; equal-time messages may arrive later.
func (m *ManagerOf[T]) NextMessageRequestAvailable(ctx context.Context, fed core.FederationName, h core.FederateHandle, t T) error {
	return m.dispatchAdvance(ctx, fed, h, t, ModeNMRA)
}

// TimeAdvanceRequest implements 8.8. It grants exactly t, after eligible
// messages have been admitted, and remains pending while the bound blocks t.
func (m *ManagerOf[T]) TimeAdvanceRequest(ctx context.Context, fed core.FederationName, h core.FederateHandle, t T) error {
	return m.dispatchAdvance(ctx, fed, h, t, ModeTAR)
}

// TimeAdvanceRequestAvailable implements 8.9. Like TAR it grants exactly
// t, but permits an inclusive bound and later equal-time messages.
func (m *ManagerOf[T]) TimeAdvanceRequestAvailable(ctx context.Context, fed core.FederationName, h core.FederateHandle, t T) error {
	return m.dispatchAdvance(ctx, fed, h, t, ModeTARA)
}

// FlushQueueRequest dispatches the 8.12 flush mode. Unlike TAR/TARA,
// its grant may be below t. Queue delivery is handled by emitGrant.
func (m *ManagerOf[T]) FlushQueueRequest(ctx context.Context, fed core.FederationName, h core.FederateHandle, t T) error {
	return m.dispatchAdvance(ctx, fed, h, t, ModeFQR)
}

// CheckStalls is called by the rtid main loop (or directly by tests with
// ManualClock) to detect federates whose last NER timestamp is older than
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
func (m *ManagerOf[T]) CheckStalls(ctx context.Context) int {
	return m.checkStalls(ctx)
}

// QueryLITS includes both GALT and pending incoming timestamps (IEEE
// 1516.1-2010 8.1.5). It is undefined only when neither is available.
func (m *ManagerOf[T]) QueryLITS(
	fed core.FederationName, h core.FederateHandle,
) (T, bool) {
	snapshot := m.Metrics(fed, h)
	if snapshot.LITS == nil {
		return 0, false
	}
	return *snapshot.LITS, true
}

// fireTimeStateChanged observes both time stores together, then invokes the
// observer without holding service locks. Callers must release the evaluator.
func (m *ManagerOf[T]) fireTimeStateChanged(ctx context.Context, fed core.FederationName, h core.FederateHandle) {
	if m.opts.OnTimeStateChanged == nil {
		return
	}
	ext := extOfOf[T](m)
	m.states.mu.Lock()
	ext.mu.Lock()
	var regulating, constrained bool
	var lookahead, current T
	if fs := m.states.getLocked(fed, h); fs != nil {
		regulating, constrained, lookahead = fs.regulating, fs.constrained, fs.lookahead
	}
	if ns := ext.getLocked(fed, h); ns != nil {
		current = ns.currentTime
	}
	ext.mu.Unlock()
	m.states.mu.Unlock()
	m.opts.OnTimeStateChanged(ctx, fed, h, regulating, constrained, lookahead, current)
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
func (m *ManagerOf[T]) Snapshot(fed core.FederationName) core.TimeSnapshotOf[T] {
	// Step 1: collect every federate that has any state recorded for
	// fed, plus its regulation flags + lookahead.
	type rawState = federateStateOf[T]
	collected := map[core.FederateHandle]rawState{}
	m.states.mu.Lock()
	defer m.states.mu.Unlock()
	for k, st := range m.states.states {
		if k.fed != fed {
			continue
		}
		collected[k.h] = *st
	}

	// Step 2: enrich with currentTime + pending state from the
	// extension store, and add any federate present only there.
	ext := extOfOf[T](m)
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

	out := core.TimeSnapshotOf[T]{
		Federates: make([]core.TimeFederateStateOf[T], 0, len(handles)),
	}
	for _, h := range handles {
		raw := collected[h]
		fs := core.TimeFederateStateOf[T]{
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
	var regSet []RegulatingFederateOf[T]
	for _, h := range handles {
		raw := collected[h]
		if !raw.regulating {
			continue
		}
		regSet = append(regSet, regulatingBoundSnapshotOf[T](h, &raw, ext.getLocked(fed, h)))
	}
	if m.opts.LBTSStrategy != nil {
		bound := m.lbtsBound(regSet)
		out.LBTS, out.LBTSUnbounded = horizonScalar(bound), bound.Unbounded
	} else {
		bound := LBTSBoundOf(regSet)
		out.LBTS, out.LBTSUnbounded = horizonScalar(bound), bound.Unbounded
	}
	return out
}

// Compile-time assertion that Manager implements core.TimeManager.
// Removing any required method fails the build at this line.
var _ core.TimeManagerOf[core.LogicalTime] = (*ManagerOf[core.LogicalTime])(nil)
var _ core.TimeManagerOf[int64] = (*ManagerOf[int64])(nil)
var _ core.BoundTimeManagerOf[int64] = (*ManagerOf[int64])(nil)
var _ core.AtomicOutgoingTSODeliveryGateOf[int64] = (*ManagerOf[int64])(nil)
