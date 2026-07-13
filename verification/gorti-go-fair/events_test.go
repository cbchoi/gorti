package main

import (
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

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
