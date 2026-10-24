package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cbchoi/gorti/examples/multilanguage-queue/workstation"
	"github.com/cbchoi/gorti/rti/pkg/federate"
)

const jobClass = "HLAinteractionRoot.Job"
const completionClass = "HLAinteractionRoot.Completion"

func writeJSON(path string, data any) error {
	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0600)
}

func run() (err error) {
	url := flag.String("url", "", "RTI endpoint")
	federationName := flag.String("federation", "", "Federation name")
	fom := flag.String("fom", "", "FOM file")
	directory := flag.String("output", "", "Run directory")
	horizon := flag.Int("horizon", 0, "Final logical time")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	xml, err := os.ReadFile(*fom)
	if err != nil {
		return err
	}
	conn, err := federate.Connect(ctx, *url)
	if err != nil {
		return err
	}
	defer conn.Close()
	fed, err := conn.JoinFederation(ctx, federate.FederationSpec{
		Name: *federationName, FOMModules: []federate.FOMModule{{Path: *fom, XML: xml}},
	}, "go-workstation")
	if err != nil {
		return err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		err = errors.Join(err, fed.Resign(cleanup))
	}()
	if err = fed.PublishInteractionClass(ctx, completionClass); err != nil {
		return err
	}
	if err = fed.SubscribeInteractionClass(ctx, jobClass); err != nil {
		return err
	}
	if err = fed.EnableTimeRegulation(ctx, 1); err != nil {
		return err
	}
	if err = fed.EnableTimeConstrained(ctx); err != nil {
		return err
	}
	model := &workstation.Server{}
	rows := []workstation.Completion{}
	grants := []float64{}
	enabled := 0
	handle := func(event federate.Event) error {
		switch value := event.(type) {
		case federate.TimeRegulationEnabled, federate.TimeConstrainedEnabled:
			enabled++
		case federate.TimeAdvanceGrant:
			grants = append(grants, value.Time)
		case federate.ReceiveInteraction:
			seq, duration := value.Parameters["seq"], value.Parameters["service"]
			if (value.ClassName != jobClass && value.ClassName != "Job") || value.Timestamp == nil || len(seq) != 4 || len(duration) != 4 {
				return fmt.Errorf("invalid timestamped Job: class=%q timestamp=%v parameters=%v", value.ClassName, value.Timestamp, value.Parameters)
			}
			row, e := model.Accept(workstation.Job{Sequence: binary.BigEndian.Uint32(seq), Arrival: *value.Timestamp, Service: binary.BigEndian.Uint32(duration)})
			if e != nil {
				return e
			}
			if e = fed.SendInteractionConfirmed(ctx, completionClass, map[string][]byte{"seq": seq}, &row.Finish); e != nil {
				return e
			}
			rows = append(rows, row)
		case federate.FederationHalted:
			return fmt.Errorf("federation halted: %s", value.Reason)
		}
		return nil
	}
	next := func() error {
		select {
		case event, ok := <-fed.Events():
			if !ok {
				return errors.New("callback stream closed")
			}
			a, r := fed.DispatchEvent(ctx, event, handle)
			return errors.Join(a, r)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for enabled < 2 {
		if err = next(); err != nil {
			return err
		}
	}
	if err = writeJSON(filepath.Join(*directory, "go-ready.json"), map[string]any{"pid": os.Getpid(), "handle": fed.Handle()}); err != nil {
		return err
	}
	for {
		if _, err = os.Stat(filepath.Join(*directory, "start")); err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	for target := 1; target <= *horizon; target++ {
		if err = fed.TimeAdvanceRequest(ctx, float64(target)); err != nil {
			return err
		}
		for len(grants) < target {
			if err = next(); err != nil {
				return err
			}
		}
		if grants[target-1] != float64(target) {
			return errors.New("unexpected grant")
		}
	}
	return writeJSON(filepath.Join(*directory, "go-result.json"), map[string]any{"pid": os.Getpid(), "handle": fed.Handle(), "jobs": rows, "grants": grants})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
