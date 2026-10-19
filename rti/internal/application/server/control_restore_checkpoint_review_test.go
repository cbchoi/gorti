package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/adapters/sdkstate"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Finished application outcomes are authored explicitly here, never inferred
// from Entry or a save-completion RPC. This exercises real TCP/bundle restore,
// not SDK handler execution or crash durability.
func TestControlRestoreCheckpointFreshTCPPhases(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		for _, phase := range []string{testPendingLabel, testOutcomeLabel, testCompletedLabel} {
			t.Run(factory+"/"+phase, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				defer cancel()
				root := t.TempDir()
				const fed = "control-checkpoint-review"
				create := func(conn *grpcgo.ClientConn) uint64 {
					t.Helper()
					v, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: factory})
					if err != nil {
						t.Fatal(err)
					}
					return v.FederationGeneration
				}
				source, conn, stop := integerProductionServer(t, root, 471)
				member := productionSyncJoin(ctx, t, conn, fed, "member", create(conn), true)
				if _, err := rtiv1.NewSyncServiceClient(conn).RegisterFederationSynchronizationPoint(member.ctx, &rtiv1.RegisterSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, Label: "historic", Tag: []byte{0, 255}}); err != nil {
					t.Fatal(err)
				}
				registered := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSyncRegistrationSucceeded() != nil })
				announced := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSyncAnnounced() != nil })
				identity := sha256.Sum256(registered.CallbackReceipt)
				if phase != testPendingLabel {
					productionSyncReport(t, conn, fed, member, registered, true)
				}
				if phase == testCompletedLabel {
					productionSyncReport(t, conn, fed, member, registered, false)
				}
				saves := rtiv1.NewSavepointServiceClient(conn)
				if _, err := saves.RequestFederationSave(member.ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, Label: testSavedLabel}); err != nil {
					t.Fatal(err)
				}
				init := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSaveInitiate() != nil })
				productionSyncReport(t, conn, fed, member, init, true)
				productionSyncReport(t, conn, fed, member, init, false)
				outcomes := []map[string]any{}
				if phase != testPendingLabel {
					outcomes = append(outcomes, map[string]any{"identity": hex.EncodeToString(identity[:]), "success": true, "exception": ""})
				}
				state, err := json.Marshal(map[string]any{"callback_epoch": 0, "known_objects": []any{}, "callback_recovery": "server-v2", "pending_outcomes": outcomes})
				if err != nil {
					t.Fatal(err)
				}
				runtime, err := sdkstate.Encode(sdkstate.Envelope{Language: "go", Scalar: factory, Generation: 471, Self: member.h, State: state})
				if err != nil {
					t.Fatal(err)
				}
				response := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, ClientState: runtime}
				if _, err := saves.FederateSaveBegun(member.ctx, response); err != nil {
					t.Fatal(err)
				}
				if _, err := saves.FederateSaveComplete(member.ctx, response); err != nil {
					t.Fatal(err)
				}
				stop()
				target, targetConn, _ := integerProductionServer(t, root, 472)
				current := productionSyncJoin(ctx, t, targetConn, fed, "member", create(targetConn), true)
				restore := rtiv1.NewSavepointServiceClient(targetConn)
				if _, err := restore.RequestFederationRestore(current.ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: current.h, Label: testSavedLabel}); err != nil {
					t.Fatal(err)
				}
				productionSyncReceive(t, current, func(e *rtiv1.FederateEvent) bool { return e.GetRestoreInitiate() != nil })
				if _, err := restore.FederateRestoreComplete(current.ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: current.h}); err != nil {
					t.Fatal(err)
				}
				for {
					event, err := current.receive()
					if err != nil {
						t.Fatal(err)
					}
					if event.CallbackRecovery != nil {
						t.Fatal("recovery control published before current restore terminal")
					}
					if event.GetRestoreCompleted() != nil {
						current.h = event.GetRestoreCompleted().GetFederateHandle()
						productionSyncReport(t, targetConn, fed, current, event, true)
						productionSyncReport(t, targetConn, fed, current, event, false)
						break
					}
				}
				var replayed *rtiv1.FederateEvent
				for replayed == nil {
					event, err := current.receive()
					if err != nil {
						t.Fatal(err)
					}
					if bytes.Equal(event.GetCallbackRecovery().GetInvocationIdentity(), identity[:]) {
						replayed = event
					}
				}
				if phase == testCompletedLabel {
					if !replayed.GetCallbackRecovery().GetCompleted() || !replayed.GetCallbackRecovery().GetOutcomeOnly() || replayed.Event != nil || len(replayed.CallbackReceipt) != 0 {
						t.Fatal("completed outcome was not a bodyless reconciliation marker")
					}
				} else {
					if replayed.GetSyncRegistrationSucceeded().GetLabel() != "historic" || len(replayed.CallbackReceipt) != 32 || bytes.Equal(replayed.CallbackReceipt, registered.CallbackReceipt) || replayed.GetCallbackRecovery().GetOutcomeOnly() != (phase == testOutcomeLabel) {
						t.Fatal("restored control lost body, phase or fresh capability")
					}
					if phase == testPendingLabel {
						productionSyncReport(t, targetConn, fed, current, replayed, true)
					}
					productionSyncReport(t, targetConn, fed, current, replayed, false)
					productionSyncReport(t, targetConn, fed, current, replayed, false)
				}
				_, err = rtiv1.NewSupportServiceClient(targetConn).ReportCallbackInvocation(current.ctx, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: current.h, CallbackReceipt: registered.CallbackReceipt, InvocationEntry: true, Success: true})
				if status.Code(err) != codes.FailedPrecondition {
					t.Fatal("old control capability accepted", err)
				}
				announcement := productionSyncReceive(t, current, func(e *rtiv1.FederateEvent) bool { return e.GetSyncAnnounced() != nil })
				if !bytes.Equal(announcement.GetSyncAnnounced().Tag, announced.GetSyncAnnounced().Tag) || announcement.CallbackRecovery == nil {
					t.Fatal("pending synchronization notification changed or came from duplicate domain reannouncement")
				}
				productionSyncReport(t, targetConn, fed, current, announcement, true)
				productionSyncReport(t, targetConn, fed, current, announcement, false)
				stateAfter := target.syncMgr.(*syncpkg.Manager).ExportState(fed)
				if len(stateAfter.Pending) != 0 || len(stateAfter.Obligations) != 0 {
					t.Fatal("captured synchronization controls were duplicated or not reconciled")
				}
				_ = source
			})
		}
	}
}
