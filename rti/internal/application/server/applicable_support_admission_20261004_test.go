package server

import (
	"context"
	"reflect"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestApplicableSupportReportingSubscriptionAdmission(t *testing.T) {
	for _, active := range []bool{true, false} {
		name := "active"
		if !active {
			name = "passive"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			r, conn, _ := integerProductionServer(t, t.TempDir(), 253)
			const fed = "support-report-admission"
			created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
			if err != nil {
				t.Fatal(err)
			}
			member := momCompletionReviewJoin(ctx, t, conn, fed, "subject", "", created.FederationGeneration)
			auth := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(member.SessionToken))
			model, err := r.foms.Get(ctx, fed)
			if err != nil {
				t.Fatal(err)
			}
			class, ok := model.LookupInteractionClass(mom.ClassReportServiceInvocation)
			if !ok {
				t.Fatal("MIM class missing")
			}
			manager := r.momMgr.(*mom.Manager)
			decl := r.declMgr.(*declaration.Manager)
			manager.SetServiceReporting(fed, core.FederateHandle(member.FederateHandle), true)
			before := decl.ExportState(fed)
			request := &rtiv1.SubInterRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.FederateHandle, InteractionClassHandle: uint64(class), Active: &active}
			var trailer metadata.MD
			_, err = rtiv1.NewDeclarationServiceClient(conn).SubscribeInteractionClass(auth, request, grpc.Trailer(&trailer))
			if status.Code(err) != codes.FailedPrecondition || len(trailer.Get("rti-spec-exception")) != 1 || trailer.Get("rti-spec-exception")[0] != "FederateServiceInvocationsAreBeingReportedViaMOM" {
				t.Errorf("reporting subscription: error=%v metadata=%v", err, trailer)
			}
			if !reflect.DeepEqual(before, decl.ExportState(fed)) {
				t.Error("rejection changed declaration state")
			}
			manager.SetServiceReporting(fed, core.FederateHandle(member.FederateHandle), false)
			if _, err := rtiv1.NewDeclarationServiceClient(conn).SubscribeInteractionClass(auth, request); err != nil {
				t.Fatal(err)
			}
			if got := decl.InteractionSubscribersFor(ctx, fed, class); len(got) != 1 || got[0] != core.FederateHandle(member.FederateHandle) {
				t.Fatalf("disabled-switch positive control: %v", got)
			}
		})
	}
}
