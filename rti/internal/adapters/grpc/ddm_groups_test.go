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
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type groupedUpdateRegistry struct {
	core.ObjectRegistry
	groups []core.AttributeRegionGroup
	calls  int
}

func (r *groupedUpdateRegistry) RequestAttributeValueUpdateWithRegionGroups(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectClassHandle, groups []core.AttributeRegionGroup, _ []byte) error {
	r.calls++
	r.groups = groups
	return nil
}

func TestDDMGroupedRequestsRealWire(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mgr, err := ddm.New(ddm.Options{Outbox: delivery.NewWithBatch(8, 1, 0), FOMs: ddmAdvisoryFOMs{}})
	if err != nil {
		t.Fatal(err)
	}
	space, _ := mgr.LookupRoutingSpace("fed", "default")
	dim, _ := mgr.LookupDimension("fed", space, "X")
	var regions []uint64
	for i := 0; i < 2; i++ {
		r, err := mgr.CreateRegion(ctx, "fed", 1, space, []ddm.DimensionHandle{dim})
		if err != nil {
			t.Fatal(err)
		}
		regions = append(regions, uint64(r))
		initializeDDMSpecification(t, mgr, "fed", 1, r)
	}
	objects := &groupedUpdateRegistry{}
	server := grpcgo.NewServer()
	rtiv1.RegisterDDMServiceServer(server, newDDMService(mgr, objects))
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	defer listener.Close()
	conn, err := grpcgo.NewClient("passthrough:///ddm-groups", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := rtiv1.NewDDMServiceClient(conn)
	groups := &rtiv1.AttributeRegionGroups{Groups: []*rtiv1.AttributeRegionGroup{
		{AttributeHandles: []uint64{1}, RegionHandles: regions[:1]}, {AttributeHandles: []uint64{2}, RegionHandles: regions[1:]},
	}}
	for _, group := range groups.Groups {
		_, err = client.AssociateRegionsForUpdates(ctx, &rtiv1.AssociateRegionsForUpdatesRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectHandle: 9, AttributeRegionGroups: &rtiv1.AttributeRegionGroups{Groups: []*rtiv1.AttributeRegionGroup{group}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = client.SubscribeObjectClassAttributesWithRegions(ctx, &rtiv1.SubscribeOCAWithRegionsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectClassHandle: 1, AttributeRegionGroups: groups})
	if err != nil {
		t.Fatal(err)
	}
	before := mgr.ExportState("fed")
	invalid := &rtiv1.AttributeRegionGroups{Groups: append(append([]*rtiv1.AttributeRegionGroup(nil), groups.Groups...), &rtiv1.AttributeRegionGroup{RegionHandles: []uint64{999}})}
	for _, invoke := range []func() error{
		func() error {
			_, err := client.SubscribeObjectClassAttributesWithRegions(ctx, &rtiv1.SubscribeOCAWithRegionsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectClassHandle: 1, AttributeRegionGroups: invalid})
			return err
		},
		func() error {
			_, err := client.UnsubscribeObjectClassAttributesWithRegions(ctx, &rtiv1.UnsubscribeOCAWithRegionsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectClassHandle: 1, AttributeRegionGroups: invalid})
			return err
		},
		func() error {
			_, err := client.AssociateRegionsForUpdates(ctx, &rtiv1.AssociateRegionsForUpdatesRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectHandle: 9, AttributeRegionGroups: invalid})
			return err
		},
		func() error {
			_, err := client.UnassociateRegionsForUpdates(ctx, &rtiv1.UnassociateRegionsForUpdatesRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectHandle: 9, AttributeRegionGroups: invalid})
			return err
		},
	} {
		if err := invoke(); err == nil {
			t.Fatal("invalid later group accepted")
		}
		if !reflect.DeepEqual(before, mgr.ExportState("fed")) {
			t.Fatal("invalid later wire group changed state")
		}
	}
	_, err = client.UnassociateRegionsForUpdates(ctx, &rtiv1.UnassociateRegionsForUpdatesRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectHandle: 9, AttributeRegionGroups: &rtiv1.AttributeRegionGroups{}})
	if err != nil || !reflect.DeepEqual(before, mgr.ExportState("fed")) {
		t.Fatalf("empty wrapper lost its presence: %v", err)
	}
	_, err = client.RequestAttributeValueUpdateWithRegions(ctx, &rtiv1.RequestAttributeValueUpdateWithRegionsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectClassHandle: 1, AttributeRegionGroups: groups})
	if err != nil || objects.calls != 1 || len(objects.groups) != 2 {
		t.Fatalf("grouped request split: %v %+v", err, objects)
	}
	_, err = client.SubscribeObjectClassAttributesWithRegions(ctx, &rtiv1.SubscribeOCAWithRegionsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectClassHandle: 1, AttributeRegionGroups: groups, AttributeHandles: []uint64{1}})
	if err == nil || !reflect.DeepEqual(before, mgr.ExportState("fed")) {
		t.Fatal("mixed legacy/grouped payload accepted")
	}
	_, err = client.UnsubscribeObjectClassAttributesWithRegions(ctx, &rtiv1.UnsubscribeOCAWithRegionsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectClassHandle: 1, AttributeRegionGroups: groups})
	if err != nil || len(mgr.ExportState("fed").ObjectSubscriptions) != 0 {
		t.Fatalf("grouped unsubscribe: %v", err)
	}
}
