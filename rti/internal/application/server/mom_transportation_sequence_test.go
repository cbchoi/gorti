package server

import (
	"context"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMOMTransportationConcurrentSequenceMatchesQueueOrder(t *testing.T) {
	f := newMOMExecutionFixture(t, momServiceRequestAttributeTransportationTypeChange)
	deps := f.executor.(*momServiceExecutor).deps
	const fed = "catalog-provider"
	queue := delivery.NewWithBatch(16, 1, 0)
	queue.Bind(fed, f.target)
	confirmation, err := newMOMTransportationConfirmation(momTransportationConfirmationDependencies{Federations: deps.Federations, Float: deps.FloatObjects, Integer: deps.IntegerObjects, CallbackEpoch: queue.CallbackEpoch, Outbox: queue})
	if err != nil {
		t.Fatal(err)
	}
	commands := make([]mom.MOMServiceCommand, 12)
	for i := range commands {
		object, _, err := deps.FloatObjects.Register(context.Background(), fed, f.target, f.command.ObjectClass, "")
		if err != nil {
			t.Fatal(err)
		}
		commands[i] = f.command
		commands[i].Object = object
	}
	var wg sync.WaitGroup
	errors := make(chan error, len(commands))
	for _, command := range commands {
		wg.Add(1)
		go func(command mom.MOMServiceCommand) {
			defer wg.Done()
			errors <- confirmation(context.Background(), fed, f.target, command, core.TransportTypeBestEffort)
		}(command)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	stream, release, err := queue.Subscribe(context.Background(), fed, f.target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	}()
	var last uint64
	tickets := map[string]bool{}
	for range commands {
		batch := <-stream
		if len(batch) != 1 {
			t.Fatal("unexpected batch size")
		}
		event := batch[0].(*momTransportationEvent).Inner()
		if event.Seq <= last {
			t.Fatalf("queue sequence reversed: %d <= %d", event.Seq, last)
		}
		last = event.Seq
		ticket := string(event.GetTransportationChangeConfirmed().Ticket)
		if len(ticket) != 32 || tickets[ticket] {
			t.Fatal("confirmation tickets are not distinct")
		}
		tickets[ticket] = true
	}
}

func TestMOMTransportationInvalidSequenceRollsBackOnlyPendingTicket(t *testing.T) {
	f := newMOMExecutionFixture(t, momServiceRequestInteractionTransportationTypeChange)
	deps := f.executor.(*momServiceExecutor).deps
	const fed = "catalog-provider"
	queue := delivery.NewWithBatch(4, 1, 0)
	queue.Bind(fed, f.target)
	sequence := uint64(0)
	confirmation, err := newMOMTransportationConfirmation(momTransportationConfirmationDependencies{Federations: deps.Federations, Float: deps.FloatObjects, Integer: deps.IntegerObjects, CallbackEpoch: queue.CallbackEpoch, Outbox: queue, NextSequence: func(core.FederationName) uint64 { return sequence }})
	if err != nil {
		t.Fatal(err)
	}
	if err := confirmation(context.Background(), fed, f.target, f.command, core.TransportTypeBestEffort); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("zero sequence accepted: %v", err)
	}
	sequence = 41
	if err := confirmation(context.Background(), fed, f.target, f.command, core.TransportTypeBestEffort); err != nil {
		t.Fatal("failed sequence left a pending ticket:", err)
	}
	stream, release, err := queue.Subscribe(context.Background(), fed, f.target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	}()
	event := (<-stream)[0].(*momTransportationEvent).Inner()
	if event.Seq != 41 {
		t.Fatalf("supplied sequence not retained: %d", event.Seq)
	}
	identity, err := deps.Federations.MemberIdentity(fed, f.target)
	if err != nil {
		t.Fatal(err)
	}
	change := event.GetTransportationChangeConfirmed()
	if _, err := deps.FloatObjects.ActivateTransportationChange(context.Background(), core.TransportationScope{Member: identity, CallbackEpoch: change.CallbackEpoch}, change.Ticket); err != nil {
		t.Fatal(err)
	}
	if err := confirmation(context.Background(), fed, f.target, f.command, core.TransportTypeReliable); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("duplicate sequence accepted: %v", err)
	}
	sequence = 42
	if err := confirmation(context.Background(), fed, f.target, f.command, core.TransportTypeReliable); err != nil {
		t.Fatal("duplicate-sequence rejection left pending ticket:", err)
	}
	if next := (<-stream)[0].(*momTransportationEvent).Inner(); next.Seq != 42 {
		t.Fatalf("retry sequence=%d", next.Seq)
	}
}
