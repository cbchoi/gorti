package server

import (
	"context"
	"reflect"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncdomain "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestSynchronizationConfirmationDefersAcrossCheckpoint(t *testing.T) {
	for _, scalar := range []string{float64TimeName, integer64TimeName} {
		for _, phase := range []string{"save", testRestoreLabel} {
			for _, rejected := range []bool{false, true} {
				outcome := "succeeded"
				if rejected {
					outcome = "failed"
				}
				t.Run(scalar+"/"+phase+"/"+outcome, func(t *testing.T) {
					srv, conn, _ := integerProductionServer(t, t.TempDir(), 791)
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					const fed = "sync-checkpoint-admission"
					created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: scalar})
					if err != nil {
						t.Fatal(err)
					}
					member := productionSyncJoin(ctx, t, conn, fed, "member", created.FederationGeneration, true)
					saves := rtiv1.NewSavepointServiceClient(conn)
					response := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h}
					var beforeControl func()
					beginSave := func(label string, begun bool) {
						t.Helper()
						if _, err := saves.RequestFederationSave(member.ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, Label: label}); err != nil {
							t.Fatal(err)
						}
						event := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSaveInitiate() != nil })
						if beforeControl != nil {
							beforeControl()
							beforeControl = nil
						}
						productionSyncReport(t, conn, fed, member, event, true)
						productionSyncReport(t, conn, fed, member, event, false)
						if begun {
							if _, err := saves.FederateSaveBegun(member.ctx, response); err != nil {
								t.Fatal(err)
							}
						}
					}
					if phase == testRestoreLabel {
						beginSave("baseline", true)
						if _, err := saves.FederateSaveComplete(member.ctx, response); err != nil {
							t.Fatal(err)
						}
						event := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSaveCompleted() != nil })
						productionSyncReport(t, conn, fed, member, event, true)
						productionSyncReport(t, conn, fed, member, event, false)
					}
					register := func() {
						t.Helper()
						_, err := rtiv1.NewSyncServiceClient(conn).RegisterFederationSynchronizationPoint(member.ctx, &rtiv1.RegisterSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, Label: "queued", Tag: []byte{0, 255}})
						if err != nil {
							t.Fatal(err)
						}
					}
					register()
					confirmation := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSyncRegistrationSucceeded() != nil })
					if rejected {
						productionSyncReport(t, conn, fed, member, confirmation, true)
						productionSyncReport(t, conn, fed, member, confirmation, false)
						register()
						confirmation = productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSyncRegistrationFailed() != nil })
					}
					wantException := "SaveInProgress"
					if _, err := rtiv1.NewSyncServiceClient(conn).RegisterFederationSynchronizationPoint(member.ctx, &rtiv1.RegisterSyncPointRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, Label: "before-control"}); err != nil {
						t.Fatal(err)
					}
					earlier := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool {
						return e.GetSyncRegistrationSucceeded().GetLabel() == "before-control"
					})
					beforeControl = func() {
						productionSyncReport(t, conn, fed, member, earlier, true)
						productionSyncReport(t, conn, fed, member, earlier, false)
					}
					if phase == "save" {
						beginSave("active", false)
					} else {
						wantException = "RestoreInProgress"
						if _, err := saves.RequestFederationRestore(member.ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, Label: "baseline"}); err != nil {
							t.Fatal(err)
						}
						beforeControl()
						beforeControl = nil
						begun := productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetRestoreBegun() != nil })
						productionSyncReport(t, conn, fed, member, begun, true)
						productionSyncReport(t, conn, fed, member, begun, false)
						productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetRestoreInitiate() != nil })
					}
					manager := srv.syncMgr.(*syncdomain.Manager)
					before := manager.ExportState(core.FederationName(fed))
					request := &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h, CallbackReceipt: confirmation.CallbackReceipt, InvocationEntry: true, Success: true}
					for retry := 0; retry < 2; retry++ {
						var trailer metadata.MD
						_, err := rtiv1.NewSupportServiceClient(conn).ReportCallbackInvocation(member.ctx, request, grpc.Trailer(&trailer))
						if status.Code(err) != codes.FailedPrecondition || !reflect.DeepEqual(trailer.Get("rti-spec-exception"), []string{wantException}) {
							t.Fatalf("queued confirmation entered during %s: %v, trailer=%v", phase, err, trailer)
						}
					}
					if !reflect.DeepEqual(before, manager.ExportState(core.FederationName(fed))) {
						t.Fatal("denied entry changed synchronization state")
					}
					var terminal *rtiv1.FederateEvent
					if phase == "save" {
						if _, err := saves.FederateSaveBegun(member.ctx, response); err != nil {
							t.Fatal(err)
						}
						if _, err := saves.FederateSaveNotComplete(member.ctx, response); err != nil {
							t.Fatal(err)
						}
						terminal = productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetSaveFailed() != nil })
					} else {
						if _, err := saves.FederateRestoreNotComplete(member.ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.h}); err != nil {
							t.Fatal(err)
						}
						terminal = productionSyncReceive(t, member, func(e *rtiv1.FederateEvent) bool { return e.GetRestoreFailed() != nil })
					}
					var trailer metadata.MD
					if _, err := rtiv1.NewSupportServiceClient(conn).ReportCallbackInvocation(member.ctx, request, grpc.Trailer(&trailer)); status.Code(err) != codes.FailedPrecondition || !reflect.DeepEqual(trailer.Get("rti-spec-exception"), []string{wantException}) {
						t.Fatalf("confirmation entered before terminal invocation: %v, trailer=%v", err, trailer)
					}
					productionSyncReport(t, conn, fed, member, terminal, true)
					productionSyncReport(t, conn, fed, member, terminal, false)
					// Aborting the checkpoint retains the same pending capability and label.
					productionSyncReport(t, conn, fed, member, confirmation, true)
					productionSyncReport(t, conn, fed, member, confirmation, false)
					productionSyncReport(t, conn, fed, member, confirmation, false)
				})
			}
		}
	}
}
