package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/eventlog"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// timedConfig configures runTimedDemo. The example main and the
// determinism / replay / stall harnesses construct it directly.
type timedConfig struct {
	// FederationName scopes all activity inside this run.
	FederationName core.FederationName

	// Ticks is the number of NER advances each regulating federate
	// performs (per round-robin pass). The total grant count is
	// approximately Ticks * len(Lookaheads); the actual grant pattern
	// depends on the per-federate lookaheads (peers with smaller
	// lookahead unblock peers with larger lookahead).
	Ticks int

	// Lookaheads is the per-federate lookahead vector; len(Lookaheads)
	// determines the federate count. Default (when nil) is the M3
	// reference triple {1.0, 2.0, 0.5} from the W4 brief.
	Lookaheads []core.LogicalTime

	// Step is the per-tick logical-time delta requested by each
	// federate. Each federate's NER target is currentTime + Step *
	// max(1, lookahead/Step). Default 1.0 if zero.
	Step core.LogicalTime

	// LogDir, when non-empty, makes the multiplex writer write per-
	// federation log files under this directory. Empty disables file
	// persistence (events still flow through the in-memory pipeline).
	LogDir string

	// EventLog overrides the default file/discard event log. Tests
	// inject a *bytes.Buffer-backed MultiplexWriter to capture the
	// stream for byte-comparison.
	EventLog core.EventLog

	// Deterministic, when true, uses a fixed FakeClock so the
	// per-record wall_ns is identical across runs. The determinism
	// harness sets this; production runs leave it false.
	Deterministic bool

	// StallTimeout overrides the time-manager's stall timeout for
	// this run. Zero uses the package default (60s); the stall
	// harness sets a tiny value (e.g. 5s) so it can advance a
	// FakeClock past the threshold cheaply.
	StallTimeout stdtime.Duration

	// SkipFederateIndex, when set in [0, len(Lookaheads)), causes
	// runTimedDemo to NOT call NER for that federate. Used by the
	// stall harness to provoke a halt.
	SkipFederateIndex int

	// Clock overrides the default real/fake clock. The stall harness
	// passes its own *core.FakeClock so the test can Advance after
	// the demo loop returns.
	Clock core.Clock
}

// timedStats summarizes a completed run.
type timedStats struct {
	// FederateCount is len(Lookaheads).
	FederateCount int
	// TicksCompleted is the per-federate NER count actually issued.
	TicksCompleted int
	// GrantsObserved is the count of TimeAdvanceGrant events delivered
	// through the outbox. May be < Ticks when requests are still held
	// pending at shutdown (M38 GA — §8.8: a blocked NER emits no
	// interim grant).
	GrantsObserved int
	// HaltsObserved counts FederationHalted events delivered through
	// the outbox.
	HaltsObserved int
	// Elapsed is end-to-end wall time (or fake clock delta) of the
	// run.
	Elapsed stdtime.Duration
}

// defaultTimedLookaheads is the M3 reference triple from the W4 brief.
var defaultTimedLookaheads = []core.LogicalTime{1.0, 2.0, 0.5}

// runTimedDemo wires an in-process rtid time-management subsystem
// (Manager + EventLog + recording outbox) and runs N federates through
// `Ticks` rounds of NER. Each federate `i` requests advance to its
// currentTime + max(Step, Lookaheads[i]) on every round. Grants and
// halts are captured in stats so callers can validate the run.
//
// Determinism: when cfg.Deterministic is true the run uses a FakeClock
// pinned at the Unix epoch; otherwise a real clock. All NER iteration
// is round-robin in handle order to honor the M3 NFR-DET-1 contract.
func runTimedDemo(ctx context.Context, cfg timedConfig) (timedStats, error) {
	if cfg.Ticks <= 0 {
		return timedStats{}, errors.New("timed: Ticks must be positive")
	}
	if cfg.FederationName == "" {
		return timedStats{}, errors.New("timed: FederationName is required")
	}
	if len(cfg.Lookaheads) == 0 {
		cfg.Lookaheads = append([]core.LogicalTime(nil), defaultTimedLookaheads...)
	}
	if cfg.Step == 0 {
		cfg.Step = core.LogicalTime(1.0)
	}

	rt, cleanup, err := buildTimedRuntime(cfg)
	if err != nil {
		return timedStats{}, err
	}
	defer cleanup()

	// Enable regulation for each federate. Handles 1..N in order so
	// the LBTS/grant ordering is reproducible.
	for i, look := range cfg.Lookaheads {
		h := core.FederateHandle(uint64(i + 1))
		if err := rt.tm.EnableRegulation(ctx, cfg.FederationName, h, look); err != nil {
			return timedStats{}, fmt.Errorf("timed: EnableRegulation fed %d: %w", h, err)
		}
	}

	// SkipFederateIndex uses 1-based handle numbering (matching the
	// handle assignment loop above): 1 ≤ idx ≤ N skips that federate's
	// NER calls so the stall harness can provoke a halt. Zero (the
	// default) means no skip — every federate NERs every tick.
	skipEnabled := cfg.SkipFederateIndex >= 1 && cfg.SkipFederateIndex <= len(cfg.Lookaheads)

	start := rt.clock.Now()
	currentTimes := make([]core.LogicalTime, len(cfg.Lookaheads))
	ticksCompleted, err := runTimedNERLoop(ctx, rt, cfg, currentTimes, skipEnabled)
	if err != nil {
		return timedStats{}, err
	}
	elapsed := rt.clock.Now().Sub(start)

	// Sync the eventlog so the demo's bytes hit disk before the
	// caller observes the file (mirrors pingpong).
	if rt.log != nil {
		if err := rt.log.Sync(ctx, cfg.FederationName); err != nil {
			// Sync may legitimately fail when no file backing exists
			// for this federation (discard sink). Tolerate
			// ErrFederationNotFound; surface anything else.
			if !errors.Is(err, core.ErrFederationNotFound) {
				return timedStats{}, fmt.Errorf("timed: Sync: %w", err)
			}
		}
	}

	grants, halts := rt.outbox.Counts()
	return timedStats{
		FederateCount:  len(cfg.Lookaheads),
		TicksCompleted: ticksCompleted,
		GrantsObserved: grants,
		HaltsObserved:  halts,
		Elapsed:        elapsed,
	}, nil
}

// runTimedNERLoop runs the per-tick NER round-robin and returns the
// number of ticks completed. Extracted from runTimedDemo so the outer
// function stays under the gocyclo budget. currentTimes is mutated in
// place so the caller can read the final per-federate logical times if
// needed.
func runTimedNERLoop(ctx context.Context, rt *timedRuntime, cfg timedConfig, currentTimes []core.LogicalTime, skipEnabled bool) (int, error) {
	ticksCompleted := 0
	for tick := 0; tick < cfg.Ticks; tick++ {
		if err := ctx.Err(); err != nil {
			return ticksCompleted, err
		}
		if err := runTimedNERTick(ctx, rt, cfg, currentTimes, skipEnabled, tick); err != nil {
			return ticksCompleted, err
		}
		ticksCompleted++
	}
	return ticksCompleted, nil
}

// runTimedNERTick performs one round-robin NER pass over every
// (non-skipped) federate. ErrDuplicateNER is swallowed (a blocked NER
// stays pending until peers raise LBTS — M38 GA, §8.8); any other
// error is surfaced to the caller. Mutates currentTimes for federates whose
// NER was accepted.
func runTimedNERTick(ctx context.Context, rt *timedRuntime, cfg timedConfig, currentTimes []core.LogicalTime, skipEnabled bool, tick int) error {
	for i := range cfg.Lookaheads {
		h := core.FederateHandle(uint64(i + 1))
		if skipEnabled && uint64(h) == uint64(cfg.SkipFederateIndex) {
			continue
		}
		delta := cfg.Step
		if cfg.Lookaheads[i] > delta {
			delta = cfg.Lookaheads[i]
		}
		target := currentTimes[i] + delta
		err := rt.tm.NextMessageRequest(ctx, cfg.FederationName, h, target)
		switch {
		case err == nil:
			currentTimes[i] = target
		case errors.Is(err, timepkg.ErrDuplicateNER):
			// Federate has an outstanding pending NER from a
			// previous tick (held below LBTS, §8.8); peers'
			// future NER calls will eventually push LBTS up and
			// the grant will arrive.
		default:
			return fmt.Errorf("timed: NER fed %d tick %d: %w", h, tick, err)
		}
	}
	return nil
}

// timedRuntime bundles the in-process components for the timed demo.
type timedRuntime struct {
	clock  core.Clock
	log    core.EventLog
	tm     *timepkg.Manager
	outbox *recordingOutbox
}

// buildTimedRuntime constructs the components and returns a cleanup
// that closes any owned event log.
func buildTimedRuntime(cfg timedConfig) (*timedRuntime, func(), error) {
	var clock core.Clock
	switch {
	case cfg.Clock != nil:
		clock = cfg.Clock
	case cfg.Deterministic:
		clock = core.NewFakeClock(stdtime.Unix(0, 0))
	default:
		clock = core.NewRealClock()
	}
	log, ownsLog, err := timedEventLog(cfg, clock)
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() {
		if ownsLog {
			if mw, ok := log.(*eventlog.MultiplexWriter); ok {
				_ = mw.Close()
			}
		}
	}
	outbox := newRecordingOutbox()
	tm, err := timepkg.New(timepkg.Options{
		Clock:        clock,
		Outbox:       outbox,
		EventLog:     log,
		StallTimeout: cfg.StallTimeout,
	})
	if err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("timed: time.New: %w", err)
	}
	return &timedRuntime{clock: clock, log: log, tm: tm, outbox: outbox}, cleanup, nil
}

// timedEventLog returns the EventLog for this run plus an owns flag
// indicating whether the caller must Close it. Mirrors pingpongEventLog.
func timedEventLog(cfg timedConfig, clock core.Clock) (core.EventLog, bool, error) {
	if cfg.EventLog != nil {
		return cfg.EventLog, false, nil
	}
	if cfg.LogDir != "" {
		mw, err := eventlog.NewMultiplexWriter(eventlog.MultiplexOptions{
			Clock: clock,
			Mode:  core.ModeVerbose,
			Dir:   cfg.LogDir,
		})
		if err != nil {
			return nil, false, err
		}
		return mw, true, nil
	}
	mw, err := eventlog.NewMultiplexWriter(eventlog.MultiplexOptions{
		Clock:   clock,
		Mode:    core.ModeVerbose,
		Factory: timedDiscardFactory(clock),
	})
	if err != nil {
		return nil, false, err
	}
	return mw, true, nil
}

func timedDiscardFactory(clock core.Clock) eventlog.WriterFactory {
	return func(opts eventlog.WriterOptions) (*eventlog.Writer, error) {
		opts.Sink = discardSink{}
		opts.Clock = clock
		return eventlog.NewWriter(opts)
	}
}

// recordingOutbox is the in-process Outbox used by runTimedDemo. It
// counts TimeAdvanceGrant + FederationHalted emissions so the demo
// caller can validate the grant fan-out without standing up a
// subscription stream. It satisfies core.Outbox; Subscribe is not
// required by the timed demo.
type recordingOutbox struct {
	mu     sync.Mutex
	grants int
	halts  int
	all    []recordedSend
}

type recordedSend struct {
	Federation core.FederationName
	Federate   core.FederateHandle
	Event      core.OutboundEvent
}

func newRecordingOutbox() *recordingOutbox {
	return &recordingOutbox{}
}

// Send implements core.Outbox.
func (o *recordingOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.all = append(o.all, recordedSend{Federation: fed, Federate: h, Event: evt})
	switch evt.(type) {
	case *timepkg.TimeAdvanceGrant:
		o.grants++
	case *timepkg.FederationHalted:
		o.halts++
	}
	return nil
}

// Counts returns (grants, halts).
func (o *recordingOutbox) Counts() (int, int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.grants, o.halts
}

// Sent returns a snapshot of recorded sends.
func (o *recordingOutbox) Sent() []recordedSend {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]recordedSend, len(o.all))
	copy(out, o.all)
	return out
}

// SentTo returns recordings filtered to (fed, h).
func (o *recordingOutbox) SentTo(fed core.FederationName, h core.FederateHandle) []recordedSend {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []recordedSend
	for _, s := range o.all {
		if s.Federation == fed && s.Federate == h {
			out = append(out, s)
		}
	}
	return out
}

// AdvanceClockAndCheckStalls is a small affordance for the stall
// harness: advances the embedded FakeClock by d (no-op if Clock is not
// a FakeClock) and runs CheckStalls on the runtime's manager. Returns
// the count of federations halted by the call.
//
// Lives on timedRuntime so tests don't have to reach into the manager
// directly. Production callers (cmd/rtid -mode=timed-demo) ignore this.
func (rt *timedRuntime) AdvanceClockAndCheckStalls(ctx context.Context, d stdtime.Duration) int {
	if fc, ok := rt.clock.(*core.FakeClock); ok {
		fc.Advance(d)
	}
	return rt.tm.CheckStalls(ctx)
}
