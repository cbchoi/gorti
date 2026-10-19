package server

import (
	"context"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

func TestRestoreGeneratedRateBundleUsesSavedFOMCandidate(t *testing.T) {
	srv, conn, _ := scheduledServer(t)
	decl := srv.declMgr.(*declaration.Manager)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const fed core.FederationName = "saved-rate"
	federations, saves := rtiv1.NewFederationServiceClient(conn), rtiv1.NewSavepointServiceClient(conn)
	_, err := federations.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: string(fed), FomModules: []*rtiv1.FOMModule{{Path: "base.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>V</name><attribute><name>x</name></attribute></objectClass></objectClass></objects></objectModel>`)}}})
	if err != nil {
		t.Fatal(err)
	}
	member, err := federations.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: string(fed), FederateName: "subscriber"})
	if err != nil {
		t.Fatal(err)
	}
	h := member.FederateHandle
	queue, stop, err := srv.outbox.Subscribe(ctx, fed, core.FederateHandle(h))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stop() }()
	save := func(label string) {
		t.Helper()
		drainScheduledEvents(queue)
		if _, err := saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: h, Label: label}); err != nil {
			t.Fatal(err)
		}
		response := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: h}
		if _, err := saves.FederateSaveBegun(ctx, response); err != nil {
			t.Fatal(err)
		}
		if _, err := saves.FederateSaveComplete(ctx, response); err != nil {
			t.Fatal(err)
		}
		drainScheduledEvents(queue)
	}
	hasRate := func() bool {
		t.Helper()
		model, err := srv.foms.Get(ctx, fed)
		if err != nil {
			t.Fatal(err)
		}
		_, exists := model.(core.FOMUpdateRates).UpdateRateValue("NewRate")
		return exists
	}
	restore := func(label string, expectedBefore bool) {
		t.Helper()
		if _, err := saves.RequestFederationRestore(ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: h, Label: label}); err != nil {
			t.Fatalf("restore generated %s bundle: %v", label, err)
		}
		if hasRate() != expectedBefore {
			t.Fatal("FOM candidate published before restore completion")
		}
		drainScheduledEvents(queue)
		if _, err := saves.FederateRestoreComplete(ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: h}); err != nil {
			t.Fatal(err)
		}
		drainScheduledEvents(queue)
	}
	save("base")
	extender, err := federations.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: string(fed), FederateName: "extender", AdditionalFomModules: []*rtiv1.FOMModule{{Path: "rate.xml", Xml: []byte(`<objectModel><updateRates><updateRate><name>NewRate</name><rate>12.5</rate></updateRate></updateRates></objectModel>`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.fedMgr.ResignFederation(ctx, fed, core.FederateHandle(extender.FederateHandle), core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	model, err := srv.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	cls, _ := model.LookupObjectClass("HLAobjectRoot.V")
	attr, _ := model.LookupAttribute(cls, "x")
	if err := decl.SubscribeObjectClassAttributesWithOptions(ctx, fed, core.FederateHandle(h), cls, []core.AttributeHandle{attr}, core.SubscriptionOptions{Active: true, UpdateRateDesignator: "NewRate"}); err != nil {
		t.Fatal(err)
	}
	save("rate")
	restore("base", true)
	if hasRate() {
		t.Fatal("base restore retained future rate")
	}
	restore("rate", false)
	if !hasRate() {
		t.Fatal("rate restore lost its saved model")
	}
	state := decl.ExportState(fed)
	found := false
	for _, entry := range state.ObjectOptions {
		if entry.Federate == core.FederateHandle(h) && entry.Class == cls && entry.Attribute == attr && entry.Options.UpdateRateDesignator == "NewRate" {
			found = true
		}
	}
	if !found {
		t.Fatal("rate subscription not restored")
	}
}
