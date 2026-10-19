package server

import (
	"context"
	"encoding/binary"
	"reflect"
	"slices"
	"testing"
	"time"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc/metadata"
)

func TestProductionMOMAttributePointGlobalAndRegionalBothFamilies(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		for _, phase := range []string{"late", "refresh", "periodic"} {
			t.Run(implementation+"/"+phase, func(t *testing.T) { productionMOMAttributePoint(t, implementation, phase) })
		}
	}
}

func productionMOMAttributePoint(t *testing.T, implementation, phase string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const fed = "mom-point-root"
	runtime, conn, _ := integerProductionServer(t, t.TempDir(), 253)
	created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: implementation, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
	if err != nil {
		t.Fatal(err)
	}
	var members []*rtiv1.JoinFederationResponse
	for _, name := range []string{"subject-a", "subject-b", "global", "regional"} {
		members = append(members, momCompletionReviewJoin(ctx, t, conn, fed, name, "", created.FederationGeneration))
	}
	model, err := runtime.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	class, _ := model.LookupObjectClass(mom.ClassHLAfederate)
	attr, _ := model.LookupAttribute(class, "HLAfederateHandle")
	dimension, _ := model.(core.FOMHandleNameLookup).LookupDimension("HLAfederate")
	m := runtime.momMgr.(*mom.Manager)
	subjects := map[core.ObjectHandle]core.FederateHandle{}
	var all []core.FederateHandle
	for _, member := range m.ExportState(fed).Federates {
		subjects[member.ObjectHandle] = member.Handle
		all = append(all, member.Handle)
	}
	slices.Sort(all)
	queues := map[uint64]<-chan []core.OutboundEvent{}
	for _, member := range members[2:] {
		q, release, err := runtime.outbox.Subscribe(ctx, fed, core.FederateHandle(member.FederateHandle))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		queues[member.FederateHandle] = q
		drainScheduledEvents(q)
	}
	global, regional := members[2], members[3]
	globalCtx := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(global.SessionToken))
	regionalCtx := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(regional.SessionToken))
	if _, err := rtiv1.NewDeclarationServiceClient(conn).SubscribeObjectClassAttributes(globalCtx, &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: global.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}}); err != nil {
		t.Fatal(err)
	}
	regions := rtiv1.NewDDMServiceClient(conn)
	region, err := regions.CreateRegion(regionalCtx, &rtiv1.CreateRegionRequest{WireVersion: 1, FederationName: fed, FederateHandle: regional.FederateHandle, RoutingSpaceHandle: 1, DimensionHandles: []uint64{uint64(dimension)}})
	if err != nil {
		t.Fatal(err)
	}
	coordinate := uint64(uint32(members[0].FederateHandle))
	if _, err := regions.SetRangeBounds(regionalCtx, &rtiv1.SetRangeBoundsRequest{WireVersion: 1, FederationName: fed, FederateHandle: regional.FederateHandle, RegionHandle: region.RegionHandle, DimensionHandle: uint64(dimension), Bounds: &rtiv1.Range{Lower: coordinate, Upper: coordinate + 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := regions.CommitRegionModifications(regionalCtx, &rtiv1.CommitRegionRequest{WireVersion: 1, FederationName: fed, FederateHandle: regional.FederateHandle, RegionHandles: []uint64{region.RegionHandle}}); err != nil {
		t.Fatal(err)
	}
	if _, err := regions.SubscribeObjectClassAttributesWithRegions(regionalCtx, &rtiv1.SubscribeOCAWithRegionsRequest{WireVersion: 1, FederationName: fed, FederateHandle: regional.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attr)}, RegionHandles: []uint64{region.RegionHandle}}); err != nil {
		t.Fatal(err)
	}
	if phase != "late" {
		for _, q := range queues {
			drainScheduledEvents(q)
		}
		if phase == "refresh" {
			for _, member := range members {
				if err := m.RefreshMetrics(ctx, fed, core.FederateHandle(member.FederateHandle)); err != nil {
					t.Fatal(err)
				}
			}
		} else {
			timing, _ := model.LookupInteractionClass(mom.ClassSetTiming)
			subjectParam, _ := model.LookupParameter(timing, "HLAfederate")
			periodParam, _ := model.LookupParameter(timing, "HLAreportPeriod")
			decl := rtiv1.NewDeclarationServiceClient(conn)
			if _, err := decl.PublishInteractionClass(globalCtx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: fed, FederateHandle: global.FederateHandle, InteractionClassHandle: uint64(timing)}); err != nil {
				t.Fatal(err)
			}
			for _, member := range members {
				handle := make([]byte, 12)
				binary.BigEndian.PutUint32(handle, 8)
				binary.BigEndian.PutUint64(handle[4:], member.FederateHandle)
				if _, err := rtiv1.NewObjectServiceClient(conn).SendInteraction(globalCtx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: fed, FederateHandle: global.FederateHandle, InteractionClassHandle: uint64(timing), Parameters: map[uint64][]byte{uint64(subjectParam): handle, uint64(periodParam): {0, 0, 0, 1}}}); err != nil {
					t.Fatal(err)
				}
			}
			defer func() { _ = m.FederationDestroyed(context.Background(), fed) }()
			time.Sleep(1300 * time.Millisecond)
		}
	}
	observed := func(q <-chan []core.OutboundEvent) []core.FederateHandle {
		var seen []core.FederateHandle
		for _, event := range drainScheduledEvents(q) {
			wire, err := grpcadapter.EncodeOutboundEvent(event)
			if err != nil {
				t.Fatal(err)
			}
			if reflection := wire.GetReflect(); reflection != nil {
				if _, hasValue := reflection.GetAttributes()[uint64(attr)]; !hasValue {
					continue
				}
				if len(wire.GetCallbackReceipt()) == 0 {
					t.Fatal("MOM data reflection bypassed mandatory callback receipt")
				}
				subject, ok := subjects[core.ObjectHandle(reflection.GetObjectHandle())]
				if !ok {
					t.Fatalf("reflection for unknown MOM subject object %d", reflection.GetObjectHandle())
				}
				seen = append(seen, subject)
			}
		}
		slices.Sort(seen)
		return slices.Compact(seen)
	}
	if got := observed(queues[global.FederateHandle]); !reflect.DeepEqual(got, all) {
		t.Errorf("ordinary global subjects=%v want %v", got, all)
	}
	want := []core.FederateHandle{core.FederateHandle(members[0].FederateHandle)}
	if got := observed(queues[regional.FederateHandle]); !reflect.DeepEqual(got, want) {
		t.Errorf("regional %s subjects=%v want only normalized subject %v", phase, got, want)
	}
}
