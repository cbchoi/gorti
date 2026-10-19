package grpc

import (
	"context"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func queueDataEvent(order rtiv1.OrderType) *rtiv1.FederateEvent {
	return &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{
		Metadata: &rtiv1.ObjectCallbackMetadata{ReceivedOrder: &order},
	}}}
}

func TestDataQueueCountsUntilEntryWithoutPayloadDeduplication(t *testing.T) {
	r, _, _ := requiredCallbackFixture(t, 8)
	ctx := context.Background()
	var tickets [][]byte
	for _, order := range []rtiv1.OrderType{rtiv1.OrderType_ORDER_TYPE_RECEIVE, rtiv1.OrderType_ORDER_TYPE_RECEIVE, rtiv1.OrderType_ORDER_TYPE_TIMESTAMP} {
		ticket, _, err := r.PrepareRequired(ctx, "f", 1, queueDataEvent(order))
		if err != nil {
			t.Fatal(err)
		}
		tickets = append(tickets, ticket)
	}
	if counts, err := r.PendingDataCounts(ctx, "f", 1); err != nil || counts != (core.DataQueueCounts{}) {
		t.Fatal("tentative data counted", counts, err)
	}
	r.mu.Lock()
	for _, ticket := range tickets {
		r.entries[string(ticket)].admitted = true
	}
	r.mu.Unlock()
	if counts, err := r.PendingDataCounts(ctx, "f", 1); err != nil || counts != (core.DataQueueCounts{RO: 2, TSO: 1}) {
		t.Fatal(counts, err)
	}
	if counts, err := r.PendingDataCounts(ctx, "f", 2); err != nil || counts != (core.DataQueueCounts{}) {
		t.Fatal("foreign receiver counted", counts, err)
	}
	for _, ticket := range tickets[:2] {
		if err := r.ReportEntry(ctx, "f", 1, ticket); err != nil {
			t.Fatal(err)
		}
		if err := r.ReportEntry(ctx, "f", 1, ticket); err != nil {
			t.Fatal(err)
		}
	}
	if counts, err := r.PendingDataCounts(ctx, "f", 1); err != nil || counts != (core.DataQueueCounts{TSO: 1}) {
		t.Fatal("entry count", counts, err)
	}
	if err := r.Report(ctx, "f", 1, tickets[0], true, ""); err != nil {
		t.Fatal(err)
	}
	if counts, err := r.PendingDataCounts(ctx, "f", 1); err != nil || counts != (core.DataQueueCounts{TSO: 1}) {
		t.Fatal("completion double decrement", counts, err)
	}
	r.ClearFederation("f")
	if counts, err := r.PendingDataCounts(ctx, "f", 1); err != nil || counts != (core.DataQueueCounts{}) {
		t.Fatal("teardown retained data", counts, err)
	}
}

func TestDataQueueCountsRejectUnknownOrderAndIgnoreControls(t *testing.T) {
	r, _, _ := requiredCallbackFixture(t, 8)
	ctx := context.Background()
	control := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SyncSynchronized{SyncSynchronized: &rtiv1.FederationSynchronized{Label: "p"}}}
	for _, event := range []*rtiv1.FederateEvent{control, queueDataEvent(rtiv1.OrderType_ORDER_TYPE_UNSPECIFIED)} {
		ticket, _, err := r.PrepareRequired(ctx, "f", 1, event)
		if err != nil {
			t.Fatal(err)
		}
		r.mu.Lock()
		r.entries[string(ticket)].admitted = true
		r.mu.Unlock()
	}
	if _, err := r.PendingDataCounts(ctx, "f", 1); err == nil {
		t.Fatal("unknown order became a synthetic zero")
	}
}
