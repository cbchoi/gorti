package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

const (
	controlPeerReady uint32 = 1
	controlStartTAR  uint32 = 2

	waiterLookahead = 1.0
	peerLookahead   = 2.0
)

type config struct {
	url        string
	federation string
	role       string
	target     float64
	peerDelay  time.Duration
	timeout    time.Duration
	fomPath    string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "go-tar-wait:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := parseFlags()
	if err := validateConfig(cfg); err != nil {
		return err
	}

	xml, err := os.ReadFile(cfg.fomPath)
	if err != nil {
		return fmt.Errorf("read FOM: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	conn, err := federate.Connect(ctx, cfg.url)
	if err != nil {
		return fmt.Errorf("connect %s: %w", cfg.url, err)
	}
	defer conn.Close()

	fed, err := conn.JoinFederation(ctx, federate.FederationSpec{
		Name:       cfg.federation,
		FOMModules: []federate.FOMModule{{Path: cfg.fomPath, XML: xml}},
	}, cfg.role)
	if err != nil {
		return fmt.Errorf("join federation: %w", err)
	}
	defer resign(fed)

	if err := fed.PublishInteractionClass(ctx, "Control"); err != nil {
		return fmt.Errorf("publish Control: %w", err)
	}
	if err := fed.SubscribeInteractionClass(ctx, "Control"); err != nil {
		return fmt.Errorf("subscribe Control: %w", err)
	}

	lookahead := waiterLookahead
	if cfg.role == "peer" {
		lookahead = peerLookahead
	}
	if err := fed.EnableTimeRegulation(ctx, lookahead); err != nil {
		return fmt.Errorf("enable time regulation: %w", err)
	}
	if err := fed.EnableTimeConstrained(ctx); err != nil {
		return fmt.Errorf("enable time constrained: %w", err)
	}

	fmt.Printf("[%s] joined %q, logical time=0, lookahead=%g\n", cfg.role, cfg.federation, lookahead)
	if cfg.role == "peer" {
		return runPeer(ctx, fed, cfg)
	}
	return runWaiter(ctx, fed, cfg)
}

func runWaiter(ctx context.Context, fed *federate.Federate, cfg config) error {
	fmt.Println("[waiter] waiting until peer has joined and enabled time regulation...")
	if err := waitForControl(ctx, fed, controlPeerReady); err != nil {
		return fmt.Errorf("wait for peer ready: %w", err)
	}
	if err := sendControl(ctx, fed, controlStartTAR); err != nil {
		return fmt.Errorf("send start signal: %w", err)
	}

	started := time.Now()
	if err := fed.TimeAdvanceRequest(ctx, cfg.target); err != nil {
		return fmt.Errorf("TAR(%g): %w", cfg.target, err)
	}
	fmt.Printf("[waiter] TAR(%g) requested; grant is now blocked by the peer at time 0\n", cfg.target)

	grant, err := waitForGrant(ctx, fed)
	if err != nil {
		return fmt.Errorf("wait for grant: %w", err)
	}
	elapsed := time.Since(started)
	if grant != cfg.target {
		return fmt.Errorf("grant=%g, want %g", grant, cfg.target)
	}
	tolerance := 250 * time.Millisecond
	if elapsed+tolerance < cfg.peerDelay {
		return fmt.Errorf("grant arrived after %s, before peer delay %s elapsed", elapsed.Round(time.Millisecond), cfg.peerDelay)
	}

	fmt.Printf("[waiter] GRANT(%g) after %s: PASS - peer TAR released the pending request\n",
		grant, elapsed.Round(time.Millisecond))
	return nil
}

func runPeer(ctx context.Context, fed *federate.Federate, cfg config) error {
	fmt.Println("[peer] advertising readiness and waiting for waiter TAR start...")
	if err := advertiseUntilStart(ctx, fed); err != nil {
		return fmt.Errorf("ready handshake: %w", err)
	}

	fmt.Printf("[peer] waiter started TAR; holding logical time 0 for %s\n", cfg.peerDelay)
	timer := time.NewTimer(cfg.peerDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return ctx.Err()
	}

	if err := fed.TimeAdvanceRequest(ctx, cfg.target); err != nil {
		return fmt.Errorf("peer TAR(%g): %w", cfg.target, err)
	}
	fmt.Printf("[peer] TAR(%g) requested; LBTS can now cover the target\n", cfg.target)

	grant, err := waitForGrant(ctx, fed)
	if err != nil {
		return fmt.Errorf("wait for peer grant: %w", err)
	}
	if grant != cfg.target {
		return fmt.Errorf("peer grant=%g, want %g", grant, cfg.target)
	}
	fmt.Printf("[peer] GRANT(%g): PASS\n", grant)
	return nil
}

func advertiseUntilStart(ctx context.Context, fed *federate.Federate) error {
	if err := sendControl(ctx, fed, controlPeerReady); err != nil {
		return err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case ev, ok := <-fed.Events():
			if !ok {
				return errors.New("events channel closed")
			}
			if kind, ok := controlKind(ev); ok && kind == controlStartTAR {
				return nil
			}
			if halted, ok := ev.(federate.FederationHalted); ok {
				return fmt.Errorf("federation halted: %s", halted.Reason)
			}
		case <-ticker.C:
			if err := sendControl(ctx, fed, controlPeerReady); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func waitForControl(ctx context.Context, fed *federate.Federate, want uint32) error {
	for {
		select {
		case ev, ok := <-fed.Events():
			if !ok {
				return errors.New("events channel closed")
			}
			if kind, ok := controlKind(ev); ok && kind == want {
				return nil
			}
			if halted, ok := ev.(federate.FederationHalted); ok {
				return fmt.Errorf("federation halted: %s", halted.Reason)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func waitForGrant(ctx context.Context, fed *federate.Federate) (float64, error) {
	for {
		select {
		case ev, ok := <-fed.Events():
			if !ok {
				return 0, errors.New("events channel closed")
			}
			switch typed := ev.(type) {
			case federate.TimeAdvanceGrant:
				return typed.Time, nil
			case federate.FederationHalted:
				return 0, fmt.Errorf("federation halted: %s", typed.Reason)
			}
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

func sendControl(ctx context.Context, fed *federate.Federate, kind uint32) error {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, kind)
	return fed.SendInteraction(ctx, "Control", map[string][]byte{"kind": payload}, nil)
}

func controlKind(ev federate.Event) (uint32, bool) {
	received, ok := ev.(federate.ReceiveInteraction)
	if !ok || received.ClassName != "Control" {
		return 0, false
	}
	payload := received.Parameters["kind"]
	if len(payload) != 4 {
		return 0, false
	}
	return binary.BigEndian.Uint32(payload), true
}

func resign(fed *federate.Federate) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = fed.Resign(ctx)
}

func validateConfig(cfg config) error {
	if cfg.role != "waiter" && cfg.role != "peer" {
		return fmt.Errorf("--role must be waiter or peer, got %q", cfg.role)
	}
	if cfg.target <= 0 || math.IsNaN(cfg.target) || math.IsInf(cfg.target, 0) {
		return errors.New("--target must be positive and finite")
	}
	if cfg.peerDelay < 0 {
		return errors.New("--peer-delay must not be negative")
	}
	if cfg.timeout <= cfg.peerDelay {
		return errors.New("--timeout must be greater than --peer-delay")
	}
	return nil
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.url, "url", "127.0.0.1:8442", "rtid host:port")
	flag.StringVar(&cfg.federation, "federation", "tar-wait-example", "federation name")
	flag.StringVar(&cfg.role, "role", "", "federate role: waiter|peer")
	flag.Float64Var(&cfg.target, "target", 5, "logical time requested with TAR")
	flag.DurationVar(&cfg.peerDelay, "peer-delay", 3*time.Second, "time the peer remains at logical time 0")
	flag.DurationVar(&cfg.timeout, "timeout", 20*time.Second, "overall example timeout")
	flag.StringVar(&cfg.fomPath, "fom", "examples/go-tar-wait/tar-wait-fom.xml", "FOM XML path")
	flag.Parse()
	return cfg
}
