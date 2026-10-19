package grpc

import (
	"context"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type ddmAdvisoryFOM struct{ core.FOMHandle }

func (ddmAdvisoryFOM) IsValid() bool { return true }

func (ddmAdvisoryFOM) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 0, false
}

type ddmAdvisoryFOMs struct{ core.FOMRepository }

func (ddmAdvisoryFOMs) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return ddmAdvisoryFOM{}, nil
}

func TestDDMAdvisoryOverflowRollbackAndRetryRealWire(t *testing.T) {
	for _, interactions := range []bool{false, true} {
		t.Run(map[bool]string{false: "object", true: "interaction"}[interactions], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			repo := ddmAdvisoryFOMs{}
			out := delivery.NewWithBatch(1, 1, 0)
			queue, closeQueue, err := out.Subscribe(ctx, "fed", 1)
			if err != nil {
				t.Fatal(err)
			}
			defer checkTransportCleanup(t, closeQueue)
			decl := declaration.New()
			decl.SetFOMRepository(repo)
			decl.SetAdvisoryOutbox(out)
			if interactions {
				err = decl.PublishInteractionClass(ctx, "fed", 1, 1)
			} else {
				err = decl.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1})
			}
			if err != nil {
				t.Fatal(err)
			}
			mgr, err := ddm.New(ddm.Options{Outbox: out, FOMs: repo, OnObjectSubscriptionInterest: decl.SetRegionalObjectSubscriptionInterest, OnInteractionSubscriptionInterest: decl.SetRegionalInteractionSubscriptionInterest})
			if err != nil {
				t.Fatal(err)
			}
			space, _ := mgr.LookupRoutingSpace("fed", "default")
			dim, _ := mgr.LookupDimension("fed", space, "X")
			region, err := mgr.CreateRegion(ctx, "fed", 2, space, []ddm.DimensionHandle{dim})
			if err != nil {
				t.Fatal(err)
			}
			listener := bufconn.Listen(1 << 20)
			initializeDDMSpecification(t, mgr, "fed", 2, region)
			server := grpcgo.NewServer()
			rtiv1.RegisterDDMServiceServer(server, newDDMService(mgr, nil))
			go func() { _ = server.Serve(listener) }()
			defer server.Stop()
			defer listener.Close()
			conn, err := grpcgo.NewClient("passthrough:///ddm-atomic", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			client := rtiv1.NewDDMServiceClient(conn)
			for step, action := range []string{"active", "passive", "active", "unsubscribe"} {
				invoke := func() error {
					active := action == "active"
					if interactions {
						if action == "unsubscribe" {
							_, err := client.UnsubscribeInteractionClassWithRegions(ctx, &rtiv1.UnsubscribeICWithRegionsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2, InteractionClassHandle: 1, RegionHandles: []uint64{uint64(region)}})
							return err
						}
						_, err := client.SubscribeInteractionClassWithRegions(ctx, &rtiv1.SubscribeICWithRegionsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2, InteractionClassHandle: 1, RegionHandles: []uint64{uint64(region)}, Active: &active})
						return err
					}
					if action == "unsubscribe" {
						_, err := client.UnsubscribeObjectClassAttributesWithRegions(ctx, &rtiv1.UnsubscribeOCAWithRegionsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2, ObjectClassHandle: 1, AttributeHandles: []uint64{1}, RegionHandles: []uint64{uint64(region)}})
						return err
					}
					_, err := client.SubscribeObjectClassAttributesWithRegions(ctx, &rtiv1.SubscribeOCAWithRegionsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2, ObjectClassHandle: 1, AttributeHandles: []uint64{1}, RegionHandles: []uint64{uint64(region)}, Active: &active})
					return err
				}
				beforeDDM, beforeDecl := mgr.ExportState("fed"), decl.ExportState("fed")
				if err := out.Send(ctx, "fed", 1, &declaration.AdvisoryEvent{}); err != nil {
					t.Fatal(err)
				}
				if err := invoke(); status.Code(err) != codes.Internal {
					t.Fatalf("%s full queue error=%v", action, err)
				}
				if !reflect.DeepEqual(beforeDDM, mgr.ExportState("fed")) {
					t.Errorf("%s rejected RPC changed DDM subscriptions", action)
				}
				if !reflect.DeepEqual(beforeDecl, decl.ExportState("fed")) {
					t.Errorf("%s rejected RPC changed advisory interests", action)
				}
				<-queue
				if err := invoke(); err != nil {
					t.Fatal(err)
				}
				want := declaration.AdvisoryStartRegistration
				if step%2 == 1 {
					want = declaration.AdvisoryStopRegistration
				}
				if interactions {
					want += 2
				}
				select {
				case batch := <-queue:
					if len(batch) != 1 || batch[0].(*declaration.AdvisoryEvent).Kind != want {
						t.Fatalf("%s retry edge=%v want=%v", action, batch, want)
					}
				case <-time.After(time.Second):
					t.Fatalf("%s retry lost required advisory edge", action)
				}
				if err := invoke(); err != nil {
					t.Fatal(err)
				}
				select {
				case extra := <-queue:
					t.Fatalf("%s duplicate edge=%v", action, extra)
				default:
				}
			}
		})
	}
}
