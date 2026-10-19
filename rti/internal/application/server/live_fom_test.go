package server

import (
	"context"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestLiveFOMJoinProductionWireAndSnapshot(t *testing.T) {
	srv, conn, _ := scheduledServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := rtiv1.NewFederationServiceClient(conn)
	const name = "live-fom"
	base := []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Z</name><attribute><name>value</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objects><dimensions><dimension><name>Z</name><upperBound>100</upperBound></dimension></dimensions></objectModel>`)
	extra := []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>A</name></objectClass></objectClass></objects><dimensions><dimension><name>A</name><upperBound>200</upperBound></dimension></dimensions></objectModel>`)
	created, err := client.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: name, FomModules: []*rtiv1.FOMModule{{Path: "base.xml", Xml: base}}})
	if err != nil {
		t.Fatal(err)
	}
	generation := created.GetFederationGeneration()
	first, err := client.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: name, FederateName: "first", ExpectedFederationGeneration: &generation})
	if err != nil {
		t.Fatal(err)
	}
	if first.GetFomView() == nil || first.FomView.Revision == 0 {
		t.Fatal("join omitted authoritative model")
	}
	before, err := srv.foms.Get(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	oldClass, ok := before.LookupObjectClass("HLAobjectRoot.Z")
	if !ok {
		t.Fatal("base class missing")
	}
	space, ok := srv.ddmMgr.LookupRoutingSpace(name, "default")
	if !ok {
		t.Fatal("default routing space missing")
	}
	oldDimension, ok := srv.ddmMgr.LookupDimension(name, space, "Z")
	if !ok {
		t.Fatal("base dimension missing")
	}
	snapshot := savebundle.FOM{Repository: srv.foms}
	saved, err := snapshot.Marshal(name)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: name, FederateName: "second", ExpectedFederationGeneration: &generation, AdditionalFomModules: []*rtiv1.FOMModule{{Path: "extension.xml", Xml: extra}}})
	if err != nil {
		t.Fatal(err)
	}
	if second.FomView.Revision != first.FomView.Revision+1 {
		t.Fatal("revision did not advance")
	}
	current, err := srv.foms.Get(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := current.LookupObjectClass("HLAobjectRoot.Z"); !ok || got != oldClass {
		t.Fatal("existing class remapped")
	}
	if _, ok := current.LookupObjectClass("HLAobjectRoot.A"); !ok {
		t.Fatal("new class missing")
	}
	if got, ok := srv.ddmMgr.LookupDimension(name, space, "Z"); !ok || got != oldDimension {
		t.Fatal("existing DDM dimension remapped")
	}
	if _, ok := srv.ddmMgr.LookupDimension(name, space, "A"); !ok {
		t.Fatal("DDM cache not refreshed")
	}
	view, err := client.GetFOMView(ctx, &rtiv1.GetFOMViewRequest{WireVersion: 1, FederationName: name, FederateHandle: first.FederateHandle})
	if err != nil || view.GetRevision() != second.FomView.Revision {
		t.Fatalf("existing member refresh: %v %v", view, err)
	}
	bad := []byte(`<objectModel><dimensions><dimension><name>Z</name><upperBound>999</upperBound></dimension></dimensions></objectModel>`)
	_, err = client.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: name, FederateName: "bad", ExpectedFederationGeneration: &generation, AdditionalFomModules: []*rtiv1.FOMModule{{Xml: bad}}})
	if err == nil {
		t.Fatal("conflicting extension admitted")
	}
	if len(srv.fedMgr.MembersOf(core.FederationName(name))) != 2 {
		t.Fatal("failed join left roster entry")
	}
	if err := snapshot.Unmarshal(name, saved); err != nil {
		t.Fatal(err)
	}
	old, err := srv.foms.Get(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := old.LookupObjectClass("HLAobjectRoot.A"); ok {
		t.Fatal("restored FOM retained future class")
	}
	if got, ok := old.LookupObjectClass("HLAobjectRoot.Z"); !ok || got != oldClass {
		t.Fatal("restored FOM lost stable binding")
	}
}
