package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	"github.com/cbchoi/gorti/rti/internal/eventlog"
	"github.com/cbchoi/gorti/rti/internal/federation"
	"github.com/cbchoi/gorti/rti/internal/object"
)

// pingpongConfig configures runPingpongDemo. The example main and the
// determinism / replay tests construct it directly.
type pingpongConfig struct {
	// FederationName scopes all activity inside this run.
	FederationName core.FederationName

	// Rounds is the count of ping->pong->ping round-trips. Each round
	// produces 2 InteractionSent events in the event log.
	Rounds int

	// LogDir, when non-empty, makes the multiplex writer write per-
	// federation log files under this directory. Empty disables file
	// persistence (events still flow through the in-memory pipeline).
	LogDir string

	// EventLog overrides the default file/discard event log. Tests
	// inject a *bytes.Buffer-backed MultiplexWriter to capture the
	// stream for byte-comparison.
	EventLog core.EventLog

	// Deterministic, when true, uses a fixed FakeClock so the
	// per-record wall_ns is identical across runs. Enables the
	// determinism harness; production runs leave this false to use the
	// real wall clock (the wall_ns field is informational per proto/
	// rti/v1/eventlog.proto).
	Deterministic bool

	// FederationMode is the operating mode for the created federation
	// (TASK-076). Zero (ModeUnspecified) is normalized to ModeVerbose
	// by the federation manager — see manager.go::CreateFederation.
	FederationMode core.Mode
}

// pingpongStats summarizes a completed run.
type pingpongStats struct {
	RoundsCompleted int
	Elapsed         time.Duration
}

// runPingpongDemo wires an in-process rtid (federation, declaration,
// object, eventlog) and runs two federate goroutines that exchange
// cfg.Rounds round-trips. Synchronous handoff via channels makes the
// event-log append order strictly deterministic (ping append → pong
// receive → pong append → ping receive → loop).
func runPingpongDemo(ctx context.Context, cfg pingpongConfig) (pingpongStats, error) {
	if cfg.Rounds <= 0 {
		return pingpongStats{}, errors.New("pingpong: Rounds must be positive")
	}
	if cfg.FederationName == "" {
		return pingpongStats{}, errors.New("pingpong: FederationName is required")
	}

	rt, cleanup, err := buildPingpongRuntime(cfg)
	if err != nil {
		return pingpongStats{}, err
	}
	defer cleanup()

	pingHandle, pongHandle, err := pingpongJoin(ctx, cfg, rt)
	if err != nil {
		return pingpongStats{}, err
	}
	if err := pingpongDeclarations(ctx, cfg, rt, pingHandle, pongHandle); err != nil {
		return pingpongStats{}, err
	}

	pingChan, _, err := rt.outbox.Subscribe(ctx, cfg.FederationName, pingHandle)
	if err != nil {
		return pingpongStats{}, err
	}
	pongChan, _, err := rt.outbox.Subscribe(ctx, cfg.FederationName, pongHandle)
	if err != nil {
		return pingpongStats{}, err
	}

	start := rt.clock.Now()
	pongDone := startPongWorker(ctx, cfg, rt, pongHandle, pongChan)
	if err := drivePingLoop(ctx, cfg, rt, pingHandle, pingChan); err != nil {
		return pingpongStats{}, err
	}
	if err := <-pongDone; err != nil {
		return pingpongStats{}, err
	}
	elapsed := rt.clock.Now().Sub(start)

	if err := pingpongResign(ctx, cfg, rt, pingHandle, pongHandle); err != nil {
		return pingpongStats{}, err
	}
	if err := rt.log.Sync(ctx, cfg.FederationName); err != nil {
		return pingpongStats{}, err
	}
	return pingpongStats{RoundsCompleted: cfg.Rounds, Elapsed: elapsed}, nil
}

// pingpongRuntime bundles the in-process components for the demo so the
// helper functions don't need a long argument list.
type pingpongRuntime struct {
	clock   core.Clock
	log     core.EventLog
	fedMgr  *federation.Manager
	declMgr core.DeclarationManagement
	objReg  *object.Registry
	outbox  *syncOutbox
}

// buildPingpongRuntime constructs the components and returns a cleanup
// closing any owned event log.
func buildPingpongRuntime(cfg pingpongConfig) (*pingpongRuntime, func(), error) {
	var clock core.Clock = core.NewRealClock()
	if cfg.Deterministic {
		clock = core.NewFakeClock(time.Unix(0, 0))
	}
	log, ownsLog, err := pingpongEventLog(cfg, clock)
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
	foms := newPingpongFOMRepo()
	fedMgr, err := federation.New(federation.Options{
		Clock:    clock,
		EventLog: log,
		FOMs:     foms,
	})
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	declMgr := declaration.New()
	outbox := newSyncOutbox()
	objReg, err := object.New(object.Options{
		EventLog:     log,
		Declarations: declMgr,
		Outbox:       outbox,
		FOMs:         foms,
		Clock:        clock,
	})
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return &pingpongRuntime{
		clock: clock, log: log, fedMgr: fedMgr,
		declMgr: declMgr, objReg: objReg, outbox: outbox,
	}, cleanup, nil
}

// pingpongJoin creates the federation and joins both federates.
func pingpongJoin(ctx context.Context, cfg pingpongConfig, rt *pingpongRuntime) (core.FederateHandle, core.FederateHandle, error) {
	mode := cfg.FederationMode
	if mode == core.ModeUnspecified {
		mode = core.ModeVerbose
	}
	if err := rt.fedMgr.CreateFederation(ctx, core.CreateFederationRequest{
		Name: cfg.FederationName,
		Mode: mode,
		Seed: 1,
	}); err != nil {
		return 0, 0, fmt.Errorf("pingpong: CreateFederation: %w", err)
	}
	pingHandle, err := rt.fedMgr.JoinFederation(ctx, core.JoinFederationRequest{
		Federation: cfg.FederationName, FederateName: "ping",
	})
	if err != nil {
		return 0, 0, fmt.Errorf("pingpong: ping Join: %w", err)
	}
	pongHandle, err := rt.fedMgr.JoinFederation(ctx, core.JoinFederationRequest{
		Federation: cfg.FederationName, FederateName: "pong",
	})
	if err != nil {
		return 0, 0, fmt.Errorf("pingpong: pong Join: %w", err)
	}
	return pingHandle, pongHandle, nil
}

// pingpongDeclarations records publish/subscribe for honk + ack.
func pingpongDeclarations(ctx context.Context, cfg pingpongConfig, rt *pingpongRuntime, ping, pong core.FederateHandle) error {
	for _, op := range []struct {
		fn func(context.Context, core.FederationName, core.FederateHandle, core.InteractionClassHandle) error
		h  core.FederateHandle
		c  core.InteractionClassHandle
	}{
		{rt.declMgr.PublishInteractionClass, ping, honkClass},
		{rt.declMgr.SubscribeInteractionClass, ping, ackClass},
		{rt.declMgr.PublishInteractionClass, pong, ackClass},
		{rt.declMgr.SubscribeInteractionClass, pong, honkClass},
	} {
		if err := op.fn(ctx, cfg.FederationName, op.h, op.c); err != nil {
			return err
		}
	}
	return nil
}

// startPongWorker launches the pong-side loop and returns a channel
// that yields its terminal error (nil on clean completion).
//
// pongChan delivers batches of events; a single round here always
// produces a 1-event batch (one honk per cycle), so we drain
// per-batch and count one received event per batch.
func startPongWorker(ctx context.Context, cfg pingpongConfig, rt *pingpongRuntime, pong core.FederateHandle, pongChan <-chan []core.OutboundEvent) <-chan error {
	done := make(chan error, 1)
	go func() {
		received := 0
		for received < cfg.Rounds {
			select {
			case batch := <-pongChan:
				received += len(batch)
			case <-ctx.Done():
				done <- ctx.Err()
				return
			}
			if err := rt.objReg.SendInteraction(ctx, cfg.FederationName, pong, ackClass, nil, nil); err != nil {
				done <- fmt.Errorf("pong: SendInteraction: %w", err)
				return
			}
		}
		done <- nil
	}()
	return done
}

// drivePingLoop runs the ping side of the round-trip.
func drivePingLoop(ctx context.Context, cfg pingpongConfig, rt *pingpongRuntime, ping core.FederateHandle, pingChan <-chan []core.OutboundEvent) error {
	for i := 0; i < cfg.Rounds; i++ {
		if err := rt.objReg.SendInteraction(ctx, cfg.FederationName, ping, honkClass, nil, nil); err != nil {
			return fmt.Errorf("ping: SendInteraction: %w", err)
		}
		select {
		case <-pingChan:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// pingpongResign cleans up both federates.
func pingpongResign(ctx context.Context, cfg pingpongConfig, rt *pingpongRuntime, ping, pong core.FederateHandle) error {
	for _, h := range []core.FederateHandle{ping, pong} {
		if err := rt.fedMgr.ResignFederation(ctx, cfg.FederationName, h, core.ResignActionUnconditionallyDivestAttributes); err != nil {
			return err
		}
	}
	return nil
}

// honkClass / ackClass are the two interaction classes the demo uses.
// Hand-managed (the permissive FOM repo accepts any handle); 1 and 2
// are stable across runs which keeps the eventlog body deterministic.
const (
	honkClass = core.InteractionClassHandle(1)
	ackClass  = core.InteractionClassHandle(2)
)

// pingpongEventLog returns the EventLog for this run plus an owns flag
// indicating whether the caller must Close it.
func pingpongEventLog(cfg pingpongConfig, clock core.Clock) (core.EventLog, bool, error) {
	if cfg.EventLog != nil {
		return cfg.EventLog, false, nil
	}
	mode := cfg.FederationMode
	if mode == core.ModeUnspecified {
		mode = core.ModeVerbose
	}
	if cfg.LogDir != "" {
		mw, err := eventlog.NewMultiplexWriter(eventlog.MultiplexOptions{
			Clock: clock,
			Mode:  mode,
			Seed:  1,
			Dir:   cfg.LogDir,
		})
		if err != nil {
			return nil, false, err
		}
		return mw, true, nil
	}
	mw, err := eventlog.NewMultiplexWriter(eventlog.MultiplexOptions{
		Clock:   clock,
		Mode:    mode,
		Seed:    1,
		Factory: pingpongDiscardFactory(clock),
	})
	if err != nil {
		return nil, false, err
	}
	return mw, true, nil
}

func pingpongDiscardFactory(clock core.Clock) eventlog.WriterFactory {
	return func(opts eventlog.WriterOptions) (*eventlog.Writer, error) {
		opts.Sink = discardSink{}
		opts.Clock = clock
		return eventlog.NewWriter(opts)
	}
}

// syncOutbox is the in-process Outbox used by the pingpong demo. It
// satisfies grpc.SubscribableOutbox shape (channel-per-federate) but
// without the bounded-overflow contract — the demo uses a 1024-buffered
// channel and never overflows under expected load. Each Send is wrapped
// in a 1-event batch so the channel signature matches the production
// SubscribableOutbox after the batched-delivery refactor.
type syncOutbox struct {
	mu          sync.Mutex
	subscribers map[fedHandleKey]chan []core.OutboundEvent
}

func newSyncOutbox() *syncOutbox {
	return &syncOutbox{subscribers: map[fedHandleKey]chan []core.OutboundEvent{}}
}

// Send implements core.Outbox.
func (s *syncOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	s.mu.Lock()
	ch, ok := s.subscribers[fedHandleKey{fed: fed, h: h}]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	ch <- []core.OutboundEvent{evt}
	return nil
}

// Subscribe registers a per-federate inbox.
func (s *syncOutbox) Subscribe(_ context.Context, fed core.FederationName, h core.FederateHandle) (<-chan []core.OutboundEvent, func() error, error) {
	key := fedHandleKey{fed: fed, h: h}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.subscribers[key]; dup {
		return nil, nil, fmt.Errorf("pingpong: subscriber already registered for (%s, %d)", fed, h)
	}
	ch := make(chan []core.OutboundEvent, 1024)
	s.subscribers[key] = ch
	cancel := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if existing, ok := s.subscribers[key]; ok && existing == ch {
			delete(s.subscribers, key)
			close(ch)
		}
		return nil
	}
	return ch, cancel, nil
}

// pingpongFOMRepo is a permissive FOM repository — Load returns a handle
// that resolves any name to handle 1; the demo pre-allocates honk/ack
// classes directly. The federation manager's Load step succeeds; the
// declaration manager / object registry trust the demo's hand-managed
// handle space.
type pingpongFOMRepo struct{}

func newPingpongFOMRepo() *pingpongFOMRepo { return &pingpongFOMRepo{} }

func (r *pingpongFOMRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	return permissiveFOMHandle{}, nil
}

func (r *pingpongFOMRepo) Get(_ context.Context, _ core.FederationName) (core.FOMHandle, error) {
	return permissiveFOMHandle{}, nil
}

type permissiveFOMHandle struct{}

func (permissiveFOMHandle) IsValid() bool                                           { return true }
func (permissiveFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) { return 1, true }
func (permissiveFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}
