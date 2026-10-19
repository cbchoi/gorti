package server

import (
	"context"
	"testing"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	grpcgo "google.golang.org/grpc"
)

// Manager restart fixtures inspect raw queues; this collector is their callback
// body. The separate TCP campaign proves the normal stream/SDK receipt boundary.
func invokeQueuedSynchronizationFixture(ctx context.Context, t *testing.T, conn *grpcgo.ClientConn, fed core.FederationName, h core.FederateHandle, queue <-chan []core.OutboundEvent) {
	t.Helper()
	count := 0
	for _, event := range drainScheduledEvents(queue) {
		wire, err := grpcadapter.EncodeOutboundEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		if wire.GetSyncAnnounced() == nil && wire.GetSyncSynchronized() == nil && wire.GetSyncRegistrationSucceeded() == nil && wire.GetSyncRegistrationFailed() == nil {
			continue
		}
		if len(wire.CallbackReceipt) != 32 {
			t.Fatal("restart fixture synchronization callback lacks mandatory receipt")
		}
		for _, entry := range []bool{true, false} {
			_, err := rtiv1.NewSupportServiceClient(conn).ReportCallbackInvocation(ctx, &rtiv1.ReportCallbackInvocationRequest{
				WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(h), CallbackReceipt: wire.CallbackReceipt, Success: true, InvocationEntry: entry,
			})
			if err != nil {
				t.Fatal("restart fixture callback invocation", entry, err)
			}
		}
		count++
	}
	if count == 0 {
		t.Fatal("restart fixture did not invoke synchronization callback")
	}
}
