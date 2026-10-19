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
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Raw authenticated clients author known finished outcomes explicitly. This
// proves server replay and receipt reconciliation, not SDK body execution.
func TestOwnershipCheckpointFreshTCPBothFamilies(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		for _, phase := range []string{testPendingLabel, testOutcomeLabel, "outcome-owned-completed", testCompletedLabel} {
			t.Run(factory+"/"+phase, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				defer cancel()
				dir := t.TempDir()
				const fed = "ownership-checkpoint-tcp"
				create := func(conn *grpcgo.ClientConn) uint64 {
					t.Helper()
					created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: factory, FomModules: []*rtiv1.FOMModule{{Path: "ownership.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>value</name></attribute></objectClass></objectClass></objects></objectModel>`)}}})
					if err != nil {
						t.Fatal(err)
					}
					return created.FederationGeneration
				}
				source, conn, stop := integerProductionServer(t, dir, 481)
				generation := create(conn)
				owner := productionSyncJoin(ctx, t, conn, fed, "owner", generation, true)
				requester := productionSyncJoin(ctx, t, conn, fed, "requester", generation, true)
				model, err := source.foms.Get(ctx, fed)
				if err != nil {
					t.Fatal(err)
				}
				class, _ := model.LookupObjectClass("Vehicle")
				attr, _ := model.LookupAttribute(class, "value")
				decl := rtiv1.NewDeclarationServiceClient(conn)
				for _, member := range []productionSyncMember{owner, requester} {
					if _, err := decl.PublishObjectClassAttributes(member.ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}}); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := decl.SubscribeObjectClassAttributes(requester.ctx, &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: requester.h, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}}); err != nil {
					t.Fatal(err)
				}
				registered, err := rtiv1.NewObjectServiceClient(conn).RegisterObjectInstance(owner.ctx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: fed, FederateHandle: owner.h, ObjectClassHandle: uint64(class)})
				if err != nil {
					t.Fatal(err)
				}
				discover := productionSyncReceive(t, requester, func(e *rtiv1.FederateEvent) bool { return e.GetDiscover() != nil })
				productionSyncReport(t, conn, fed, requester, discover, true)
				productionSyncReport(t, conn, fed, requester, discover, false)
				epoch, err := source.outbox.CallbackEpoch(fed, core.FederateHandle(requester.h))
				if err != nil {
					t.Fatal(err)
				}
				receipts := rtiv1.NewOwnershipCallbackServiceClient(conn)
				if _, err := receipts.EnableOwnershipCallbackReceipts(requester.ctx, &rtiv1.OwnershipCallbackScopeRequest{WireVersion: 1, FederationName: fed, FederateHandle: requester.h, FederationGeneration: generation, CallbackEpoch: epoch}); err != nil {
					t.Fatal(err)
				}
				if _, err := rtiv1.NewOwnershipServiceClient(conn).AttributeOwnershipAcquisition(requester.ctx, &rtiv1.AcquireRequest{WireVersion: 1, FederationName: fed, FederateHandle: requester.h, ObjectHandle: registered.ObjectHandle, AttributeHandles: []uint64{uint64(attr)}, IfAvailable: true}); err != nil {
					t.Fatal(err)
				}
				original := productionSyncReceive(t, requester, func(e *rtiv1.FederateEvent) bool { return e.GetOwnershipUnavailable() != nil })
				if original.OwnershipReceipt == nil || original.OwnershipReceipt.DeliveryAttempt == 0 {
					t.Fatal("source terminal has no ownership authority")
				}
				identity := sha256.Sum256(original.CallbackReceipt)
				if phase != testPendingLabel {
					productionSyncReport(t, conn, fed, requester, original, true)
				}
				if phase == "outcome-owned-completed" || phase == testCompletedLabel {
					if _, err := receipts.ReportOwnershipCallbackInvocation(requester.ctx, &rtiv1.ReportOwnershipCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: requester.h, Receipt: original.OwnershipReceipt}); err != nil {
						t.Fatal(err)
					}
				}
				if phase == testCompletedLabel {
					productionSyncReport(t, conn, fed, requester, original, false)
				}
				saves := rtiv1.NewSavepointServiceClient(conn)
				if _, err := saves.RequestFederationSave(owner.ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: owner.h, Label: testSavedLabel}); err != nil {
					t.Fatal(err)
				}
				for _, member := range []productionSyncMember{owner, requester} {
					init := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSaveInitiate() != nil })
					productionSyncReport(t, conn, fed, member, init, true)
					productionSyncReport(t, conn, fed, member, init, false)
					outcomes := []map[string]any{}
					if member.h == requester.h && phase != testPendingLabel {
						outcomes = append(outcomes, map[string]any{"identity": hex.EncodeToString(identity[:]), "success": true, "exception": ""})
					}
					state, err := json.Marshal(map[string]any{"callback_epoch": epoch, "known_objects": []any{}, "callback_recovery": "server-v2", "pending_outcomes": outcomes})
					if err != nil {
						t.Fatal(err)
					}
					runtime, err := sdkstate.Encode(sdkstate.Envelope{Language: "go", Scalar: factory, Generation: 481, Self: member.h, State: state})
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
				}
				stop()
				target, targetConn, _ := integerProductionServer(t, dir, 482)
				currentGeneration := create(targetConn)
				current := productionSyncJoin(ctx, t, targetConn, fed, "requester", currentGeneration, true)
				currentOwner := productionSyncJoin(ctx, t, targetConn, fed, "owner", currentGeneration, true)
				restore := rtiv1.NewSavepointServiceClient(targetConn)
				if _, err := restore.RequestFederationRestore(currentOwner.ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: currentOwner.h, Label: testSavedLabel}); err != nil {
					t.Fatal(err)
				}
				for _, member := range []productionSyncMember{currentOwner, current} {
					productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetRestoreInitiate() != nil })
					if _, err := restore.FederateRestoreComplete(member.ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h}); err != nil {
						t.Fatal(err)
					}
				}
				for {
					event, err := current.receive()
					if err != nil {
						t.Fatal(err)
					}
					if event.CallbackRecovery != nil {
						t.Fatal("ownership recovery overtook restore terminal")
					}
					if event.GetRestoreCompleted() != nil {
						current.h = event.GetRestoreCompleted().GetFederateHandle()
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
					if !replayed.GetCallbackRecovery().GetCompleted() || !replayed.GetCallbackRecovery().GetOutcomeOnly() || replayed.Event != nil || len(replayed.CallbackReceipt) != 0 || replayed.OwnershipReceipt != nil {
						t.Fatal("completed ownership reconciliation retained body or authority")
					}
				} else {
					body := replayed.GetOwnershipUnavailable()
					if body == nil || body.ObjectHandle != registered.ObjectHandle || len(body.AttributeHandles) != 1 || body.AttributeHandles[0] != uint64(attr) || len(replayed.CallbackReceipt) != 32 || bytes.Equal(replayed.CallbackReceipt, original.CallbackReceipt) || replayed.GetCallbackRecovery().GetOutcomeOnly() != (phase != testPendingLabel) {
						t.Fatal("ownership replay changed body, phase or general authority", replayed)
					}
					if phase == "outcome-owned-completed" {
						if replayed.OwnershipReceipt != nil {
							t.Fatal("completed ownership acquired another authority")
						}
					} else {
						receipt := replayed.OwnershipReceipt
						currentEpoch, err := target.outbox.CallbackEpoch(fed, core.FederateHandle(current.h))
						if err != nil || receipt == nil || receipt.CallbackId != original.OwnershipReceipt.CallbackId || receipt.FederationGeneration != currentGeneration || receipt.CallbackEpoch != currentEpoch || receipt.DeliveryAttempt == 0 {
							t.Fatal("ownership authority was not freshly scoped", receipt, err)
						}
						if phase == testPendingLabel {
							productionSyncReport(t, targetConn, fed, current, replayed, true)
						}
						if _, err := rtiv1.NewOwnershipCallbackServiceClient(targetConn).ReportOwnershipCallbackInvocation(current.ctx, &rtiv1.ReportOwnershipCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: current.h, Receipt: receipt}); err != nil {
							t.Fatal(err)
						}
					}
					// Known finished outcomes do not issue a second Entry.
					productionSyncReport(t, targetConn, fed, current, replayed, false)
					productionSyncReport(t, targetConn, fed, current, replayed, false)
				}
				if target.ownMgr.(*ownership.Manager).HasAcquisitionObligation(fed, core.FederateHandle(current.h), core.ObjectHandle(registered.ObjectHandle), []core.AttributeHandle{attr}) {
					t.Fatal("reconciled ownership obligation remains")
				}
				_, err = rtiv1.NewOwnershipCallbackServiceClient(targetConn).ReportOwnershipCallbackInvocation(current.ctx, &rtiv1.ReportOwnershipCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: current.h, Receipt: original.OwnershipReceipt})
				if status.Code(err) != codes.FailedPrecondition {
					t.Fatal("source ownership authority accepted", err)
				}
				_, err = rtiv1.NewSupportServiceClient(targetConn).ReportCallbackInvocation(current.ctx, &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: current.h, CallbackReceipt: original.CallbackReceipt, InvocationEntry: true, Success: true})
				if status.Code(err) != codes.FailedPrecondition {
					t.Fatal("source general authority accepted", err)
				}
			})
		}
	}
}
