// A test-only Go federate controlled over JSON lines by the Python M21 tests.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

const tick = "HLAinteractionRoot.Tick"

type command struct {
	Op        string   `json:"op"`
	Primitive string   `json:"primitive"`
	Time      float64  `json:"time"`
	Until     *float64 `json:"until"`
	TimeoutMS int      `json:"timeout_ms"`
	Value     uint32   `json:"value"`
}

func collect(ctx context.Context, fed *federate.Federate, cmd command) ([]map[string]any, error) {
	timer := time.NewTimer(time.Duration(cmd.TimeoutMS) * time.Millisecond)
	defer timer.Stop()
	events := make([]map[string]any, 0)
	for {
		select {
		case event, ok := <-fed.Events():
			if !ok {
				return nil, errors.New("callback stream closed")
			}
			switch event := event.(type) {
			case federate.TimeAdvanceGrant:
				events = append(events, map[string]any{"kind": "grant", "time": event.Time})
				if cmd.Until != nil && event.Time == *cmd.Until {
					return events, nil
				}
			case federate.ReceiveInteraction:
				payload := event.Parameters["seq"]
				if len(payload) != 4 || event.Timestamp == nil {
					return nil, fmt.Errorf("invalid timestamped Tick: %#v", event)
				}
				events = append(events, map[string]any{
					"kind": "receive", "time": *event.Timestamp,
					"value": binary.BigEndian.Uint32(payload), "class": event.ClassName,
				})
			case federate.FederationHalted:
				return nil, fmt.Errorf("federation halted: %s", event.Reason)
			}
		case <-timer.C:
			if cmd.Until != nil {
				return nil, fmt.Errorf("grant %g not observed; events=%v", *cmd.Until, events)
			}
			return events, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func run() (err error) {
	url := flag.String("url", "", "RTI endpoint")
	federation := flag.String("federation", "", "federation name")
	name := flag.String("name", "go-peer", "federate name")
	fom := flag.String("fom", "", "FOM path")
	regulator := flag.Bool("regulator", false, "enable time regulation")
	flag.Parse()
	xml, err := os.ReadFile(*fom)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	conn, err := federate.Connect(ctx, *url)
	if err != nil {
		return err
	}
	defer conn.Close()
	fed, err := conn.JoinFederation(ctx, federate.FederationSpec{
		Name: *federation, FOMModules: []federate.FOMModule{{Path: *fom, XML: xml}},
	}, *name)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		err = errors.Join(err, fed.Resign(cleanup))
	}()
	if err := fed.PublishInteractionClass(ctx, tick); err != nil {
		return err
	}
	if err := fed.SubscribeInteractionClass(ctx, tick); err != nil {
		return err
	}
	if *regulator {
		if err := fed.EnableTimeRegulation(ctx, 1); err != nil {
			return err
		}
	}
	if err := fed.EnableTimeConstrained(ctx); err != nil {
		return err
	}
	output := json.NewEncoder(os.Stdout)
	if err := output.Encode(map[string]any{
		"ready": true, "pid": os.Getpid(), "handle": fed.Handle(), "language": "go",
	}); err != nil {
		return err
	}
	input := json.NewDecoder(os.Stdin)
	for {
		var cmd command
		if err := input.Decode(&cmd); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		response := map[string]any{"op": cmd.Op}
		switch cmd.Op {
		case "request":
			methods := map[string]func(context.Context, float64) error{
				"TAR": fed.TimeAdvanceRequest, "TARA": fed.TimeAdvanceRequestAvailable,
				"NMR": fed.NextMessageRequest, "NMRA": fed.NextMessageRequestAvailable,
				"FQR": fed.FlushQueueRequest,
			}
			method := methods[cmd.Primitive]
			if method == nil {
				return fmt.Errorf("unknown advance primitive %q", cmd.Primitive)
			}
			if err := method(ctx, cmd.Time); err != nil {
				return err
			}
			response["primitive"] = cmd.Primitive
		case "send":
			payload := make([]byte, 4)
			binary.BigEndian.PutUint32(payload, cmd.Value)
			if err := fed.SendInteractionConfirmed(ctx, tick, map[string][]byte{"seq": payload}, &cmd.Time); err != nil {
				return err
			}
		case "collect":
			events, err := collect(ctx, fed, cmd)
			if err != nil {
				return err
			}
			response["events"] = events
		case "quit":
			return nil
		default:
			return fmt.Errorf("unknown operation %q", cmd.Op)
		}
		logical, err := fed.QueryLogicalTime(ctx)
		if err != nil {
			return err
		}
		response["logical_time"] = logical
		if err := output.Encode(response); err != nil {
			return err
		}
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
