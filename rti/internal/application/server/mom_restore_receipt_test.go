package server

import (
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestMOMRestoreInitiationReceiptSurvivesProductionCommit(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		name := "same-handle"
		if replacement {
			name = "replacement-handle"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			srv, conn, _ := restartCheckpointServer(t, t.TempDir(), 61)
			defer srv.sessions.Close()
			const fed core.FederationName = "restore-receipt"
			defer func() {
				if err := srv.momMgr.FederationDestroyed(context.Background(), fed); err != nil {
					t.Error(err)
				}
			}()
			federations := rtiv1.NewFederationServiceClient(conn)
			created, err := federations.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: string(fed), FomModules: []*rtiv1.FOMModule{{Path: "model.xml", Xml: []byte(`<objectModel/>`)}}})
			if err != nil {
				t.Fatal(err)
			}
			join := func() (*rtiv1.JoinFederationResponse, context.Context, rtiv1.StreamService_EventBatchesClient, context.CancelFunc) {
				t.Helper()
				joined, err := federations.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: string(fed), FederateName: "member", ExpectedFederationGeneration: &created.FederationGeneration})
				if err != nil {
					t.Fatal(err)
				}
				signed := metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(joined.SessionToken))
				streamCtx, stop := context.WithCancel(signed)
				stream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(streamCtx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: joined.FederateHandle, ExpectedFederationGeneration: &created.FederationGeneration})
				if err != nil {
					t.Fatal(err)
				}
				if ready, err := stream.Recv(); err != nil || !ready.GetReady() {
					t.Fatal("callback stream not ready", ready, err)
				}
				return joined, signed, stream, stop
			}
			joined, signed, stream, stop := join()
			defer func() { stop() }()
			savedHandle := joined.FederateHandle
			support := rtiv1.NewSupportServiceClient(conn)
			entered := map[string]bool{}
			entry := func(h uint64, event *rtiv1.FederateEvent) error {
				if entered[string(event.CallbackReceipt)] || event.GetCallbackRecovery().GetOutcomeOnly() {
					return nil
				}
				_, err := support.ReportCallbackInvocation(signed, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: h, CallbackReceipt: event.CallbackReceipt, InvocationEntry: true, Success: true})
				if err == nil {
					entered[string(event.CallbackReceipt)] = true
				}
				return err
			}
			report := func(h uint64, event *rtiv1.FederateEvent) error {
				t.Helper()
				if event.GetCallbackRecovery().GetCompleted() {
					return nil
				}
				if len(event.CallbackReceipt) != 32 {
					t.Fatalf("missing authenticated receipt: %v", event)
				}
				if err := entry(h, event); err != nil {
					return err
				}
				_, err := support.ReportCallbackInvocation(signed, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: h, CallbackReceipt: event.CallbackReceipt, Success: true})
				return err
			}
			var queued []*rtiv1.FederateEvent
			receive := func() *rtiv1.FederateEvent {
				t.Helper()
				for len(queued) == 0 {
					batch, err := stream.Recv()
					if err != nil {
						t.Fatal(err)
					}
					queued = append(queued, batch.Events...)
				}
				event := queued[0]
				queued = queued[1:]
				return event
			}
			saves := rtiv1.NewSavepointServiceClient(conn)
			if _, err := saves.RequestFederationSave(signed, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: joined.FederateHandle, Label: testSavedLabel}); err != nil {
				t.Fatal(err)
			}
			initSave := receive()
			if initSave.GetSaveInitiate() == nil {
				t.Fatal("missing save initiation", initSave)
			}
			if err := report(joined.FederateHandle, initSave); err != nil {
				t.Fatal(err)
			}
			saveResponse := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: joined.FederateHandle}
			if _, err := saves.FederateSaveBegun(signed, saveResponse); err != nil {
				t.Fatal(err)
			}
			if _, err := saves.FederateSaveComplete(signed, saveResponse); err != nil {
				t.Fatal(err)
			}
			if err := report(joined.FederateHandle, receive()); err != nil {
				t.Fatal(err)
			}
			if replacement {
				if _, err := federations.ResignFederation(signed, &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: joined.FederateHandle, Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION}); err != nil {
					t.Fatal(err)
				}
				stop()
				joined, signed, stream, stop = join()
				if joined.FederateHandle == savedHandle {
					t.Fatal("replacement did not change the handle")
				}
			}
			if _, err := saves.RequestFederationRestore(signed, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: joined.FederateHandle, Label: testSavedLabel}); err != nil {
				t.Fatal(err)
			}
			var initiated *rtiv1.FederateEvent
			for initiated == nil {
				event := receive()
				if event.GetRestoreInitiate() != nil {
					initiated = event
				} else if err := report(joined.FederateHandle, event); err != nil {
					t.Fatal(err)
				}
			}
			if initiated.GetRestoreInitiate().GetFederateHandle() != savedHandle {
				t.Fatal("incorrect announced restore identity")
			}
			// The last participant completes restore inside initiateFederateRestore,
			// before the SDK's finally block reports that callback's actual outcome.
			if err := entry(joined.FederateHandle, initiated); err != nil {
				t.Fatal(err)
			}
			if _, err := saves.FederateRestoreComplete(signed, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: joined.FederateHandle}); err != nil {
				t.Fatal(err)
			}
			if err := report(savedHandle, initiated); err != nil {
				t.Fatalf("in-flight restore initiation receipt rejected after commit: %v", err)
			}
			if err := report(savedHandle, initiated); err != nil {
				t.Fatalf("idempotent report rejected: %v", err)
			}
			restored := receive()
			if restored.GetRestoreCompleted().GetFederateHandle() != savedHandle {
				t.Fatal("terminal callback did not preserve the live stream", restored)
			}
			if err := report(savedHandle, restored); err != nil {
				t.Fatal(err)
			}
			if err := report(savedHandle, initSave); status.Code(err) != codes.FailedPrecondition {
				t.Fatal("an arbitrary pre-restore callback receipt survived", err)
			}
			stranger, err := federations.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: string(fed), FederateName: "stranger", ExpectedFederationGeneration: &created.FederationGeneration})
			if err != nil {
				t.Fatal(err)
			}
			strangerCtx := metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(stranger.SessionToken))
			for _, h := range []uint64{stranger.FederateHandle, savedHandle} {
				_, err := support.ReportCallbackInvocation(strangerCtx, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: h, CallbackReceipt: initiated.CallbackReceipt, Success: true})
				want := codes.PermissionDenied
				if h == stranger.FederateHandle {
					want = codes.FailedPrecondition
				}
				if status.Code(err) != want {
					t.Fatalf("other member report at handle %d: got %v, want %s", h, err, want)
				}
			}
			state := srv.momMgr.(*mom.Manager).ExportState(fed)
			for _, member := range state.Federates {
				if member.Handle == core.FederateHandle(savedHandle) && member.RuntimeMetrics != nil {
					if member.RuntimeMetrics.PendingCallbacks != 0 || len(member.RuntimeMetrics.Failures) != 0 {
						t.Fatalf("restore corrupted callback accounting: %+v", member.RuntimeMetrics)
					}
					return
				}
			}
			t.Fatal("restored MOM runtime metrics absent")
		})
	}
}
