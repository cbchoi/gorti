package server

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

type momCatalogOutbox struct{ events []core.OutboundEvent }

func (o *momCatalogOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, e core.OutboundEvent) error {
	o.events = append(o.events, e)
	return nil
}

func newMOMCatalogDeps(t *testing.T) (momCatalogDependencies, core.FOMHandle, core.FederateHandle, core.FederateHandle, *momCatalogOutbox) {
	t.Helper()
	ctx := context.Background()
	repo := composition.NewFOMRepository()
	clock := core.NewManualClock(time.Unix(0, 0))
	out := &momCatalogOutbox{}
	modules := []core.FOMModule{{Path: "actual.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>value</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objects><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Pulse</name></interactionClass></interactionClass></interactions><updateRates><updateRate><name>slow</name><rate>1</rate></updateRate><updateRate><name>fast</name><rate>10</rate></updateRate></updateRates></objectModel>`)}}
	fom, err := repo.Load(ctx, modules)
	if err != nil {
		t.Fatal(err)
	}
	fed, err := federation.New(federation.Options{Clock: clock, FOMs: repo})
	if err != nil {
		t.Fatal(err)
	}
	if err := fed.CreateFederation(ctx, core.CreateFederationRequest{Name: "catalog-provider", FOMModules: modules}); err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("catalog-provider", fom)
	a, err := fed.JoinFederation(ctx, core.JoinFederationRequest{Federation: "catalog-provider", FederateName: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := fed.JoinFederation(ctx, core.JoinFederationRequest{Federation: "catalog-provider", FederateName: "b"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := mom.New(mom.Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	traffic := mom.NewTrafficAccumulator()
	if err := m.SetTrafficAccumulator(traffic); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{a, b} {
		if err := m.FederateJoined(ctx, "catalog-provider", h, "member", "test"); err != nil {
			t.Fatal(err)
		}
	}
	decl := declaration.New()
	decl.SetFOMRepository(repo)
	own, err := ownership.New(ownership.Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := object.New(object.Options{Clock: clock, FOMs: repo, Declarations: decl, Outbox: out, Ownership: own, OnRegister: func(f core.FederationName, h core.FederateHandle, o core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
		own.RegisterInitialOwnership(f, h, o, attrs)
	}})
	if err != nil {
		t.Fatal(err)
	}
	sync, err := syncpkg.New(syncpkg.Options{Outbox: out, Members: fed.MembersOf})
	if err != nil {
		t.Fatal(err)
	}
	regions, err := ddm.New(ddm.Options{Outbox: out, FOMs: repo})
	if err != nil {
		t.Fatal(err)
	}
	return momCatalogDependencies{Manager: m, Traffic: traffic, Declarations: decl, Objects: reg, Ownership: own, Synchronization: sync, Regions: regions, FOMs: repo, Federations: fed}, fom, a, b, out
}

func TestMOMCatalogSourcesActualState(t *testing.T) {
	d, fom, a, b, out := newMOMCatalogDeps(t)
	ctx := context.Background()
	const fed core.FederationName = "catalog-provider"
	s, err := newMOMCatalogSources(d)
	if err != nil {
		t.Fatal(err)
	}
	cls, _ := fom.LookupObjectClass("Vehicle")
	attr, _ := fom.LookupAttribute(cls, "value")
	privilege, _ := fom.LookupAttribute(cls, "HLAprivilegeToDeleteObject")
	attrs := []core.AttributeHandle{attr, privilege}
	if err := d.Declarations.PublishObjectClassAttributes(ctx, fed, a, cls, attrs); err != nil {
		t.Fatal(err)
	}
	if err := d.Declarations.SubscribeObjectClassAttributesWithOptions(ctx, fed, b, cls, []core.AttributeHandle{attr}, core.SubscriptionOptions{Active: true, UpdateRateDesignator: "fast"}); err != nil {
		t.Fatal(err)
	}
	obj, _, err := d.Objects.Register(ctx, fed, a, cls, "")
	if err != nil {
		t.Fatal(err)
	}
	subs, err := s.Subscriptions(ctx, fed, b)
	if err != nil || len(subs.Objects) != 1 || subs.Objects[0].MaxUpdateRate != "fast" || !reflect.DeepEqual(subs.Objects[0].Attributes, []core.AttributeHandle{attr}) {
		t.Fatalf("actual subscriptions: %+v %v", subs, err)
	}
	info, err := s.ObjectInstanceInformation(ctx, fed, b, obj)
	if err != nil || !info.Known || info.RegisteredClass != cls || len(info.OwnedAttributes) != 0 {
		t.Fatalf("known object: %+v %v", info, err)
	}
	counts, err := s.ObjectInstanceCounts(ctx, fed, a, mom.ObjectsDeletable)
	if err != nil || counts[uint64(cls)] != 1 {
		t.Fatalf("deletable: %v %v", counts, err)
	}
	before := len(out.events)
	if err := s.ModifyAttributeState(ctx, fed, b, obj, attr, true); err == nil {
		t.Fatal("unpublished grant accepted")
	}
	if len(out.events) != before {
		t.Fatal("failed grant emitted callbacks")
	}
	if err := d.Declarations.PublishObjectClassAttributes(ctx, fed, b, cls, attrs); err != nil {
		t.Fatal(err)
	}
	if err := s.ModifyAttributeState(ctx, fed, b, obj, attr, true); err != nil {
		t.Fatal(err)
	}
	if owner, _ := d.Ownership.QueryOwnership(fed, obj, attr); owner != b {
		t.Fatal("grant not applied")
	}
	if len(out.events) != before {
		t.Fatal("MOM grant notified federates")
	}
	if err := s.ModifyAttributeState(ctx, fed, b, obj, privilege, true); err != nil {
		t.Fatal(err)
	}
	counts, err = s.ObjectInstanceCounts(ctx, fed, a, mom.ObjectsDeletable)
	if err != nil || len(counts) != 0 {
		t.Fatal("registrant mistaken for delete-privilege owner", counts, err)
	}
	if err := d.Traffic.RecordObjectTraffic(fed, a, obj, cls, false); err != nil {
		t.Fatal(err)
	}
	if err := d.Traffic.RecordObjectTraffic(fed, a, obj, cls, false); err != nil {
		t.Fatal(err)
	}
	counts, err = s.ObjectInstanceCounts(ctx, fed, a, mom.ObjectsUpdated)
	if err != nil || counts[uint64(cls)] != 1 {
		t.Fatal("history source", counts, err)
	}
	if err := d.Synchronization.Register(ctx, fed, testReadyLabel, nil, []core.FederateHandle{a, b}); err != nil {
		t.Fatal(err)
	}
	if err := d.Synchronization.Achieve(ctx, fed, a, testReadyLabel); err != nil {
		t.Fatal(err)
	}
	points, err := s.SynchronizationPoints(ctx, fed)
	if err != nil || len(points) != 1 || len(points[0].Federates) != 2 || points[0].Federates[0].Status != 3 || points[0].Federates[1].Status != 2 {
		t.Fatalf("sync state: %+v %v", points, err)
	}
	modules, err := s.FOMModules(ctx, fed, 0)
	if err != nil || len(modules) != 1 || modules[0].Designator != "actual.xml" {
		t.Fatal("actual modules", modules, err)
	}
	if _, err := s.FOMModules(ctx, fed, b); err == nil {
		t.Fatal("global view substituted for join modules")
	}
	report, _ := fom.LookupInteractionClass(mom.ClassReportUpdatesSent)
	if err := d.Declarations.SubscribeInteractionClass(ctx, fed, b, report); err != nil {
		t.Fatal(err)
	}
	recipients, err := s.ReportRecipients(ctx, fed, a, mom.ClassReportUpdatesSent, "")
	if err != nil || !reflect.DeepEqual(recipients, []core.FederateHandle{b}) {
		t.Fatal("report routing", recipients, err)
	}
}

func TestMOMCatalogSourcesRejectUnavailableModuleBaseline(t *testing.T) {
	d, _, _, _, _ := newMOMCatalogDeps(t)
	sources, err := newMOMCatalogSources(d)
	if err != nil {
		t.Fatal(err)
	}
	d.FOMs.RememberFor("catalog-provider", composition.NewModelHandle(model.NewFOM(nil, nil, nil)))
	if _, err := sources.FOMModules(context.Background(), "catalog-provider", 0); err == nil {
		t.Fatal("source-less model reported a true empty module list")
	}
}

func TestMOMCatalogSubscriptionRegionalUnion(t *testing.T) {
	_, fom, _, target, _ := newMOMCatalogDeps(t)
	class, _ := fom.LookupObjectClass("Vehicle")
	attr, _ := fom.LookupAttribute(class, "value")
	privilege, _ := fom.LookupAttribute(class, "HLAprivilegeToDeleteObject")
	global := &declaration.ManagerState{ObjectOptions: []declaration.ObjectSubscriptionOptionsState{{Class: class, Attribute: attr, Federate: target, Options: core.SubscriptionOptions{Active: true, UpdateRateDesignator: "slow"}}}}
	regional := &ddm.ManagerState{ObjectSubscriptions: []ddm.ObjectSubscriptionState{{ObjectClass: class, Attribute: privilege, Subscriber: target, UpdateRateDesignator: "fast"}, {ObjectClass: class, Attribute: attr, Subscriber: target, Passive: true, UpdateRateDesignator: "slow"}}}
	got, err := momSubscriptions(global, regional, fom, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Objects) != 2 || got.Objects[0].Active || got.Objects[0].MaxUpdateRate != "slow" || !got.Objects[1].Active || got.Objects[1].MaxUpdateRate != "fast" || len(got.Objects[1].Attributes) != 2 {
		t.Fatalf("regional/global combinations: %+v", got)
	}
	regional.ObjectSubscriptions[0].UpdateRateDesignator = "unknown"
	if _, err := momSubscriptions(global, regional, fom, target); err == nil {
		t.Fatal("invented rate for missing definition")
	}
}
