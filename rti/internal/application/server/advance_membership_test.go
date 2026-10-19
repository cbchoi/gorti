package server

import (
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAdvanceMembership_ProductionGRPC(t *testing.T) {
	const fed core.FederationName = "advance-membership"
	ctx, srv, conn, _ := momCompletionReviewServer(t, fed)
	feds := rtiv1.NewFederationServiceClient(conn)
	created, err := feds.CreateFederation(ctx, &rtiv1.CreateFederationRequest{
		WireVersion: 1, FederationName: string(fed),
		FomModules: []*rtiv1.FOMModule{{Path: "empty.xml", Xml: []byte(`<objectModel/>`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	join := func(t *testing.T, name string) uint64 {
		t.Helper()
		joined, err := feds.JoinFederation(ctx, &rtiv1.JoinFederationRequest{
			WireVersion: 1, FederationName: string(fed), FederateName: name,
			ExpectedFederationGeneration: &created.FederationGeneration,
		})
		if err != nil {
			t.Fatal(err)
		}
		return joined.FederateHandle
	}
	client := rtiv1.NewTimeServiceClient(conn)
	services := []struct {
		name string
		call func(context.Context, uint64, ...stdgrpc.CallOption) (*rtiv1.Empty, error)
	}{
		{"TAR", func(ctx context.Context, h uint64, opts ...stdgrpc.CallOption) (*rtiv1.Empty, error) {
			return client.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: h, LogicalTime: 7}, opts...)
		}},
		{"TARA", func(ctx context.Context, h uint64, opts ...stdgrpc.CallOption) (*rtiv1.Empty, error) {
			return client.TimeAdvanceRequestAvailable(ctx, &rtiv1.TARARequest{WireVersion: 1, FederationName: string(fed), FederateHandle: h, LogicalTime: 7}, opts...)
		}},
		{testNextMessageRequest, func(ctx context.Context, h uint64, opts ...stdgrpc.CallOption) (*rtiv1.Empty, error) {
			return client.NextMessageRequest(ctx, &rtiv1.NERRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: h, LogicalTime: 7}, opts...)
		}},
		{testNextMessageRequestAvailable, func(ctx context.Context, h uint64, opts ...stdgrpc.CallOption) (*rtiv1.Empty, error) {
			return client.NextMessageRequestAvailable(ctx, &rtiv1.NMRARequest{WireVersion: 1, FederationName: string(fed), FederateHandle: h, LogicalTime: 7}, opts...)
		}},
		{testFlushQueueRequest, func(ctx context.Context, h uint64, opts ...stdgrpc.CallOption) (*rtiv1.Empty, error) {
			return client.FlushQueueRequest(ctx, &rtiv1.FQRRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: h, LogicalTime: 7}, opts...)
		}},
	}
	for _, service := range services {
		t.Run(service.name, func(t *testing.T) {
			t.Run("joined-unengaged", func(t *testing.T) {
				h := join(t, service.name+"-joined")
				if _, err := service.call(ctx, h); err != nil {
					t.Fatalf("joined unengaged advance rejected by production server: %v", err)
				}
				for _, entry := range srv.timeMgr.Snapshot(fed).Federates {
					if entry.Handle != core.FederateHandle(h) {
						continue
					}
					if entry.CurrentTime != 7 || entry.HasPendingRequest || entry.Regulating || entry.Constrained {
						t.Fatalf("joined unengaged caller did not advance: %+v", entry)
					}
					return
				}
				t.Fatal("successful advance did not create logical-time state")
			})
			for _, membership := range []string{"unknown", "resigned"} {
				t.Run(membership, func(t *testing.T) {
					h := ^uint64(0)
					if membership == "resigned" {
						h = join(t, service.name+"-resigned")
						if _, err := feds.ResignFederation(ctx, &rtiv1.ResignFederationRequest{
							WireVersion: 1, FederationName: string(fed), FederateHandle: h,
							Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION,
						}); err != nil {
							t.Fatal(err)
						}
					}
					before := srv.timeMgr.Snapshot(fed)
					var trailers metadata.MD
					_, err := service.call(ctx, h, stdgrpc.Trailer(&trailers))
					if status.Code(err) != codes.FailedPrecondition {
						t.Errorf("%s caller: got %v, want FailedPrecondition", membership, err)
					}
					if got := trailers.Get("rti-spec-exception"); len(got) != 1 || got[0] != testNotExecutionMember {
						t.Errorf("%s caller exception = %v, want FederateNotExecutionMember (error=%v)", membership, got, err)
					}
					if after := srv.timeMgr.Snapshot(fed); !reflect.DeepEqual(after, before) {
						t.Errorf("rejected caller created or mutated time state: before=%+v after=%+v", before, after)
					}
				})
			}
		})
	}
}
