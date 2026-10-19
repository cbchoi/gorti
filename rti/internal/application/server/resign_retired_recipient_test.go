package server

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

func TestResignAfterOwnershipTransferDoesNotTargetRetiredSubscriber(t *testing.T) {
	for _, implementation := range []string{float64TimeName, integer64TimeName} {
		t.Run(implementation, func(t *testing.T) {
			srv, conn, ctx := resignRPCServer(t)
			_, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{
				WireVersion: 1, FederationName: "retired", LogicalTimeImplementationName: implementation,
				FomModules: []*rtiv1.FOMModule{{Path: "retired.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>V</name><attribute><name>x</name></attribute><attribute><name>y</name></attribute></objectClass></objectClass></objects></objectModel>`)}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var handles []core.FederateHandle
			for _, name := range []string{"publisher", "subscriber"} {
				h, err := srv.fedMgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "retired", FederateName: name, FederateType: "test"})
				if err != nil {
					t.Fatal(err)
				}
				handles = append(handles, h)
			}
			pub, sub := handles[0], handles[1]
			fom, err := srv.foms.Get(ctx, "retired")
			if err != nil {
				t.Fatal(err)
			}
			class, _ := fom.LookupObjectClass("HLAobjectRoot.V")
			x, _ := fom.LookupAttribute(class, "x")
			y, _ := fom.LookupAttribute(class, "y")
			attrs := []core.AttributeHandle{x, y}
			for _, h := range handles {
				if err := srv.declMgr.PublishObjectClassAttributes(ctx, "retired", h, class, attrs); err != nil {
					t.Fatal(err)
				}
			}
			if err := srv.declMgr.SubscribeObjectClassAttributes(ctx, "retired", sub, class, attrs); err != nil {
				t.Fatal(err)
			}
			var obj core.ObjectHandle
			if implementation == integer64TimeName {
				obj, _, err = srv.integerObjects.Register(ctx, "retired", pub, class, "")
			} else {
				obj, _, err = srv.objReg.Register(ctx, "retired", pub, class, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			owners := srv.ownMgr.(*ownership.Manager)
			if err := owners.NegotiatedDivestTwoPhase(ctx, "retired", pub, obj, []core.AttributeHandle{x}, nil); err != nil {
				t.Fatal(err)
			}
			if err := owners.Acquire(ctx, "retired", sub, obj, []core.AttributeHandle{x}, nil); err != nil {
				t.Fatal(err)
			}
			if err := owners.ConfirmDivestiture(ctx, "retired", pub, obj, []core.AttributeHandle{x}); err != nil {
				t.Fatal(err)
			}
			if !owners.IsOwnedBy("retired", sub, obj, x) {
				t.Fatal("transfer did not commit")
			}
			if err := srv.fedMgr.ResignFederation(ctx, "retired", sub, core.ResignActionCancelThenDelete); err != nil {
				t.Fatal(err)
			}
			if members := srv.fedMgr.MembersOf("retired"); len(members) != 1 || members[0] != pub {
				t.Fatalf("retired subscriber still joined: %v", members)
			}
			known := false
			if implementation == integer64TimeName {
				_, _, known = srv.integerObjects.LookupKnownObjectInstance("retired", sub, obj)
			} else {
				_, _, known = srv.objReg.LookupKnownObjectInstance("retired", sub, obj)
			}
			if known {
				t.Fatal("ownership refresh rediscovered an object for the retired subscriber")
			}
			if !owners.IsOwnedBy("retired", pub, obj, y) {
				t.Fatal("subscriber resignation changed surviving ownership")
			}
			if err := srv.fedMgr.ResignFederation(ctx, "retired", pub, core.ResignActionCancelThenDelete); err != nil {
				t.Logf("ownership state: %+v", owners.ExportState("retired"))
				t.Fatalf("publisher resignation targeted a retired recipient: %v", err)
			}
			if members := srv.fedMgr.MembersOf("retired"); len(members) != 0 {
				t.Fatalf("final resignation retained membership: %v", members)
			}
			classOf := srv.objReg.ClassOf
			if implementation == integer64TimeName {
				classOf = srv.integerObjects.ClassOf
			}
			if classOf("retired", obj) != 0 {
				t.Fatal("last-member resignation retained the object")
			}
		})
	}
}
