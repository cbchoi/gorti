package server

import (
	"context"
	"reflect"
	"testing"
	"time"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncdomain "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestFMEarly20261004JoinDuringRestorePreservesRosterAndAllowsRetry(t *testing.T) {
	for _, family := range []string{float64TimeName, integer64TimeName} {
		t.Run(family, func(t *testing.T) {
			srv, conn, _ := integerProductionServer(t, t.TempDir(), 71)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			const fed = "fm-early-restore"
			federations := rtiv1.NewFederationServiceClient(conn)
			created, err := federations.CreateFederation(ctx, &rtiv1.CreateFederationRequest{
				WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: family,
				FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")},
			})
			if err != nil {
				t.Fatal(err)
			}
			member := momCompletionReviewJoin(ctx, t, conn, fed, "member", "", created.FederationGeneration)
			channel, release, err := srv.outbox.Subscribe(ctx, fed, core.FederateHandle(member.FederateHandle))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = release() }()
			saves := rtiv1.NewSavepointServiceClient(conn)
			if _, err := saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.FederateHandle, Label: testSavedLabel}); err != nil {
				t.Fatal(err)
			}
			initiated := 0
			for _, event := range drainScheduledEvents(channel) {
				wire, err := grpcadapter.EncodeOutboundEvent(event)
				if err != nil {
					t.Fatal(err)
				}
				if save := wire.GetSaveInitiate(); save != nil && save.Label == testSavedLabel {
					initiated++
				}
			}
			if initiated != 1 {
				t.Fatalf("save initiation callbacks=%d", initiated)
			}
			response := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.FederateHandle}
			if _, err := saves.FederateSaveBegun(ctx, response); err != nil {
				t.Fatal(err)
			}
			if _, err := saves.FederateSaveComplete(ctx, response); err != nil {
				t.Fatal(err)
			}
			if _, err := saves.RequestFederationRestore(ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.FederateHandle, Label: testSavedLabel}); err != nil {
				t.Fatal(err)
			}
			before := srv.fedMgr.Snapshot()
			request := &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: fed, FederateName: "retry", ExpectedFederationGeneration: &created.FederationGeneration}
			var trailers metadata.MD
			if _, err := federations.JoinFederation(ctx, request, grpcgo.Trailer(&trailers)); err == nil || !reflect.DeepEqual(trailers.Get("rti-spec-exception"), []string{"RestoreInProgress"}) {
				t.Fatalf("restore admission error=%v trailers=%v", err, trailers)
			}
			if !reflect.DeepEqual(before, srv.fedMgr.Snapshot()) {
				t.Fatal("rejected join changed roster or generation")
			}
			if _, err := saves.AbortFederationRestore(ctx, &rtiv1.AbortFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: &member.FederateHandle}); err != nil {
				t.Fatal(err)
			}
			joined, err := federations.JoinFederation(ctx, request)
			if err != nil || joined.GetLogicalTimeImplementationName() != family || joined.GetFederateHandle() == member.FederateHandle {
				t.Fatalf("retry join=%v err=%v", joined, err)
			}
			if len(srv.fedMgr.MembersOf(fed)) != 2 {
				t.Fatal("retry did not produce exactly one new member")
			}
		})
	}
}

func TestFMEarly20261004MissingExecutionSyncRegistrationHasNoEffects(t *testing.T) {
	srv, conn, _ := integerProductionServer(t, t.TempDir(), 72)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const fed = "fm-early-sync"
	generation := momCompletionReviewCreate(ctx, t, conn, fed, "Item")
	member := momCompletionReviewJoin(ctx, t, conn, fed, "member", "", generation)
	channel, release, err := srv.outbox.Subscribe(ctx, fed, core.FederateHandle(member.FederateHandle))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()
	manager := srv.syncMgr.(*syncdomain.Manager)
	before := manager.ExportState(fed)
	client := rtiv1.NewSyncServiceClient(conn)
	request := &rtiv1.RegisterSyncPointRequest{WireVersion: 1, FederationName: "absent", FederateHandle: member.FederateHandle, Label: "point"}
	if _, err := client.RegisterFederationSynchronizationPoint(ctx, request); err == nil {
		t.Fatal("absent execution accepted synchronization registration")
	}
	if !reflect.DeepEqual(before, manager.ExportState(fed)) || manager.ExportState("absent") != nil || len(drainScheduledEvents(channel)) != 0 {
		t.Fatal("rejected registration created a point or callback")
	}
	request.FederationName = fed
	if _, err := client.RegisterFederationSynchronizationPoint(ctx, request); err != nil {
		t.Fatal(err)
	}
	confirmed := 0
	for _, event := range drainScheduledEvents(channel) {
		wire, err := grpcadapter.EncodeOutboundEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		if confirmation := wire.GetSyncRegistrationSucceeded(); confirmation != nil && confirmation.Label == "point" {
			confirmed++
		}
	}
	if confirmed != 1 || manager.QueryState(fed, "point") != syncdomain.StateAnnounced {
		t.Fatalf("valid retry confirmations=%d state=%v", confirmed, manager.QueryState(fed, "point"))
	}
}
