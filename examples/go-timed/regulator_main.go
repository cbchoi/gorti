// regulator_main.go — federate subprocess entry. Dials rtid via
// rti/pkg/federate, enables time regulation + (optionally) constrained,
// then runs `cycles` advance-request cycles. Each cycle issues either
// NER or TAR (per --primitive flag) and waits for the matching grant
// on the federate's Events() channel before issuing the next.
//
// On exit writes the result JSON: federate name, lookahead, primitive,
// list of grant times, and Tick send count.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprintln(os.Stderr, "regulator_main:", err)
		os.Exit(1)
	}
}

type mainArgs struct {
	url         string
	federation  string
	name        string
	lookahead   float64
	primitive   string // "NER" or "TAR"
	constrained bool
	cycles      int
	tickStep    float64 // logical time advance per cycle
	resultPath  string
	fomPath     string
}

func mainErr() error {
	args := parseArgs()

	// Read FOM bytes.
	xml, err := os.ReadFile(args.fomPath)
	if err != nil {
		return fmt.Errorf("read FOM: %w", err)
	}

	ctx := context.Background()
	conn, err := federate.Connect(ctx, args.url)
	if err != nil {
		return fmt.Errorf("connect %s: %w", args.url, err)
	}
	defer conn.Close()

	fmt.Fprintf(os.Stderr, "regulator_main[%s]: connected to %s\n", args.name, args.url)

	fed, err := conn.JoinFederation(ctx, federate.FederationSpec{
		Name:                args.federation,
		FOMModules:          []federate.FOMModule{{Path: args.fomPath, XML: xml}},
		StallTimeoutSeconds: 30,
	}, args.name)
	if err != nil {
		return fmt.Errorf("join: %w", err)
	}
	defer fed.Resign(ctx)

	fmt.Fprintf(os.Stderr, "regulator_main[%s]: joined as handle %d\n", args.name, fed.Handle())

	// Declare pub/sub Tick.
	if err := fed.PublishInteractionClass(ctx, "Tick"); err != nil {
		return fmt.Errorf("publish Tick: %w", err)
	}
	if err := fed.SubscribeInteractionClass(ctx, "Tick"); err != nil {
		return fmt.Errorf("subscribe Tick: %w", err)
	}

	// Enable regulation + (optionally) constrained.
	if err := fed.EnableTimeRegulation(ctx, args.lookahead); err != nil {
		return fmt.Errorf("EnableTimeRegulation: %w", err)
	}
	if args.constrained {
		if err := fed.EnableTimeConstrained(ctx); err != nil {
			return fmt.Errorf("EnableTimeConstrained: %w", err)
		}
	}

	r := &regulator{name: args.name, lookahead: args.lookahead}

	// Cycle loop. Each iteration issues an advance primitive, waits
	// for a *full* grant (M22 W3 — see waitForFullGrant for why
	// forced grants don't end the cycle), then emits a Tick at the
	// earliest valid TSO time after the grant.
	for i := 1; i <= args.cycles; i++ {
		t := float64(i) * args.tickStep
		switch args.primitive {
		case "NER":
			err = fed.NextMessageRequest(ctx, t)
		case "TAR":
			err = fed.TimeAdvanceRequest(ctx, t)
		default:
			return fmt.Errorf("unknown --primitive %q", args.primitive)
		}
		if err != nil {
			return fmt.Errorf("cycle %d %s(%v): %w", i, args.primitive, t, err)
		}

		// Wait for full grant. NER may produce intermediate forced
		// grants when this federate is sole-pending and LBTS < t;
		// per IEEE 1516.1 those are advisory ("messages with ts <=
		// LBTS are deliverable now") and the federate stays in
		// time-advancing-state until the full grant arrives. TAR
		// produces a single grant per request and finishes the cycle.
		grantT, err := waitForFullGrant(fed, t, args.primitive, 30*time.Second)
		if err != nil {
			return fmt.Errorf("cycle %d wait grant: %w", i, err)
		}
		r.grants = append(r.grants, grantT)

		// Emit a Tick at the earliest TSO time allowed after the grant.
		payload := r.nextTickPayload()
		sendT := r.tickTimestamp(grantT)
		if err := fed.SendInteraction(ctx, "Tick",
			map[string][]byte{"seq": payload}, &sendT); err != nil {
			return fmt.Errorf("cycle %d send Tick: %w", i, err)
		}
	}

	fmt.Fprintf(os.Stderr, "regulator_main[%s]: done — %d grants\n", args.name, len(r.grants))

	return writeResult(args.resultPath, r, args)
}

// waitForFullGrant drains f.Events() until a *full* TimeAdvanceGrant
// is observed (grant.Time >= requested), accumulating any
// intermediate forced grants (NER/NMRA only — TAR/TARA/FQR clear
// pending on every grant per advance.go::decideGrant). For TAR the
// loop returns on the first grant since one-grant-per-request is
// TAR's contract.
//
// M22 W3: this replaces the M21-era waitForGrant + 5ms settle delay.
// The settle delay was masking the SDK-side semantic gap, not a
// server race. Per spec a federate stays in time-advancing-state
// across forced grants; reissuing an advance primitive at that point
// correctly returns ErrTimeAdvancingState.
func waitForFullGrant(f *federate.Federate, requested float64, primitive string, timeout time.Duration) (float64, error) {
	tarLike := primitive == "TAR" || primitive == "TARA" || primitive == "FQR"
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case ev, ok := <-f.Events():
			if !ok {
				return 0, fmt.Errorf("events channel closed before grant")
			}
			g, ok := ev.(federate.TimeAdvanceGrant)
			if !ok {
				// ReceiveInteraction etc.: ignore.
				continue
			}
			if tarLike || g.Time >= requested {
				return g.Time, nil
			}
			// NER/NMRA forced grant (g.Time < requested) — accumulate
			// and keep waiting.
			fmt.Fprintf(os.Stderr, "regulator: forced grant @ %v < requested %v; waiting for full\n", g.Time, requested)
		case <-deadline.C:
			return 0, fmt.Errorf("deadline waiting for full grant (requested=%v)", requested)
		}
	}
}

func writeResult(path string, r *regulator, args mainArgs) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload := map[string]any{
		"name":        r.name,
		"lookahead":   r.lookahead,
		"primitive":   args.primitive,
		"constrained": args.constrained,
		"grants":      r.grants,
		"ticks_sent":  r.tickSeq,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func parseArgs() mainArgs {
	var a mainArgs
	flag.StringVar(&a.url, "url", "127.0.0.1:8442", "rtid host:port")
	flag.StringVar(&a.federation, "federation", "go-timed", "federation name")
	flag.StringVar(&a.name, "name", "fast", "federate name (fast|normal|slow)")
	flag.Float64Var(&a.lookahead, "lookahead", 0.5, "regulation lookahead")
	flag.StringVar(&a.primitive, "primitive", "NER", "advance primitive (NER|TAR)")
	flag.BoolVar(&a.constrained, "constrained", true, "enable time-constrained")
	flag.IntVar(&a.cycles, "cycles", 10, "number of advance cycles")
	flag.Float64Var(&a.tickStep, "tick-step", 1.0, "logical time advance per cycle")
	flag.StringVar(&a.resultPath, "result", "/tmp/go-timed/result.json", "result JSON path")
	flag.StringVar(&a.fomPath, "fom", "examples/go-timed/time-advance-fom.xml", "FOM XML path")
	flag.Parse()
	return a
}
