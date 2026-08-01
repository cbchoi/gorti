package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

func TestEventStateWaitReturnsWhenPredicateAlreadySatisfied(t *testing.T) {
	state := newEventState(config{Role: rolePublisher, Count: 1})
	state.mu.Lock()
	state.nameReserved = true
	state.mu.Unlock()

	err := state.wait(context.Background(), time.Second, "reservation", func(s *eventState) bool {
		return s.nameReserved
	})
	if err != nil {
		t.Fatalf("wait returned %v for an already-satisfied predicate", err)
	}
}

func TestEventStateWaitAlreadySatisfiedPrecedesCanceledContext(t *testing.T) {
	state := newEventState(config{Role: rolePublisher, Count: 1})
	state.mu.Lock()
	state.nameReserved = true
	state.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := state.wait(ctx, time.Second, "reservation", func(s *eventState) bool {
		return s.nameReserved
	})
	if err != nil {
		t.Fatalf("already-satisfied wait with canceled context returned %v", err)
	}
}

func TestEventStateWaitDoesNotMissBlockedWaiterNotification(t *testing.T) {
	state := newEventState(config{Role: rolePublisher, Count: 1})
	result := startBlockedEventStateWait(
		t,
		state,
		context.Background(),
		5*time.Second,
		"reservation",
		func(s *eventState) bool { return s.nameReserved },
	)

	state.mu.Lock()
	state.nameReserved = true
	state.notifyLocked()
	state.mu.Unlock()

	if err := eventStateWaitResult(t, result); err != nil {
		t.Fatalf("blocked wait missed notification: %v", err)
	}
}

func TestEventStateWaitBroadcastsToMultipleWaiters(t *testing.T) {
	state := newEventState(config{Role: rolePublisher, Count: 1})
	const waiterCount = 3
	results := make([]<-chan error, waiterCount)

	for index := range results {
		results[index] = startBlockedEventStateWait(
			t,
			state,
			context.Background(),
			5*time.Second,
			"reservation",
			func(s *eventState) bool { return s.nameReserved },
		)
	}

	state.mu.Lock()
	state.nameReserved = true
	state.notifyLocked()
	state.mu.Unlock()

	for _, result := range results {
		if err := eventStateWaitResult(t, result); err != nil {
			t.Fatalf("broadcast waiter returned %v", err)
		}
	}
}

func TestEventStateWaitReturnsContextTimeout(t *testing.T) {
	state := newEventState(config{Role: rolePublisher, Count: 1})
	result := startBlockedEventStateWait(
		t,
		state,
		context.Background(),
		100*time.Millisecond,
		"never satisfied",
		func(*eventState) bool { return false },
	)
	err := eventStateWaitResult(t, result)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait error = %v, want context deadline exceeded", err)
	}
}

func TestEventStateWaitReturnsCallbackFailure(t *testing.T) {
	state := newEventState(config{Role: rolePublisher, Count: 1})
	result := startBlockedEventStateWait(
		t,
		state,
		context.Background(),
		5*time.Second,
		"callback",
		func(*eventState) bool { return false },
	)

	state.accept(federate.FederationHalted{Reason: "test callback failure"}, counterNow())
	err := eventStateWaitResult(t, result)
	if err == nil || !strings.Contains(err.Error(), "test callback failure") {
		t.Fatalf("wait error = %v, want callback failure", err)
	}
}

func TestEventStateWaitReturnsUnexpectedStreamClosure(t *testing.T) {
	state := newEventState(config{Role: rolePublisher, Count: 1})
	result := startBlockedEventStateWait(
		t,
		state,
		context.Background(),
		5*time.Second,
		"stream callback",
		func(*eventState) bool { return false },
	)

	state.streamClosed()
	err := eventStateWaitResult(t, result)
	if err == nil || !strings.Contains(err.Error(), "event stream closed unexpectedly") {
		t.Fatalf("wait error = %v, want unexpected stream closure", err)
	}
}

func TestEventStateWaitCancellationWakesBlockedWait(t *testing.T) {
	state := newEventState(config{Role: rolePublisher, Count: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := startBlockedEventStateWait(
		t,
		state,
		ctx,
		5*time.Second,
		"reservation",
		func(s *eventState) bool { return s.nameReserved },
	)

	state.mu.Lock()
	cancel()
	state.notifyLocked()
	state.mu.Unlock()

	if err := eventStateWaitResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error = %v, want context canceled", err)
	}
}

func TestEventStateWaitDeadlineWakesBlockedWait(t *testing.T) {
	state := newEventState(config{Role: rolePublisher, Count: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result := startBlockedEventStateWait(
		t,
		state,
		ctx,
		5*time.Second,
		"reservation",
		func(s *eventState) bool { return s.nameReserved },
	)

	state.mu.Lock()
	<-ctx.Done()
	state.notifyLocked()
	state.mu.Unlock()

	if err := eventStateWaitResult(t, result); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait error = %v, want deadline exceeded", err)
	}
}

func TestEventStateWaitFailurePrecedesSatisfiedPredicate(t *testing.T) {
	wantErr := errors.New("callback failed")

	t.Run("initial check", func(t *testing.T) {
		state := newEventState(config{Role: rolePublisher, Count: 1})
		state.mu.Lock()
		state.failureErr = wantErr
		state.nameReserved = true
		state.mu.Unlock()

		err := state.wait(context.Background(), time.Second, "reservation", func(s *eventState) bool {
			return s.nameReserved
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("wait error = %v, want failure before satisfied predicate", err)
		}
	})

	t.Run("after blocking", func(t *testing.T) {
		state := newEventState(config{Role: rolePublisher, Count: 1})
		result := startBlockedEventStateWait(
			t,
			state,
			context.Background(),
			5*time.Second,
			"reservation",
			func(s *eventState) bool { return s.nameReserved },
		)

		state.mu.Lock()
		state.failureErr = wantErr
		state.nameReserved = true
		state.notifyLocked()
		state.mu.Unlock()

		if err := eventStateWaitResult(t, result); !errors.Is(err, wantErr) {
			t.Fatalf("wait error = %v, want failure before satisfied predicate", err)
		}
	})
}

func startBlockedEventStateWait(
	t *testing.T,
	state *eventState,
	ctx context.Context,
	timeout time.Duration,
	description string,
	predicate func(*eventState) bool,
) <-chan error {
	t.Helper()
	firstPredicateCheck := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		predicateChecks := 0
		result <- state.wait(ctx, timeout, description, func(s *eventState) bool {
			predicateChecks++
			if predicateChecks == 1 {
				close(firstPredicateCheck)
			}
			return predicate(s)
		})
	}()

	select {
	case <-firstPredicateCheck:
	case err := <-result:
		t.Fatalf("wait returned before its first predicate check: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the first predicate check")
	}

	// The predicate and change-channel capture happen while holding state.mu.
	// Acquiring the mutex proves that the waiter captured the channel before the
	// test changes state or broadcasts.
	state.mu.Lock()
	state.mu.Unlock()
	select {
	case err := <-result:
		t.Fatalf("wait returned before notification: %v", err)
	default:
	}
	return result
}

func eventStateWaitResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for eventState.wait result")
		return nil
	}
}

func TestSubscriberStrictlyAcceptsOneCompleteBatch(t *testing.T) {
	cfg := config{Role: roleSubscriber, Seed: "1516", Count: 1}
	state := newEventState(cfg)
	if err := state.setParticipants([]uint64{3, 8}); err != nil {
		t.Fatalf("setParticipants: %v", err)
	}
	state.accept(federate.SynchronizationPointAnnounced{
		Label: readySync, RequiredFederates: []uint64{8, 3},
	}, counterNow())
	state.accept(federate.FederationSynchronized{Label: readySync}, counterNow())
	state.accept(federate.DiscoverObjectInstance{
		ObjectHandle: 17, ClassName: objectClass, ObjectName: objectName,
	}, counterNow())
	if err := state.prepareAdvance(1, 0); err != nil {
		t.Fatal(err)
	}
	item, err := preencodeWorkload(cfg.Seed, cfg.Count)
	if err != nil {
		t.Fatal(err)
	}
	timestamp := 1.0
	state.accept(federate.ReflectAttributeValues{
		ObjectHandle: 17, ClassName: objectClass, Attributes: item[0].attributes, Timestamp: &timestamp,
	}, counterNow())
	state.accept(federate.ReceiveInteraction{
		ClassName: interactionClass, Parameters: item[0].parameters, Timestamp: &timestamp,
	}, counterNow())
	state.accept(federate.TimeAdvanceGrant{Time: 1}, counterNow())
	if err := state.failure(); err != nil {
		t.Fatal(err)
	}
	accounting := state.accounting()
	if accounting.Delivered != 2 || accounting.Rejected != 0 || accounting.Dropped != 0 {
		t.Fatalf("accounting = %+v", accounting)
	}
}

func TestSubscriberHandleCallbacksMatchNameCallbacks(t *testing.T) {
	cfg := config{Role: roleSubscriber, Seed: "1516", Count: 1}
	items, err := preencodeWorkload(cfg.Seed, cfg.Count)
	if err != nil {
		t.Fatal(err)
	}
	timestamp := 1.0

	named := newEventState(cfg)
	named.accept(federate.DiscoverObjectInstance{
		ObjectHandle: 17, ClassName: objectClass, ObjectName: objectName,
	}, counterNow())
	if err := named.prepareAdvance(1, 0); err != nil {
		t.Fatal(err)
	}
	named.accept(federate.ReflectAttributeValues{
		ObjectHandle: 17, ClassName: objectClass,
		Attributes: items[0].attributes, Timestamp: &timestamp,
	}, counterNow())
	named.accept(federate.ReceiveInteraction{
		ClassName: interactionClass, Parameters: items[0].parameters, Timestamp: &timestamp,
	}, counterNow())

	handles := callbackHandleSet{
		objectClass: 7, attributeSequence: 2, attributePayload: 3,
		interactionClass: 9, interactionSequence: 4, interactionPayload: 5,
	}
	raw := newEventState(cfg)
	raw.setDeliveryHandles(handles)
	raw.accept(federate.DiscoverObjectInstance{
		ObjectHandle: 17, ClassName: objectClass, ObjectName: objectName,
	}, counterNow())
	if err := raw.prepareAdvance(1, 0); err != nil {
		t.Fatal(err)
	}
	raw.accept(federate.ReflectAttributeValuesByHandle{
		ObjectHandle: 17, ObjectClassHandle: handles.objectClass,
		Attributes: map[uint64][]byte{
			handles.attributeSequence: items[0].attributes[sequenceField],
			handles.attributePayload:  items[0].attributes[payloadField],
		},
		Timestamp: &timestamp,
	}, counterNow())
	raw.accept(federate.ReceiveInteractionByHandle{
		InteractionClassHandle: handles.interactionClass,
		Parameters: map[uint64][]byte{
			handles.interactionSequence: items[0].parameters[sequenceField],
			handles.interactionPayload:  items[0].parameters[payloadField],
		},
		Timestamp: &timestamp,
	}, counterNow())

	if err := named.failure(); err != nil {
		t.Fatalf("name callbacks: %v", err)
	}
	if err := raw.failure(); err != nil {
		t.Fatalf("handle callbacks: %v", err)
	}
	if got, want := raw.accounting(), named.accounting(); got != want {
		t.Fatalf("handle accounting = %+v, want %+v", got, want)
	}
	rawAttribute, rawInteraction, rawTrace := raw.callbackDigests()
	nameAttribute, nameInteraction, nameTrace := named.callbackDigests()
	if rawAttribute != nameAttribute || rawInteraction != nameInteraction || rawTrace != nameTrace {
		t.Fatalf(
			"handle callback digests = (%s, %s, %s), want (%s, %s, %s)",
			rawAttribute, rawInteraction, rawTrace,
			nameAttribute, nameInteraction, nameTrace,
		)
	}
}

func TestSubscriberRejectsWrongPayloadAndAccountsDisposition(t *testing.T) {
	cfg := config{Role: roleSubscriber, Seed: "1516", Count: 1}
	state := newEventState(cfg)
	state.accept(federate.DiscoverObjectInstance{
		ObjectHandle: 17, ClassName: objectClass, ObjectName: objectName,
	}, counterNow())
	if err := state.prepareAdvance(1, 0); err != nil {
		t.Fatal(err)
	}
	items, _ := preencodeWorkload(cfg.Seed, cfg.Count)
	items[0].attributes[payloadField], _ = encodeASCIIString("wrong")
	timestamp := 1.0
	state.accept(federate.ReflectAttributeValues{
		ObjectHandle: 17, ClassName: objectClass, Attributes: items[0].attributes, Timestamp: &timestamp,
	}, counterNow())
	if state.failure() == nil {
		t.Fatal("wrong payload was accepted")
	}
	accounting := state.accounting()
	if accounting.Rejected != 1 || accounting.Dropped != 1 || accounting.Delivered != 0 || accounting.Invalid != 1 {
		t.Fatalf("accounting = %+v", accounting)
	}
}

func TestDuplicateDeliveryIsReportedSeparately(t *testing.T) {
	cfg := config{Role: roleSubscriber, Seed: "1516", Count: 1}
	state := newEventState(cfg)
	state.accept(federate.DiscoverObjectInstance{
		ObjectHandle: 17, ClassName: objectClass, ObjectName: objectName,
	}, counterNow())
	if err := state.prepareAdvance(1, 0); err != nil {
		t.Fatal(err)
	}
	items, _ := preencodeWorkload(cfg.Seed, cfg.Count)
	timestamp := 1.0
	event := federate.ReceiveInteraction{
		ClassName: interactionClass, Parameters: items[0].parameters, Timestamp: &timestamp,
	}
	state.accept(event, counterNow())
	state.accept(event, counterNow())
	accounting := state.accounting()
	if accounting.Duplicates != 1 || accounting.Invalid != 0 {
		t.Fatalf("accounting = %+v", accounting)
	}
}

func TestPublisherRequiresSuccessfulFixedNameReservation(t *testing.T) {
	state := newEventState(config{Role: rolePublisher, Count: 1})
	state.accept(federate.ObjectInstanceNameReservationSucceeded{ObjectName: objectName}, counterNow())
	if err := state.failure(); err != nil || !state.nameReserved {
		t.Fatalf("success callback: reserved=%t err=%v", state.nameReserved, err)
	}

	failed := newEventState(config{Role: rolePublisher, Count: 1})
	failed.accept(federate.ObjectInstanceNameReservationFailed{ObjectName: objectName}, counterNow())
	if failed.failure() == nil {
		t.Fatal("failed reservation callback was accepted")
	}
}

func TestSynchronizationRequiresExactParticipants(t *testing.T) {
	state := newEventState(config{Role: rolePublisher, Count: 1})
	if err := state.setParticipants([]uint64{1, 2}); err != nil {
		t.Fatalf("setParticipants: %v", err)
	}
	state.accept(federate.SynchronizationPointAnnounced{
		Label: readySync, RequiredFederates: []uint64{1, 3},
	}, counterNow())
	if state.failure() == nil {
		t.Fatal("wrong synchronization participants were accepted")
	}
}

func TestMeasurementSynchronizationRequiresOperationWarmup(t *testing.T) {
	withoutWarmup := newEventState(config{Role: rolePublisher, Count: 1, ReceiveOrder: true})
	withoutWarmup.accept(federate.SynchronizationPointAnnounced{Label: measureSync}, counterNow())
	if withoutWarmup.failure() == nil {
		t.Fatal("measurement synchronization was accepted without operation warmup")
	}

	withWarmup := newEventState(config{Role: rolePublisher, Count: 1, OperationWarmup: 1})
	if err := withWarmup.setParticipants([]uint64{1, 2}); err != nil {
		t.Fatal(err)
	}
	withWarmup.accept(federate.SynchronizationPointAnnounced{
		Label: measureSync, RequiredFederates: []uint64{2, 1},
	}, counterNow())
	withWarmup.accept(federate.FederationSynchronized{Label: measureSync}, counterNow())
	if err := withWarmup.failure(); err != nil {
		t.Fatal(err)
	}

	timestampOrder := newEventState(config{Role: rolePublisher, Count: 1})
	if err := timestampOrder.setParticipants([]uint64{1, 2}); err != nil {
		t.Fatal(err)
	}
	timestampOrder.accept(federate.SynchronizationPointAnnounced{
		Label: measureSync, RequiredFederates: []uint64{1, 2},
	}, counterNow())
	if err := timestampOrder.failure(); err != nil {
		t.Fatalf("timestamp-order staging synchronization: %v", err)
	}
}

func TestSynchronizationAnnouncementMayPrecedeParticipantSnapshot(t *testing.T) {
	state := newEventState(config{Role: rolePublisher, Count: 1})
	state.accept(federate.SynchronizationPointAnnounced{
		Label: readySync, RequiredFederates: []uint64{2, 1},
	}, counterNow())
	if err := state.failure(); err != nil {
		t.Fatalf("early announcement failed before participant snapshot: %v", err)
	}
	if err := state.setParticipants([]uint64{1, 2}); err != nil {
		t.Fatalf("setParticipants: %v", err)
	}
	if !state.announced[readySync] {
		t.Fatal("early announcement was not retained")
	}
}

func TestEarlySynchronizationAnnouncementIsValidatedAfterParticipantSnapshot(t *testing.T) {
	state := newEventState(config{Role: rolePublisher, Count: 1})
	state.accept(federate.SynchronizationPointAnnounced{
		Label: readySync, RequiredFederates: []uint64{1, 3},
	}, counterNow())
	if err := state.setParticipants([]uint64{1, 2}); err == nil {
		t.Fatal("wrong early synchronization participants were accepted")
	}
}

func TestSubscriberRejectsGrantBeforeBothTimestampedCallbacks(t *testing.T) {
	state := newEventState(config{Role: roleSubscriber, Seed: "1516", Count: 1})
	if err := state.prepareAdvance(1, 0); err != nil {
		t.Fatal(err)
	}
	state.accept(federate.TimeAdvanceGrant{Time: 1}, counterNow())
	if state.failure() == nil {
		t.Fatal("grant overtook the timestamped delivery batch")
	}
}

func TestReceiveOrderSubscriberAcceptsCallbacksWithoutTimestamp(t *testing.T) {
	cfg := config{Role: roleSubscriber, Seed: "1516", Count: 1, ReceiveOrder: true}
	state := newEventState(cfg)
	state.accept(federate.DiscoverObjectInstance{
		ObjectHandle: 17, ClassName: objectClass, ObjectName: objectName,
	}, counterNow())
	items, err := preencodeWorkload(cfg.Seed, cfg.Count)
	if err != nil {
		t.Fatal(err)
	}
	state.accept(federate.ReflectAttributeValues{
		ObjectHandle: 17, ClassName: objectClass, Attributes: items[0].attributes,
	}, counterNow())
	state.accept(federate.ReceiveInteraction{
		ClassName: interactionClass, Parameters: items[0].parameters,
	}, counterNow())
	if err := state.failure(); err != nil {
		t.Fatal(err)
	}
	accounting := state.accounting()
	if accounting.Delivered != 2 || accounting.Rejected != 0 {
		t.Fatalf("accounting = %+v", accounting)
	}
}
