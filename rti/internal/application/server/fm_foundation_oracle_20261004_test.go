package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

func TestFMFoundation20261004FreshExecutionHasOnlyRequiredMOMInstance(t *testing.T) {
	srv, conn, _ := integerProductionServer(t, t.TempDir(), 76)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, family := range []string{float64TimeName, integer64TimeName} {
		t.Run(family, func(t *testing.T) {
			if _, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: family, LogicalTimeImplementationName: family, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}}); err != nil {
				t.Fatal(err)
			}
			fed := core.FederationName(family)
			if len(srv.fedMgr.MembersOf(fed)) != 0 {
				t.Fatalf("fresh execution has joined federates: %v", srv.fedMgr.MembersOf(fed))
			}
			model, err := srv.foms.Get(ctx, fed)
			if err != nil {
				t.Fatal(err)
			}
			class, ok := model.LookupObjectClass(mom.ClassHLAfederation)
			if !ok {
				t.Fatal("required MOM federation class is absent")
			}
			// 11.2(b) requires this RTI-owned singleton. This bounded check does
			// not settle 4.1's pre-first-join registration timing or message queues.
			objects := srv.objReg.ExportState(fed).Objects
			objects = append(objects, srv.integerObjects.ExportState(fed).Objects...)
			if len(objects) != 1 || objects[0].Class != class || objects[0].Registrant != ^core.FederateHandle(0) || objects[0].Name != "HLAfederation."+family {
				t.Fatalf("fresh execution must contain only its RTI-owned MOM singleton: %+v", objects)
			}
		})
	}
}

func TestFMFoundation20261004MultipleMembershipsAcrossExecutions(t *testing.T) {
	srv, conn, _ := integerProductionServer(t, t.TempDir(), 73)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := rtiv1.NewFederationServiceClient(conn)
	for _, family := range []string{float64TimeName, integer64TimeName} {
		created, err := client.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: family, LogicalTimeImplementationName: family, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
		if err != nil {
			t.Fatal(err)
		}
		fed := core.FederationName(family)
		first := momCompletionReviewJoin(ctx, t, conn, family, "first", "", created.FederationGeneration)
		second := momCompletionReviewJoin(ctx, t, conn, family, "second", "", created.FederationGeneration)
		if first.FederateHandle == second.FederateHandle || len(srv.fedMgr.MembersOf(fed)) != 2 {
			t.Fatal("one application could not retain two distinct memberships")
		}
	}
	for _, family := range []core.FederationName{float64TimeName, integer64TimeName} {
		if len(srv.fedMgr.MembersOf(family)) != 2 {
			t.Fatal("joining the other execution displaced existing memberships")
		}
	}
}

func TestFMFoundation20261004LastResignDoesNotRediscoverUnownedInstance(t *testing.T) {
	for _, family := range []string{float64TimeName, integer64TimeName} {
		t.Run(family, func(t *testing.T) {
			srv, conn, _ := integerProductionServer(t, t.TempDir(), 74)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			const fed = "fm-foundation-unowned"
			client := rtiv1.NewFederationServiceClient(conn)
			created, err := client.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: family, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
			if err != nil {
				t.Fatal(err)
			}
			member := momCompletionReviewJoin(ctx, t, conn, fed, "owner", "", created.FederationGeneration)
			model, err := srv.foms.Get(ctx, fed)
			if err != nil {
				t.Fatal(err)
			}
			class, _ := model.LookupObjectClass("HLAobjectRoot.Item")
			privilege, _ := model.LookupAttribute(class, "HLAprivilegeToDeleteObject")
			decl := rtiv1.NewDeclarationServiceClient(conn)
			if _, err := decl.PublishObjectClassAttributes(ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(privilege)}}); err != nil {
				t.Fatal(err)
			}
			obj, err := rtiv1.NewObjectServiceClient(conn).RegisterObjectInstance(ctx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.FederateHandle, ObjectClassHandle: uint64(class)})
			if err != nil {
				t.Fatal(err)
			}
			owners := srv.ownMgr.(*ownership.Manager)
			if err := owners.UnconditionalDivest(ctx, fed, core.FederateHandle(member.FederateHandle), core.ObjectHandle(obj.ObjectHandle), []core.AttributeHandle{privilege}); err != nil {
				t.Fatal(err)
			}
			for _, entry := range owners.ExportState(fed).Owners {
				if entry.Object == core.ObjectHandle(obj.ObjectHandle) && entry.Owner != 0 {
					t.Fatal("fixture object still has an owner")
				}
			}
			if _, err := client.ResignFederation(ctx, &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: fed, FederateHandle: member.FederateHandle, Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION}); err != nil {
				t.Fatal(err)
			}
			next := momCompletionReviewJoin(ctx, t, conn, fed, "next", "", created.FederationGeneration)
			if _, err := decl.SubscribeObjectClassAttributes(ctx, &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: next.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(privilege)}}); err != nil {
				t.Fatal(err)
			}
			assertNoRetiredDiscovery(ctx, t, conn, fed, next.FederateHandle, obj.ObjectHandle)
		})
	}
}

func TestFMFoundation20261004FederationModuleListExcludesNoncontributors(t *testing.T) {
	srv, conn, _ := integerProductionServer(t, t.TempDir(), 75)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	base := momCompletionReviewModule(t, "Item")
	base.Path = "first.xml"
	_, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "fm-contributors", FomModules: []*rtiv1.FOMModule{base,
		{Path: "duplicate.xml", Xml: bytes.Clone(base.Xml)},
		{Path: "noncontributor.xml", Xml: []byte("<objectModel/>")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := rtiv1.NewMomServiceClient(conn).QueryFederationAttributes(ctx, &rtiv1.QueryFederationAttributesRequest{WireVersion: 1, FederationName: "fm-contributors"})
	if err != nil {
		t.Fatal(err)
	}
	list := response.AttributeValues["HLAFOMmoduleDesignatorList"]
	if len(list) < 4 || binary.BigEndian.Uint32(list[:4]) != 1 {
		t.Fatalf("contributing module count must be one: %x", list)
	}
	if name := catalogRPCString(t, list[4:]); name != "first.xml" {
		t.Fatalf("first contributing designator=%q", name)
	}
	view, err := srv.foms.FOMView(ctx, "fm-contributors")
	if err != nil || len(view.Modules) != 3 || view.Modules[2].Path != "noncontributor.xml" || string(view.Modules[2].XML) != "<objectModel/>" {
		t.Fatalf("MOM filtering changed stored source modules: %+v %v", view.Modules, err)
	}
	supplied, err := momModulesFromSource(view.Modules)
	if err != nil || len(supplied) != 2 || supplied[1].Designator != "noncontributor.xml" || supplied[1].Indicator != 1 || supplied[1].Data != "<objectModel/>" {
		t.Fatalf("federation filtering leaked into supplied-module list: %+v %v", supplied, err)
	}
}
