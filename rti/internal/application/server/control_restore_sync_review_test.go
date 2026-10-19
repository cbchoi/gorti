package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func controlRestoreReviewCaptured(t *testing.T, recipient core.FederateHandle, event *rtiv1.FederateEvent, outcomeOnly bool) syncpkg.RestoredCallback {
	t.Helper()
	row := syncpkg.RestoredCallback{Recipient: recipient, Sequence: event.GetSeq(), OutcomeOnly: outcomeOnly}
	handles := func(values []uint64) []core.FederateHandle {
		result := make([]core.FederateHandle, len(values))
		for i, value := range values {
			result[i] = core.FederateHandle(value)
		}
		return result
	}
	switch {
	case event.GetSyncAnnounced() != nil:
		v := event.GetSyncAnnounced()
		row.Kind, row.Label, row.Tag, row.Required = core.SyncCallbackAnnounced, v.Label, v.Tag, handles(v.RequiredFederates)
	case event.GetSyncSynchronized() != nil:
		v := event.GetSyncSynchronized()
		row.Kind, row.Label, row.Failed = core.SyncCallbackSynchronized, v.Label, handles(v.FailedToSync)
	case event.GetSyncRegistrationSucceeded() != nil:
		row.Kind, row.Label = core.SyncCallbackRegistrationSucceeded, event.GetSyncRegistrationSucceeded().Label
	case event.GetSyncRegistrationFailed() != nil:
		v := event.GetSyncRegistrationFailed()
		row.Kind, row.Label = core.SyncCallbackRegistrationFailed, v.Label
		switch v.Reason {
		case rtiv1.SyncPointFailureReason_SYNC_POINT_FAILURE_REASON_LABEL_NOT_UNIQUE:
			row.Reason = syncpkg.RegistrationFailureLabelNotUnique
		case rtiv1.SyncPointFailureReason_SYNC_POINT_FAILURE_REASON_SET_MEMBER_NOT_JOINED:
			row.Reason = syncpkg.RegistrationFailureSetMemberNotJoined
		default:
			t.Fatal("unknown immutable synchronization failure reason")
		}
	default:
		t.Fatal("not a synchronization recovery callback")
	}
	return row
}

// This proves actual TCP source entry plus fresh-root domain reconciliation.
// It does not replace the parent's v2 bundle/SDK outcome recovery campaign.
func TestControlRestoreSynchronizationTCPReconciliation(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		t.Run(factory, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			const fed = "control-sync-reconciliation"
			create := func(conn *grpcgo.ClientConn) uint64 {
				t.Helper()
				created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: factory})
				if err != nil {
					t.Fatal(err)
				}
				return created.FederationGeneration
			}
			source, sourceConn, stop := integerProductionServer(t, t.TempDir(), 461)
			member := productionSyncJoin(ctx, t, sourceConn, fed, "member", create(sourceConn), true)
			client := rtiv1.NewSyncServiceClient(sourceConn)
			request := &rtiv1.RegisterSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, Label: "immutable", Tag: []byte{0, 255}}
			if _, err := client.RegisterFederationSynchronizationPoint(member.ctx, request); err != nil {
				t.Fatal(err)
			}
			registered := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSyncRegistrationSucceeded() != nil })
			announced := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSyncAnnounced() != nil })
			if _, err := client.RegisterFederationSynchronizationPoint(member.ctx, request); err != nil {
				t.Fatal(err)
			}
			rejected := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSyncRegistrationFailed() != nil })
			productionSyncReport(t, sourceConn, fed, member, announced, true)
			unsuccessful := false
			if _, err := client.SynchronizationPointAchieved(member.ctx, &rtiv1.AchieveSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, Label: "immutable", Successfully: &unsuccessful}); err != nil {
				t.Fatal(err)
			}
			terminal := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSyncSynchronized() != nil })
			manager := source.syncMgr.(*syncpkg.Manager)
			state := manager.ExportState(fed)
			if len(state.Obligations) != 4 || len(state.Pending) != 0 {
				t.Fatal("source did not capture all four admitted callback categories")
			}
			var captured []syncpkg.RestoredCallback
			for _, event := range []*rtiv1.FederateEvent{registered, announced, rejected, terminal} {
				captured = append(captured, controlRestoreReviewCaptured(t, core.FederateHandle(member.h), event, event == announced))
			}
			if err := syncpkg.ValidateRestoredCallbacks(state, captured); err != nil {
				t.Fatal(err)
			}
			image, err := (savebundle.StrictSynchronization{Manager: manager}).Marshal(fed)
			if err != nil {
				t.Fatal(err)
			}
			stop()
			target, targetConn, _ := integerProductionServer(t, t.TempDir(), 462)
			joined := momCompletionReviewJoin(ctx, t, targetConn, fed, "member", "", create(targetConn))
			h := core.FederateHandle(joined.FederateHandle)
			rewrite := func(old core.FederateHandle) (core.FederateHandle, error) {
				if uint64(old) != member.h {
					return 0, fmt.Errorf("unknown source participant %d", old)
				}
				return h, nil
			}
			image, err = (savebundle.StrictSynchronization{}).RewriteFederates(image, rewrite)
			if err != nil {
				t.Fatal(err)
			}
			for i := range captured {
				captured[i].Recipient = h
				for j := range captured[i].Required {
					captured[i].Required[j], err = rewrite(captured[i].Required[j])
					if err != nil {
						t.Fatal(err)
					}
				}
				for j := range captured[i].Failed {
					captured[i].Failed[j], err = rewrite(captured[i].Failed[j])
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			targetManager := target.syncMgr.(*syncpkg.Manager)
			commit, err := (savebundle.StrictSynchronization{Manager: targetManager}).PrepareRestore(fed, image)
			if err != nil {
				t.Fatal(err)
			}
			commit()
			if err := targetManager.ReconcileRestoredCallbacks(fed, captured); err != nil {
				t.Fatal(err)
			}
			queue, closeQueue, err := target.outbox.Subscribe(ctx, fed, h)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := closeQueue(); err != nil {
					t.Error(err)
				}
			}()
			for repeat := 0; repeat < 2; repeat++ {
				if err := target.synchronizationLifecycle.Restored(ctx, fed); err != nil {
					t.Fatal(err)
				}
			}
			if len(queue) != 0 || len(targetManager.ExportState(fed).Pending) != 0 || len(targetManager.ExportState(fed).Obligations) != 4 {
				t.Fatal("lifecycle retry duplicated captured transport authority or discharged obligations")
			}
			signed := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joined.SessionToken))
			_, err = rtiv1.NewSupportServiceClient(targetConn).ReportCallbackInvocation(signed, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: uint64(h), CallbackReceipt: announced.CallbackReceipt, Success: true})
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatal("old source control ticket became authorization", err)
			}
			if err := targetManager.ObserveSyncCallback(ctx, fed, h, core.SyncCallbackAnnounced, "immutable", announced.Seq); err != nil {
				t.Fatal("restored entered obligation required a second entry", err)
			}
			if len(targetManager.ExportState(fed).Obligations) != 3 {
				t.Fatal("outcome reconciliation did not discharge exactly one obligation")
			}
		})
	}
}
