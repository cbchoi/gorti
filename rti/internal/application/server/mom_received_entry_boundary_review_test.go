package server

import (
	"encoding/binary"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func TestMOMReceivedBoundaryReviewCountsOnlyInvokedInteraction(t *testing.T) {
	f := newConveyedProductionFixture(t, float64TimeName)
	manager, ok := f.runtime.momMgr.(*mom.Manager)
	if !ok {
		t.Fatal("production MOM manager state source unavailable")
	}
	times := rtiv1.NewTimeServiceClient(f.conn)
	if _, err := times.EnableTimeConstrained(f.receiverCtx, &rtiv1.EnableConstrainedRequest{
		WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle,
	}); err != nil {
		t.Fatal(err)
	}
	assertReceived := func(phase string, want uint64) {
		t.Helper()
		response, err := rtiv1.NewMomServiceClient(f.conn).QueryFederateAttributes(f.receiverCtx,
			&rtiv1.QueryFederateAttributesRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle})
		if err != nil {
			t.Fatal(err)
		}
		if encoded := response.AttributeValues["HLAinteractionsReceived"]; len(encoded) == 4 {
			t.Logf("%s: standard received attribute=%d legacy fanout=%d", phase, binary.BigEndian.Uint32(encoded), response.InteractionsReceived)
		} else {
			t.Logf("%s: standard received attribute unavailable=%q legacy fanout=%d", phase, response.UnavailableAttributes["HLAinteractionsReceived"], response.InteractionsReceived)
		}
		state := manager.ExportState(core.FederationName(f.fed))
		if state.Traffic == nil {
			t.Fatal("production class-traffic state missing")
		}
		var count uint64
		for _, recipient := range state.Traffic.Federates {
			if uint64(recipient.Handle) != f.receiver.FederateHandle {
				continue
			}
			for _, traffic := range recipient.Counts {
				if traffic.Kind == mom.InteractionsReceived && traffic.Class == f.class {
					count += uint64(traffic.Count)
				}
			}
		}
		if count != want {
			t.Errorf("%s: TrafficAccumulator received class count=%d want=%d", phase, count, want)
		}
	}
	assertReceived("before send", 0)
	stamp := float64(5)
	if _, err := rtiv1.NewDDMServiceClient(f.conn).SendInteractionWithRegions(f.producerCtx,
		&rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: f.fed,
			FederateHandle: f.producer.FederateHandle, InteractionClassHandle: f.class,
			Parameters: map[uint64][]byte{f.parameter: {0, 0, 0, 1}}, RegionHandles: []uint64{f.publisher}, LogicalTime: &stamp}); err != nil {
		t.Fatal(err)
	}
	requireProductionQueueLengths(t, f, "accepted but waiting", 0, 1)
	assertReceived("accepted but time-waiting: no callback invoked", 0)
	stream, stop := f.stream(t)
	defer stop()
	if _, err := times.TimeAdvanceRequest(f.receiverCtx, &rtiv1.TARRequest{
		WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle, LogicalTime: 5,
	}); err != nil {
		t.Fatal(err)
	}
	event := conveyedProductionReceive(t, stream)
	if len(event.CallbackReceipt) != 32 {
		t.Fatal("data callback lacks mandatory ENTRY receipt")
	}
	assertReceived("wire delivered but callback not entered", 0)
	f.report(t, event, true)
	assertReceived("authenticated callback ENTRY", 1)
	f.report(t, event, true)
	assertReceived("duplicate ENTRY", 1)
	f.report(t, event, false)
	assertReceived("completion is not another received event", 1)
}
