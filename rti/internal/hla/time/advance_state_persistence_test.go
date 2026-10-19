package time

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const testRegulatingMode = "regulating"

func TestAdvanceStatePersistence_UnengagedRoundTripRetainsLogicalTime(t *testing.T) {
	for _, service := range advanceTargetServices {
		for _, enable := range []string{testRegulatingMode, "constrained"} {
			t.Run(service.name+"/"+enable, func(t *testing.T) {
				ctx := context.Background()
				const fed core.FederationName = "advance-persistence"
				const h core.FederateHandle = 1
				source, sourceOut := newIEEE2010DeliveryManager(t)
				restored, restoredOut := newIEEE2010DeliveryManager(t)
				t.Cleanup(func() { managerExt.Delete(source); managerExt.Delete(restored) })
				if err := service.call(source, ctx, fed, h, 7); err != nil {
					t.Fatalf("unengaged %s: %v", service.name, err)
				}
				assertAdvancePersistenceGrant(t, sourceOut.events, 7)
				before := source.Snapshot(fed)
				if len(before.Federates) != 1 {
					t.Fatalf("unengaged caller missing from snapshot: %+v", before)
				}
				entry := before.Federates[0]
				if entry.Handle != h || entry.CurrentTime != 7 || entry.Regulating || entry.Constrained || entry.HasPendingRequest {
					t.Fatalf("unengaged snapshot: %+v", entry)
				}
				state, err := source.ExportState(fed, clonePersistentTestEvent)
				if err != nil {
					t.Fatal(err)
				}
				if state == nil || len(state.Federates) != 1 || !state.Federates[0].HasAdvanceState || state.Federates[0].CurrentTime != 7 || state.Federates[0].Regulating || state.Federates[0].Constrained || state.Federates[0].PendingRequest {
					t.Fatalf("unengaged logical time missing from export: %+v", state)
				}
				if err := restored.ImportState(fed, state, clonePersistentTestEvent); err != nil {
					t.Fatal(err)
				}
				if got := restored.Snapshot(fed); !reflect.DeepEqual(got, before) {
					t.Fatalf("restored snapshot = %+v, want %+v", got, before)
				}
				if len(restoredOut.events) != 0 {
					t.Fatalf("restore emitted callbacks: %v", restoredOut.events)
				}
				if enable == testRegulatingMode {
					err = restored.EnableRegulation(ctx, fed, h, 1)
				} else {
					err = restored.EnableConstrained(ctx, fed, h)
				}
				if err != nil {
					t.Fatal(err)
				}
				got := restored.Snapshot(fed)
				if len(got.Federates) != 1 {
					t.Fatalf("mode enable lost federate: %+v", got)
				}
				entry = got.Federates[0]
				if entry.CurrentTime != 7 || entry.HasPendingRequest || entry.Regulating != (enable == testRegulatingMode) || entry.Constrained != (enable == "constrained") {
					t.Fatalf("mode enable reset restored logical time or flags: %+v", entry)
				}
				if err := service.call(restored, ctx, fed, h, 8); err != nil {
					t.Fatalf("advance after restore and mode enable: %v", err)
				}
				assertAdvancePersistenceGrant(t, restoredOut.events, 8)
			})
		}
	}
}

func TestAdvanceStatePersistence_HeldSameTimeTSOSurvivesRestore(t *testing.T) {
	ctx := context.Background()
	const fed core.FederationName = "held-tso-persistence"
	const h core.FederateHandle = 1
	source, sourceOut := newIEEE2010DeliveryManager(t)
	restored, restoredOut := newIEEE2010DeliveryManager(t)
	t.Cleanup(func() { managerExt.Delete(source); managerExt.Delete(restored) })
	if err := source.EnableConstrained(ctx, fed, h); err != nil {
		t.Fatal(err)
	}
	// An Available grant permits a later equal-time message; time-granted
	// state still holds that TSO until the next advance (2010 8.1.6/8.1.7).
	if err := source.TimeAdvanceRequestAvailable(ctx, fed, h, 5); err != nil {
		t.Fatal(err)
	}
	assertAdvancePersistenceGrant(t, sourceOut.events, 5)
	event := &persistentTestEvent{sequence: 23, data: []byte{0, 255}}
	delivery := core.TSOBufferedDelivery{Recipient: h, Timestamp: 5, Event: event, Sender: 2, RetractionHandle: 77}
	reservation := source.ReserveTSO(fed, []core.TSOBufferedDelivery{delivery})
	defer reservation.Release()
	if len(reservation.Immediate()) != 0 || len(reservation.Buffered()) != 1 {
		t.Fatalf("time-granted same-time TSO must be held: immediate=%v buffered=%v", reservation.Immediate(), reservation.Buffered())
	}
	reservation.Commit(ctx)
	if source.ShouldDeliverNow(fed, h, 5) {
		t.Fatal("same-time TSO gate opened without another advance")
	}
	state, err := source.ExportState(fed, clonePersistentTestEvent)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil || len(state.Federates) != 1 || !reflect.DeepEqual(state.Federates[0].Queue, []core.TSOBufferedDelivery{delivery}) {
		t.Fatalf("export lost held same-time TSO: %+v", state)
	}
	if err := restored.ImportState(fed, state, clonePersistentTestEvent); err != nil {
		t.Fatal(err)
	}
	again, err := restored.ExportState(fed, clonePersistentTestEvent)
	if err != nil || !reflect.DeepEqual(again, state) {
		t.Fatalf("held TSO round trip = %+v, %v; want %+v", again, err, state)
	}
	if len(sourceOut.events) != 1 || len(restoredOut.events) != 0 {
		t.Fatalf("snapshot/import released TSO: source=%v restored=%v", sourceOut.events, restoredOut.events)
	}
	if err := restored.EnableAsynchronousDelivery(ctx, fed, h); err != nil {
		t.Fatal(err)
	}
	if len(restoredOut.events) != 0 || restored.ShouldDeliverNow(fed, h, 5) {
		t.Fatalf("async prematurely released restored same-time TSO: %v", restoredOut.events)
	}
	probe := restored.ReserveTSO(fed, []core.TSOBufferedDelivery{delivery})
	defer probe.Release()
	if len(probe.Immediate()) != 0 || len(probe.Buffered()) != 1 {
		t.Fatalf("restored async reservation bypassed TSO hold: immediate=%v buffered=%v", probe.Immediate(), probe.Buffered())
	}
	probe.Release()
	state.Federates[0].AsynchronousDelivery = true
	again, err = restored.ExportState(fed, clonePersistentTestEvent)
	if err != nil || !reflect.DeepEqual(again, state) {
		t.Fatalf("async changed held TSO state: got=%+v err=%v want=%+v", again, err, state)
	}
	if err := restored.NextMessageRequest(ctx, fed, h, 9); err != nil {
		t.Fatal(err)
	}
	assertAdvancePersistenceMessageThenGrant(t, restoredOut.events, event, 5)
}

func TestAdvanceStatePersistence_SameTimeNMRGrantAfterRestore(t *testing.T) {
	ctx := context.Background()
	const fed core.FederationName = "same-time-nmr-restore"
	m, out := newIEEE2010DeliveryManager(t)
	t.Cleanup(func() { managerExt.Delete(m) })
	event := &persistentTestEvent{sequence: 42, data: []byte("same-time")}
	// A saved time-granted image isolates NMR restoration from live TSO
	// admission, so a gate regression cannot hide the grant-target regression.
	state := &ManagerState{StallTimeout: m.opts.StallTimeout, Federates: []FederateState{{
		Handle: 1, HasRegulationState: true, Constrained: true,
		HasAdvanceState: true, CurrentTime: 5,
		Queue: []core.TSOBufferedDelivery{{Recipient: 1, Timestamp: 5, Event: event, Sender: 2, RetractionHandle: 91}},
	}}}
	if err := m.ImportState(fed, state, clonePersistentTestEvent); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 0 {
		t.Fatalf("import emitted callbacks: %v", out.events)
	}
	if err := m.NextMessageRequest(ctx, fed, 1, 9); err != nil {
		t.Fatal(err)
	}
	assertAdvancePersistenceMessageThenGrant(t, out.events, event, 5)
	snapshot := m.Snapshot(fed)
	if len(snapshot.Federates) != 1 || snapshot.Federates[0].CurrentTime != 5 || snapshot.Federates[0].HasPendingRequest {
		t.Fatalf("same-time NMR did not finish at restored current time: %+v", snapshot)
	}
	got, err := m.ExportState(fed, clonePersistentTestEvent)
	if err != nil || got == nil || len(got.Federates) != 1 || len(got.Federates[0].Queue) != 0 {
		t.Fatalf("delivered restored TSO remains queued: %+v, %v", got, err)
	}
}

func assertAdvancePersistenceGrant(t *testing.T, events []core.OutboundEvent, want core.LogicalTime) {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("callbacks = %v, want one TAG(%g)", events, want)
	}
	grant, ok := events[0].(*TimeAdvanceGrant)
	if !ok || grant.Time != want {
		t.Fatalf("callback = %+v, want TAG(%g)", events[0], want)
	}
}

func assertAdvancePersistenceMessageThenGrant(t *testing.T, events []core.OutboundEvent, want *persistentTestEvent, timestamp core.LogicalTime) {
	t.Helper()
	if len(events) != 2 || !reflect.DeepEqual(events[0], want) {
		t.Fatalf("restored message must precede exactly one grant: %+v", events)
	}
	if events[0] == want {
		t.Fatal("restored callback aliases the saved event")
	}
	assertAdvancePersistenceGrant(t, events[1:], timestamp)
}
