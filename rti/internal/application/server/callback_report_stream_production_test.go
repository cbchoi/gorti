package server

import (
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery/callbackprofile"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func TestCallbackReportStreamProductionAuthenticationAndLifecycle(t *testing.T) {
	if !callbackprofile.Async {
		t.Skip("optional callback stream build")
	}
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		t.Run(factory, func(t *testing.T) {
			f := newConveyedProductionFixture(t, factory)
			stream, cancel := f.stream(t)
			defer cancel()
			f.send(t)
			event := conveyedProductionReceive(t, stream)
			entry := &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle, CallbackReceipt: event.CallbackReceipt, InvocationEntry: true, Success: true}
			foreign, err := rtiv1.NewCallbackReportServiceClient(f.conn).Exchange(f.foreignCtx)
			if err != nil {
				t.Fatal(err)
			}
			if err := foreign.Send(&rtiv1.CallbackReportBatch{FirstSequence: 1, Reports: []*rtiv1.ReportCallbackInvocationRequest{entry}}); err != nil {
				t.Fatal(err)
			}
			ack, err := foreign.Recv()
			if err != nil || ack.AcknowledgedSequence != 0 || ack.FailedSequence != 1 || ack.ErrorCode == 0 {
				t.Fatalf("foreign entry accepted: %v/%v", ack, err)
			}
			requireProductionQueueLengths(t, f, "foreign stream has no effect", 1, 0)
			legitimate, err := rtiv1.NewCallbackReportServiceClient(f.conn).Exchange(f.receiverCtx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := legitimate.CloseSend(); err != nil {
					t.Error(err)
				}
			}()
			outcome := &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle, CallbackReceipt: event.CallbackReceipt, Success: true}
			if err := legitimate.Send(&rtiv1.CallbackReportBatch{FirstSequence: 1, Reports: []*rtiv1.ReportCallbackInvocationRequest{entry, outcome}}); err != nil {
				t.Fatal(err)
			}
			ack, err = legitimate.Recv()
			if err != nil || ack.AcknowledgedSequence != 2 || ack.FailedSequence != 0 {
				t.Fatalf("real entry/outcome: %v/%v", ack, err)
			}
			requireProductionQueueLengths(t, f, "entry and completion applied", 0, 0)
			if err := legitimate.Send(&rtiv1.CallbackReportBatch{FirstSequence: 3, Reports: []*rtiv1.ReportCallbackInvocationRequest{entry, outcome}}); err != nil {
				t.Fatal(err)
			}
			ack, err = legitimate.Recv()
			if err != nil || ack.AcknowledgedSequence != 4 || ack.FailedSequence != 0 {
				t.Fatalf("idempotent retry: %v/%v", ack, err)
			}
			requireProductionQueueLengths(t, f, "retry does not double decrement", 0, 0)
		})
	}
}

func TestCallbackReportStreamProductionDataBatchAndReportingSwitch(t *testing.T) {
	if !callbackprofile.Async {
		t.Skip("optional callback stream build")
	}
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		t.Run(factory, func(t *testing.T) {
			f := newConveyedProductionFixture(t, factory)
			if _, err := rtiv1.NewDeclarationServiceClient(f.conn).SubscribeInteractionClass(f.receiverCtx,
				&rtiv1.SubInterRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.receiver.FederateHandle, InteractionClassHandle: f.class}); err != nil {
				t.Fatal(err)
			}
			stream, cancel := f.stream(t)
			defer cancel()
			reports, err := rtiv1.NewCallbackReportServiceClient(f.conn).Exchange(f.receiverCtx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := reports.CloseSend(); err != nil {
					t.Error(err)
				}
			}()
			manager := f.runtime.momMgr.(*mom.Manager)
			fed, handle := core.FederationName(f.fed), core.FederateHandle(f.receiver.FederateHandle)
			state := func() mom.FederateState {
				for _, member := range manager.ExportState(fed).Federates {
					if member.Handle == handle {
						return member
					}
				}
				t.Fatal("receiver missing")
				return mom.FederateState{}
			}
			var sequence uint64 = 1
			for _, reporting := range []bool{false, true, false} {
				manager.SetServiceReporting(fed, handle, reporting)
				before := state()
				for range 8 {
					if _, err := rtiv1.NewObjectServiceClient(f.conn).SendInteraction(f.producerCtx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: f.producer.FederateHandle, InteractionClassHandle: f.class, Parameters: map[uint64][]byte{f.parameter: {0, 0, 0, 1}}}); err != nil {
						t.Fatal(err)
					}
				}
				events := queueProductionReceiveMany(t, stream, 8)
				batch := &rtiv1.CallbackReportBatch{FirstSequence: sequence}
				for _, event := range events {
					if event.GetReceive().Metadata.ReceivedRegions != nil {
						t.Fatal("expected ordinary data without conveyed regions")
					}
					for _, entry := range []bool{true, false} {
						batch.Reports = append(batch.Reports, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: f.fed, FederateHandle: uint64(handle), CallbackReceipt: event.CallbackReceipt, InvocationEntry: entry, Success: true})
					}
				}
				foreign, err := rtiv1.NewCallbackReportServiceClient(f.conn).Exchange(f.foreignCtx)
				if err != nil {
					t.Fatal(err)
				}
				if err := foreign.Send(&rtiv1.CallbackReportBatch{FirstSequence: 1, Reports: batch.Reports}); err != nil {
					t.Fatal(err)
				}
				denied, err := foreign.Recv()
				_ = foreign.CloseSend()
				if err != nil || denied.FailedSequence != 1 || denied.AcknowledgedSequence != 0 {
					t.Fatalf("foreign data reports accepted: %v/%v", denied, err)
				}
				if err := reports.Send(batch); err != nil {
					t.Fatal(err)
				}
				ack, err := reports.Recv()
				sequence += uint64(len(batch.Reports))
				if err != nil || ack.FailedSequence != 0 || ack.AcknowledgedSequence != sequence-1 {
					t.Fatalf("%v/%v", ack, err)
				}
				after := state()
				if after.RuntimeMetrics.PendingCallbacks != before.RuntimeMetrics.PendingCallbacks || after.RuntimeMetrics.Counters["HLAinteractionsReceived"] != before.RuntimeMetrics.Counters["HLAinteractionsReceived"]+8 {
					t.Fatal("batch accounting differs from callback execution")
				}
				wantSerial := before.ServiceReports.NextSerial
				if reporting {
					wantSerial += 8
				}
				if after.ServiceReports.NextSerial != wantSerial {
					t.Fatalf("MOM detailed reports lost: got %d want %d", after.ServiceReports.NextSerial, wantSerial)
				}
			}
		})
	}
}
