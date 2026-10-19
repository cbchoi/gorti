package server

import (
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestApplicableFMRestoreServicesRequireExistingExecution(t *testing.T) {
	srv, conn, members := scheduledServer(t)
	client := rtiv1.NewSavepointServiceClient(conn)
	ctx := context.Background()
	before := srv.saveMgr.Snapshot("scheduled")
	operations := map[string]func(string) error{
		"query-save": func(fed string) error {
			_, err := client.QuerySaveState(ctx, &rtiv1.QuerySaveStateRequest{WireVersion: 1, FederationName: fed})
			return err
		},
		"request-restore": func(fed string) error {
			_, err := client.RequestFederationRestore(ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: uint64(members[0]), Label: "absent"})
			return err
		},
		"restore-complete": func(fed string) error {
			_, err := client.FederateRestoreComplete(ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: uint64(members[0])})
			return err
		},
		"restore-not-complete": func(fed string) error {
			_, err := client.FederateRestoreNotComplete(ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: uint64(members[0])})
			return err
		},
		"abort-restore": func(fed string) error {
			h := uint64(members[0])
			_, err := client.AbortFederationRestore(ctx, &rtiv1.AbortFederationRestoreRequest{WireVersion: 1, FederationName: fed, FederateHandle: &h})
			return err
		},
		"query-restore": func(fed string) error {
			_, err := client.QueryRestoreState(ctx, &rtiv1.QueryRestoreStateRequest{WireVersion: 1, FederationName: fed})
			return err
		},
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			if err := operation("nonexistent-execution"); status.Code(err) != codes.NotFound {
				t.Fatalf("missing execution admission: %v", err)
			}
			if after := srv.saveMgr.Snapshot("scheduled"); !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected operation changed other execution: %+v -> %+v", before, after)
			}
		})
	}
	// Positive controls keep the execution-existence rejection distinct from an unavailable service.
	if err := operations["query-save"]("scheduled"); err != nil {
		t.Fatal(err)
	}
	if err := operations["query-restore"]("scheduled"); err != nil {
		t.Fatal(err)
	}
	if _, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "nonexistent-execution"}, grpcgo.WaitForReady(true)); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.fedMgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "nonexistent-execution", FederateName: "after-rejection"}); err != nil {
		t.Fatal(err)
	}
	if err := operations["query-restore"]("nonexistent-execution"); err != nil {
		t.Fatal("rejection poisoned later execution", err)
	}
}
