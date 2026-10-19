package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

type startupStub struct {
	handle      uint64
	events      chan federate.Event
	registered  []uint64
	achieved    bool
	dispatching bool
	reportErr   error
}

func (f *startupStub) Handle() uint64                { return f.handle }
func (f *startupStub) Events() <-chan federate.Event { return f.events }
func (f *startupStub) ListFederationMembers(context.Context) ([]federate.FederationMember, error) {
	return []federate.FederationMember{{Handle: 2}, {Handle: 1}}, nil
}
func (f *startupStub) RegisterFederationSynchronizationPoint(_ context.Context, _ string, _ []byte, handles []uint64) error {
	f.registered = handles
	return nil
}
func (f *startupStub) SynchronizationPointAchieved(context.Context, string, bool) error {
	if !f.dispatching {
		return errors.New("achieved outside callback entry")
	}
	f.achieved = true
	return nil
}
func (f *startupStub) DispatchEvent(_ context.Context, event federate.Event, handler func(federate.Event) error) (error, error) {
	f.dispatching = true
	defer func() { f.dispatching = false }()
	return handler(event), f.reportErr
}

func TestStartupBarrierWaitsForSynchronizationAndDispatchesAnnouncement(t *testing.T) {
	for _, handle := range []uint64{1, 2} {
		f := &startupStub{handle: handle, events: make(chan federate.Event, 3)}
		f.events <- federate.SynchronizationPointAnnounced{Label: startupLabel}
		f.events <- federate.FederationSynchronized{Label: "unrelated"}
		f.events <- federate.FederationSynchronized{Label: startupLabel}
		if err := waitForStartup(context.Background(), f, 2); err != nil {
			t.Fatal(err)
		}
		if !f.achieved || len(f.events) != 0 {
			t.Fatalf("barrier incomplete: achieved=%v events=%d", f.achieved, len(f.events))
		}
		if handle == 1 && !reflect.DeepEqual(f.registered, []uint64{2, 1}) {
			t.Fatalf("leader did not register every member: %v", f.registered)
		}
		if handle == 2 && f.registered != nil {
			t.Fatal("non-leader registered a second synchronization point")
		}
	}
}

func TestStartupBarrierRejectsFailureAndClosedStream(t *testing.T) {
	for _, event := range []federate.Event{
		federate.FederationSynchronized{Label: startupLabel, FailedToSync: []uint64{2}},
		federate.SynchronizationPointRegistrationFailed{Label: startupLabel},
		federate.FederationHalted{Reason: "stopped"},
	} {
		f := &startupStub{handle: 1, events: make(chan federate.Event, 1)}
		f.events <- event
		close(f.events)
		if err := waitForStartup(context.Background(), f, 2); err == nil {
			t.Fatalf("accepted failure: %T", event)
		}
	}
	f := &startupStub{handle: 1, events: make(chan federate.Event)}
	close(f.events)
	if err := waitForStartup(context.Background(), f, 2); err == nil {
		t.Fatal("accepted a closed callback stream")
	}
}

func TestStartupBarrierPropagatesReportFailureAndCancellation(t *testing.T) {
	want := errors.New("report failed")
	f := &startupStub{handle: 1, events: make(chan federate.Event, 1), reportErr: want}
	f.events <- federate.FederationSynchronized{Label: startupLabel}
	if err := waitForStartup(context.Background(), f, 2); !errors.Is(err, want) {
		t.Fatalf("got %v, want report failure", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.reportErr = nil
	if err := waitForStartup(ctx, f, 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
	if err := waitForStartup(ctx, f, 3); !errors.Is(err, context.Canceled) {
		t.Fatalf("membership wait: got %v, want cancellation", err)
	}
	if err := waitForStartup(ctx, f, 0); err == nil {
		t.Fatal("accepted zero participants")
	}
}
