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

	clock := core.NewRealClock()
	log, ownsLog, err := pingpongEventLog(cfg, clock)
	if err != nil {
		return pingpongStats{}, err
	}
	if ownsLog {
		defer func() {
			if mw, ok := log.(*eventlog.MultiplexWriter); ok {
				_ = mw.Close()
			}
		}()
	}

	foms := newPingpongFOMRepo()
	fedMgr, err := federation.New(federation.Options{
		Clock:    clock,
		EventLog: log,
		FOMs:     foms,
	})
	if err != nil {
		return pingpongStats{}, err
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
		return pingpongStats{}, err
	}

	if err := fedMgr.CreateFederation(ctx, core.CreateFederationRequest{
		Name: cfg.FederationName,
		Mode: core.ModeVerbose,
		Seed: 1,
	}); err != nil {
		return pingpongStats{}, fmt.Errorf("pingpong: CreateFederation: %w", err)
	}

	pingHandle, err := fedMgr.JoinFederation(ctx, core.JoinFederationRequest{
		Federation: cfg.FederationName, FederateName: "ping",
	})
	if err != nil {
		return pingpongStats{}, fmt.Errorf("pingpong: ping Join: %w", err)
	}
	pongHandle, err := fedMgr.JoinFederation(ctx, core.JoinFederationRequest{
		Federation: cfg.FederationName, FederateName: "pong",
	})
	if err != nil {
		return pingpongStats{}, fmt.Errorf("pingpong: pong Join: %w", err)
	}

	const honkClass = core.InteractionClassHandle(1)
	const ackClass = core.InteractionClassHandle(2)

	if err := declMgr.PublishInteractionClass(ctx, cfg.FederationName, pingHandle, honkClass); err != nil {
		return pingpongStats{}, err
	}
	if err := declMgr.SubscribeInteractionClass(ctx, cfg.FederationName, pingHandle, ackClass); err != nil {
		return pingpongStats{}, err
	}
	if err := declMgr.PublishInteractionClass(ctx, cfg.FederationName, pongHandle, ackClass); err != nil {
		return pingpongStats{}, err
	}
	if err := declMgr.SubscribeInteractionClass(ctx, cfg.FederationName, pongHandle, honkClass); err != nil {
		return pingpongStats{}, err
	}

	pingChan, _, err := outbox.Subscribe(ctx, cfg.FederationName, pingHandle)
	if err != nil {
		return pingpongStats{}, err
	}
	pongChan, _, err := outbox.Subscribe(ctx, cfg.FederationName, pongHandle)
	if err != nil {
		return pingpongStats{}, err
	}

	pongDone := make(chan error, 1)
	go func() {
		for i := 0; i < cfg.Rounds; i++ {
			select {
			case <-pongChan:
			case <-ctx.Done():
				pongDone <- ctx.Err()
				return
			}
			if err := objReg.SendInteraction(ctx, cfg.FederationName, pongHandle, ackClass, nil, nil); err != nil {
				pongDone <- fmt.Errorf("pong: SendInteraction: %w", err)
				return
			}
		}
		pongDone <- nil
	}()

	start := clock.Now()
	for i := 0; i < cfg.Rounds; i++ {
		if err := objReg.SendInteraction(ctx, cfg.FederationName, pingHandle, honkClass, nil, nil); err != nil {
			return pingpongStats{}, fmt.Errorf("ping: SendInteraction: %w", err)
		}
		select {
		case <-pingChan:
		case <-ctx.Done():
			return pingpongStats{}, ctx.Err()
		}
	}

	if err := <-pongDone; err != nil {
		return pingpongStats{}, err
	}
	elapsed := clock.Now().Sub(start)

	if err := fedMgr.ResignFederation(ctx, cfg.FederationName, pingHandle, core.ResignActionUnconditionallyDivestAttributes); err != nil {
		return pingpongStats{}, err
	}
	if err := fedMgr.ResignFederation(ctx, cfg.FederationName, pongHandle, core.ResignActionUnconditionallyDivestAttributes); err != nil {
		return pingpongStats{}, err
	}
	if err := log.Sync(ctx, cfg.FederationName); err != nil {
		return pingpongStats{}, err
	}

	return pingpongStats{
		RoundsCompleted: cfg.Rounds,
		Elapsed:         elapsed,
	}, nil
}

// pingpongEventLog returns the EventLog for this run plus an owns flag
// indicating whether the caller must Close it.
func pingpongEventLog(cfg pingpongConfig, clock core.Clock) (core.EventLog, bool, error) {
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
// channel and never overflows under expected load.
type syncOutbox struct {
	mu          sync.Mutex
	subscribers map[fedHandleKey]chan core.OutboundEvent
}

func newSyncOutbox() *syncOutbox {
	return &syncOutbox{subscribers: map[fedHandleKey]chan core.OutboundEvent{}}
}

// Send implements core.Outbox.
func (s *syncOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	s.mu.Lock()
	ch, ok := s.subscribers[fedHandleKey{fed: fed, h: h}]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	ch <- evt
	return nil
}

// Subscribe registers a per-federate inbox.
func (s *syncOutbox) Subscribe(_ context.Context, fed core.FederationName, h core.FederateHandle) (<-chan core.OutboundEvent, func() error, error) {
	key := fedHandleKey{fed: fed, h: h}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.subscribers[key]; dup {
		return nil, nil, fmt.Errorf("pingpong: subscriber already registered for (%s, %d)", fed, h)
	}
	ch := make(chan core.OutboundEvent, 1024)
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

func (permissiveFOMHandle) IsValid() bool                                                       { return true }
func (permissiveFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool)             { return 1, true }
func (permissiveFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool)   { return 1, true }
func (permissiveFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}
