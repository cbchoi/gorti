package object_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

const knownFed core.FederationName = "known-class"

type knownAssociationValidator struct{ registry **object.Registry }

func (v knownAssociationValidator) PrepareRegionAssociation(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, func(), error) {
	return (*v.registry).PrepareRegionAssociation(ctx, fed, caller, obj, attrs)
}

type knownDDM struct{ *ddm.Manager }

func (d knownDDM) PublisherRegionsFor(f core.FederationName, o core.ObjectHandle, a core.AttributeHandle) []object.DDMRegionHandle {
	var result []object.DDMRegionHandle
	for _, region := range d.Manager.PublisherRegionsFor(f, o, a) {
		result = append(result, object.DDMRegionHandle(region))
	}
	return result
}
func (d knownDDM) SubscribersForUpdate(f core.FederationName, c core.ObjectClassHandle, a core.AttributeHandle, regions []object.DDMRegionHandle) []core.FederateHandle {
	var mapped []ddm.RegionHandle
	for _, region := range regions {
		mapped = append(mapped, ddm.RegionHandle(region))
	}
	return d.Manager.SubscribersForUpdate(f, c, a, mapped)
}

type knownFixture struct {
	ctx    context.Context
	reg    *object.Registry
	decl   *declaration.Manager
	ddm    *ddm.Manager
	owners *ownership.Manager
	repo   interface {
		core.FOMRepository
		RememberFor(core.FederationName, core.FOMHandle)
	}
	fom                  core.FOMHandle
	parent, child        core.ObjectClassHandle
	base, inherited, own core.AttributeHandle
	dimension            ddm.DimensionHandle
	queues               map[core.FederateHandle]<-chan []core.OutboundEvent
}

func knownOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newKnownFixture(t *testing.T) *knownFixture {
	t.Helper()
	f := &knownFixture{ctx: context.Background(), repo: composition.NewFOMRepository(), queues: map[core.FederateHandle]<-chan []core.OutboundEvent{}}
	xml := []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Base</name><attribute><name>position</name><dataType>HLAinteger32BE</dataType><dimensions><dimension>X</dimension></dimensions></attribute><objectClass><name>Child</name><attribute><name>childValue</name><dataType>HLAinteger32BE</dataType><dimensions><dimension>X</dimension></dimensions></attribute></objectClass></objectClass></objectClass></objects><dimensions><dimension><name>X</name><dataType>HLAinteger32BE</dataType><upperBound>100</upperBound></dimension></dimensions></objectModel>`)
	var err error
	f.fom, err = f.repo.Load(f.ctx, []core.FOMModule{{Path: "known.xml", XML: xml}})
	knownOK(t, err)
	f.repo.RememberFor(knownFed, f.fom)
	f.parent, _ = f.fom.LookupObjectClass("Base")
	f.child, _ = f.fom.LookupObjectClass("Child")
	f.base, _ = f.fom.LookupAttribute(f.parent, "position")
	f.inherited, _ = f.fom.LookupAttribute(f.child, "position")
	f.own, _ = f.fom.LookupAttribute(f.child, "childValue")
	if f.base == f.inherited {
		t.Fatal("fixture must exercise differing inherited attribute handles")
	}
	out := delivery.NewWithBatch(256, 1, 0)
	for _, federate := range []core.FederateHandle{1, 2, 3, 4} {
		queue, closeQueue, err := out.Subscribe(f.ctx, knownFed, federate)
		knownOK(t, err)
		t.Cleanup(func() { _ = closeQueue() })
		f.queues[federate] = queue
	}
	f.decl = declaration.New()
	f.decl.SetFOMRepository(f.repo)
	f.owners, err = ownership.New(ownership.Options{Outbox: out,
		PrepareAcquisition: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			return f.reg.PrepareOwnershipAcquisition(ctx, fed, h, obj, attrs)
		},
		PrepareDivestiture: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			return f.reg.PrepareOwnershipReleaseDenied(ctx, fed, h, obj, attrs)
		}})
	knownOK(t, err)
	f.ddm, err = ddm.New(ddm.Options{Outbox: out, FOMs: f.repo, Objects: knownAssociationValidator{&f.reg},
		OnObjectSubscriptionInterest: func(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) error {
			f.reg.ObjectClassSubscribed(ctx, fed, sub, cls, attrs)
			return nil
		}})
	knownOK(t, err)
	f.reg, err = object.New(object.Options{Clock: core.NewManualClock(time.Unix(0, 0)), FOMs: f.repo, Declarations: f.decl, Outbox: out,
		DDM: knownDDM{f.ddm}, RegistrationRegions: f.ddm, Ownership: f.owners,
		OnRegister: func(fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			f.owners.RegisterInitialOwnership(fed, h, obj, attrs)
		}})
	knownOK(t, err)
	f.decl.SetOnSubscribeObjectClass(f.reg.ObjectClassSubscribed)
	knownOK(t, f.decl.PublishObjectClassAttributes(f.ctx, knownFed, 1, f.child, []core.AttributeHandle{f.inherited, f.own}))
	space, _ := f.ddm.LookupRoutingSpace(knownFed, ddm.DefaultRoutingSpace)
	f.dimension, _ = f.ddm.LookupDimension(knownFed, space, "X")
	return f
}

func (f *knownFixture) subscribe(t *testing.T, sub core.FederateHandle, cls core.ObjectClassHandle, attrs ...core.AttributeHandle) {
	t.Helper()
	knownOK(t, f.decl.SubscribeObjectClassAttributes(f.ctx, knownFed, sub, cls, attrs))
}
func (f *knownFixture) register(t *testing.T) core.ObjectHandle {
	t.Helper()
	obj, _, err := f.reg.Register(f.ctx, knownFed, 1, f.child, "")
	knownOK(t, err)
	return obj
}
func (f *knownFixture) drain(sub core.FederateHandle) []*object.CallbackEvent {
	var events []*object.CallbackEvent
	for {
		select {
		case batch := <-f.queues[sub]:
			for _, event := range batch {
				if callback, ok := event.(*object.CallbackEvent); ok {
					events = append(events, callback)
				}
			}
		default:
			return events
		}
	}
}
func (f *knownFixture) assertClass(t *testing.T, sub core.FederateHandle, obj core.ObjectHandle, want core.ObjectClassHandle) {
	t.Helper()
	_, cls, known := f.reg.LookupKnownObjectInstance(knownFed, sub, obj)
	if !known || cls != want {
		t.Fatalf("recipient %d known class=%d known=%t, want %d", sub, cls, known, want)
	}
}

func TestKnownClassAncestorDiscoveryAndStableRediscovery(t *testing.T) {
	f := newKnownFixture(t)
	f.subscribe(t, 2, f.parent, f.base)
	f.subscribe(t, 3, f.parent, f.base)
	f.subscribe(t, 3, f.child, f.own)
	obj := f.register(t)
	f.assertClass(t, 1, obj, f.child)
	f.assertClass(t, 2, obj, f.parent)
	f.assertClass(t, 3, obj, f.child)
	for sub, want := range map[core.FederateHandle]core.ObjectClassHandle{2: f.parent, 3: f.child} {
		events := f.drain(sub)
		if len(events) != 1 || events[0].Kind != object.CallbackDiscover || events[0].ObjectClass != want {
			t.Fatalf("recipient %d discovery=%+v, want class %d", sub, events, want)
		}
	}
	f.subscribe(t, 2, f.child, f.own, f.inherited)
	knownOK(t, f.decl.UnsubscribeObjectClassAttributes(f.ctx, knownFed, 2, f.parent, []core.AttributeHandle{f.base}))
	f.assertClass(t, 2, obj, f.parent)
	if events := f.drain(2); len(events) != 0 {
		t.Fatalf("subscription change rediscovered known object: %+v", events)
	}
	knownOK(t, f.reg.LocalDelete(f.ctx, knownFed, 2, obj))
	f.assertClass(t, 2, obj, f.child)
	events := f.drain(2)
	if len(events) != 1 || events[0].ObjectClass != f.child {
		t.Fatalf("local-delete rediscovery=%+v", events)
	}
}

func TestKnownClassReflectionUsesRememberedSubscriptionAndHandles(t *testing.T) {
	f := newKnownFixture(t)
	f.subscribe(t, 2, f.parent, f.base)
	obj := f.register(t)
	f.assertClass(t, 2, obj, f.parent)
	f.drain(2)
	knownOK(t, f.reg.UpdateAttributes(f.ctx, knownFed, 1, obj, map[core.AttributeHandle][]byte{f.inherited: {7}, f.own: {9}}, nil))
	reflected := 0
	for _, event := range f.drain(2) {
		if event.Kind == object.CallbackReflect {
			reflected++
			if event.ObjectClass != f.parent || len(event.Attributes) != 1 || len(event.Attributes[uint64(f.base)]) != 1 || event.Attributes[uint64(f.base)][0] != 7 {
				t.Fatalf("ancestor reflection=%+v", event)
			}
		}
	}
	if reflected != 1 {
		t.Fatalf("reflections=%d", reflected)
	}
	f.subscribe(t, 2, f.child, f.own, f.inherited)
	knownOK(t, f.decl.UnsubscribeObjectClassAttributes(f.ctx, knownFed, 2, f.parent, []core.AttributeHandle{f.base}))
	f.drain(2)
	knownOK(t, f.reg.UpdateAttributes(f.ctx, knownFed, 1, obj, map[core.AttributeHandle][]byte{f.inherited: {8}, f.own: {10}}, nil))
	for _, event := range f.drain(2) {
		if event.Kind == object.CallbackReflect {
			t.Fatal("new child subscription changed the remembered parent routing class")
		}
	}
}

func (f *knownFixture) region(t *testing.T, owner core.FederateHandle, lower, upper uint64) ddm.RegionHandle {
	t.Helper()
	space, _ := f.ddm.LookupRoutingSpace(knownFed, ddm.DefaultRoutingSpace)
	region, err := f.ddm.CreateRegion(f.ctx, knownFed, owner, space, []ddm.DimensionHandle{f.dimension})
	knownOK(t, err)
	knownOK(t, f.ddm.SetRangeBounds(knownFed, owner, region, f.dimension, ddm.Range{Lower: lower, Upper: upper}))
	knownOK(t, f.ddm.CommitRegionModifications(f.ctx, knownFed, owner, []ddm.RegionHandle{region}))
	return region
}

func TestKnownClassDDMCandidateDoesNotFallBackOnNonoverlap(t *testing.T) {
	f := newKnownFixture(t)
	f.subscribe(t, 2, f.parent, f.base)
	pub, outside, inside := f.region(t, 1, 0, 10), f.region(t, 2, 50, 60), f.region(t, 3, 0, 10)
	knownOK(t, f.ddm.SubscribeObjectClassAttributesWithRegions(f.ctx, knownFed, 2, f.child, []core.AttributeHandle{f.inherited}, []ddm.RegionHandle{outside}))
	knownOK(t, f.ddm.SubscribeObjectClassAttributesWithRegions(f.ctx, knownFed, 3, f.parent, []core.AttributeHandle{f.base}, []ddm.RegionHandle{inside}))
	obj, _, err := f.reg.RegisterWithRegions(f.ctx, knownFed, 1, f.child, "", map[core.AttributeHandle][]core.DDMRegionHandleCore{f.inherited: {pub}})
	knownOK(t, err)
	if _, _, known := f.reg.LookupKnownObjectInstance(knownFed, 2, obj); known || len(f.drain(2)) != 0 {
		t.Fatal("non-overlapping nearest subscription fell back to an ancestor")
	}
	f.assertClass(t, 3, obj, f.parent)
	events := f.drain(3)
	if len(events) != 1 || events[0].Kind != object.CallbackDiscover || events[0].ObjectClass != f.parent {
		t.Fatalf("regional ancestor discovery=%+v", events)
	}
	knownOK(t, f.ddm.UnsubscribeObjectClassAttributesWithRegions(f.ctx, knownFed, 2, f.child, []core.AttributeHandle{f.inherited}, []ddm.RegionHandle{outside}))
	f.assertClass(t, 2, obj, f.parent)
	knownOK(t, f.reg.UpdateAttributes(f.ctx, knownFed, 1, obj, map[core.AttributeHandle][]byte{f.inherited: {11}}, nil))
	found := false
	for _, event := range f.drain(3) {
		if event.Kind == object.CallbackReflect {
			found = true
			if event.ObjectClass != f.parent || len(event.Attributes) != 1 || event.Attributes[uint64(f.base)][0] != 11 {
				t.Fatalf("regional ancestor reflection=%+v", event)
			}
		}
	}
	if !found {
		t.Fatal("regional ancestor did not receive update")
	}
}

func TestKnownClassSnapshotPreservesHistoryAndLegacyActualClass(t *testing.T) {
	f := newKnownFixture(t)
	f.subscribe(t, 2, f.parent, f.base)
	obj := f.register(t)
	snapshot := f.reg.ExportState(knownFed)
	f.subscribe(t, 2, f.child, f.own)
	knownOK(t, f.decl.UnsubscribeObjectClassAttributes(f.ctx, knownFed, 2, f.parent, []core.AttributeHandle{f.base}))
	commit, err := f.reg.PrepareImportState(knownFed, snapshot)
	knownOK(t, err)
	commit()
	f.assertClass(t, 2, obj, f.parent)
	if !reflect.DeepEqual(snapshot, f.reg.ExportState(knownFed)) {
		t.Fatal("known-class snapshot did not round trip")
	}
	for i := range snapshot.Discoveries {
		snapshot.Discoveries[i].KnownClass = 0
	}
	commit, err = f.reg.PrepareImportState(knownFed, snapshot)
	knownOK(t, err)
	commit()
	f.assertClass(t, 2, obj, f.child)
}

func TestKnownClassOwnershipQueryAndPublicationUseKnownCoordinates(t *testing.T) {
	f := newKnownFixture(t)
	f.subscribe(t, 2, f.parent, f.base)
	obj := f.register(t)
	f.owners.RegisterInitialOwnership(knownFed, 3, obj, []core.AttributeHandle{f.own})
	owner, owned, err := f.reg.QueryAttributeOwnership(f.ctx, knownFed, 2, obj, f.base)
	if err != nil || !owned || owner != 1 {
		t.Errorf("ancestor ownership query owner=%d owned=%t error=%v", owner, owned, err)
	}
	if _, _, err := f.reg.QueryAttributeOwnership(f.ctx, knownFed, 2, obj, 999); !errors.Is(err, core.ErrAttributeNotFound) {
		t.Errorf("undefined known-class attribute: %v", err)
	}
	knownOK(t, f.decl.PublishObjectClassAttributes(f.ctx, knownFed, 2, f.parent, []core.AttributeHandle{f.base}))
	// The ownership manager's preparation hook uses registered coordinates.
	release, err := f.reg.PrepareOwnershipAcquisition(f.ctx, knownFed, 2, obj, []core.AttributeHandle{f.inherited})
	if err != nil {
		t.Fatalf("publication at remembered ancestor rejected: %v", err)
	}
	release()
}

func TestKnownClassAcquiredOwnerAttributeServices(t *testing.T) {
	f := newKnownFixture(t)
	f.subscribe(t, 2, f.parent, f.base)
	f.subscribe(t, 3, f.child, f.inherited, f.own)
	obj := f.register(t)
	knownOK(t, f.decl.PublishObjectClassAttributes(f.ctx, knownFed, 2, f.parent, []core.AttributeHandle{f.base}))
	input := []core.AttributeHandle{f.base}
	mapped, release, err := f.reg.PrepareKnownOwnershipAcquisition(f.ctx, knownFed, 2, obj, input)
	knownOK(t, err)
	if !reflect.DeepEqual(mapped, []core.AttributeHandle{f.inherited}) || input[0] != f.base {
		t.Fatalf("acquisition mapping=%v input=%v", mapped, input)
	}
	release()
	knownOK(t, f.owners.Acquire(f.ctx, knownFed, 2, obj, mapped, nil))
	_, err = f.owners.DivestIfWantedWithResult(f.ctx, knownFed, 1, obj, mapped)
	knownOK(t, err)
	if !f.owners.IsOwnedBy(knownFed, 2, obj, f.inherited) || !f.owners.IsOwnedBy(knownFed, 1, obj, f.own) {
		t.Fatal("acquisition changed the wrong canonical attribute")
	}
	f.drain(3)
	for name, update := range map[string]func() error{
		"typed": func() error {
			return f.reg.UpdateAttributes(f.ctx, knownFed, 2, obj, map[core.AttributeHandle][]byte{f.base: {42}}, nil)
		},
		"wire": func() error {
			return f.reg.UpdateAttributesWire(f.ctx, knownFed, 2, obj, map[uint64][]byte{uint64(f.base): {42}}, nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			knownOK(t, update())
			found := false
			for _, event := range f.drain(3) {
				if event.Kind == object.CallbackReflect {
					found = true
					if event.ObjectClass != f.child || len(event.Attributes) != 1 || !reflect.DeepEqual(event.Attributes[uint64(f.inherited)], []byte{42}) {
						t.Fatalf("acquired-owner update reflected wrong attribute: %+v", event)
					}
				}
			}
			if !found {
				t.Fatal("no acquired-owner reflection")
			}
		})
	}
	t.Run("transport", func(t *testing.T) {
		knownOK(t, f.reg.RequestAttributeTransportationTypeChange(f.ctx, knownFed, 2, obj, input, core.TransportTypeBestEffort))
		if f.reg.AttributeTransportType(knownFed, obj, f.inherited) != core.TransportTypeBestEffort || f.reg.AttributeTransportType(knownFed, obj, f.own) != core.TransportTypeUnspecified {
			t.Fatal("transport override applied in caller coordinates")
		}
		tt, err := f.reg.QueryAttributeTransportationType(f.ctx, knownFed, 2, obj, f.base)
		knownOK(t, err)
		if tt != core.TransportTypeBestEffort {
			t.Fatalf("queried transport=%v", tt)
		}
		scope := core.TransportationScope{Member: core.MemberIdentity{Federation: knownFed, Handle: 2, Incarnation: 1}, CallbackEpoch: 1}
		change, err := f.reg.PrepareAttributeTransportationChange(f.ctx, scope, obj, input, core.TransportTypeReliable)
		knownOK(t, err)
		confirmed, err := f.reg.ActivateTransportationChange(f.ctx, scope, change.Ticket)
		knownOK(t, err)
		if !reflect.DeepEqual(confirmed.Attributes, input) || f.reg.AttributeTransportType(knownFed, obj, f.inherited) != core.TransportTypeReliable || f.reg.AttributeTransportType(knownFed, obj, f.own) != core.TransportTypeUnspecified {
			t.Fatalf("transport confirmation=%+v", confirmed)
		}
	})
	t.Run("order", func(t *testing.T) {
		knownOK(t, f.reg.ChangeAttributeOrderType(f.ctx, knownFed, 2, obj, input, core.OrderReceive))
		snapshot := f.reg.ExportState(knownFed)
		if len(snapshot.AttributeOrders) != 1 || snapshot.AttributeOrders[0].Attribute != f.inherited {
			t.Fatalf("order state=%+v", snapshot.AttributeOrders)
		}
	})
	t.Run("request update", func(t *testing.T) {
		f.drain(1)
		f.drain(2)
		knownOK(t, f.reg.RequestAttributeValueUpdate(f.ctx, knownFed, 2, obj, input, []byte("known")))
		found := false
		for _, event := range f.drain(2) {
			if event.Kind == object.CallbackProvideUpdate {
				found = true
				if !reflect.DeepEqual(event.AttributeHandles, input) {
					t.Fatalf("owner callback=%+v", event)
				}
			}
		}
		if !found {
			t.Fatal("request did not reach canonical owner")
		}
		for _, event := range f.drain(1) {
			if event.Kind == object.CallbackProvideUpdate {
				t.Fatal("request reached wrong-attribute owner")
			}
		}
	})
	t.Run("association fail closed", func(t *testing.T) {
		region := f.region(t, 2, 0, 10)
		before := f.ddm.ExportState(knownFed)
		err := f.ddm.AssociateRegionsForUpdates(f.ctx, knownFed, 2, obj, map[core.AttributeHandle][]ddm.RegionHandle{f.base: {region}})
		if !errors.Is(err, core.ErrInvalidRegionContext) {
			t.Fatalf("unmapped association error=%v", err)
		}
		if !reflect.DeepEqual(before, f.ddm.ExportState(knownFed)) {
			t.Fatal("rejected ancestor association changed DDM state")
		}
	})
}

func TestKnownClassMappingRejectsInvisibleAttributesAtomically(t *testing.T) {
	f := newKnownFixture(t)
	f.subscribe(t, 2, f.parent, f.base)
	obj := f.register(t)
	attrs := []core.AttributeHandle{f.inherited, f.own}
	if mapped, err := f.reg.MapRegisteredAttributesToKnown(f.ctx, knownFed, 2, obj, attrs); !errors.Is(err, core.ErrAttributeNotFound) || mapped != nil {
		t.Fatalf("hidden child attribute projection=%v error=%v", mapped, err)
	}
	if mapped, err := f.reg.MapKnownAttributesToRegistered(f.ctx, knownFed, 2, obj, []core.AttributeHandle{f.base, 999}); !errors.Is(err, core.ErrAttributeNotFound) || mapped != nil {
		t.Fatalf("invalid later known attribute mapping=%v error=%v", mapped, err)
	}
	if attrs[0] != f.inherited || attrs[1] != f.own {
		t.Fatal("mapping changed caller input")
	}
	if _, err := f.reg.MapKnownAttributesToRegistered(f.ctx, knownFed, 4, obj, []core.AttributeHandle{f.base}); !errors.Is(err, core.ErrObjectHandleInvalid) {
		t.Fatalf("unknown recipient error=%v", err)
	}
}

func TestKnownClassCommittedRegionRefreshDiscoversOnce(t *testing.T) {
	f := newKnownFixture(t)
	pub, sub := f.region(t, 1, 0, 10), f.region(t, 2, 20, 30)
	knownOK(t, f.ddm.SubscribeObjectClassAttributesWithRegions(f.ctx, knownFed, 2, f.parent, []core.AttributeHandle{f.base}, []ddm.RegionHandle{sub}))
	obj, _, err := f.reg.RegisterWithRegions(f.ctx, knownFed, 1, f.child, "", map[core.AttributeHandle][]core.DDMRegionHandleCore{f.inherited: {pub}})
	knownOK(t, err)
	f.reg.RefreshDiscoveries(f.ctx, knownFed)
	if _, _, known := f.reg.LookupKnownObjectInstance(knownFed, 2, obj); known {
		t.Fatal("nonoverlapping object was discovered")
	}
	knownOK(t, f.ddm.SetRangeBounds(knownFed, 2, sub, f.dimension, ddm.Range{Lower: 5, Upper: 15}))
	f.reg.RefreshDiscoveries(f.ctx, knownFed)
	if _, _, known := f.reg.LookupKnownObjectInstance(knownFed, 2, obj); known {
		t.Fatal("pending region bounds changed discovery")
	}
	knownOK(t, f.ddm.CommitRegionModifications(f.ctx, knownFed, 2, []ddm.RegionHandle{sub}))
	f.reg.RefreshDiscoveries(f.ctx, knownFed)
	f.assertClass(t, 2, obj, f.parent)
	events := f.drain(2)
	if len(events) != 1 || events[0].Kind != object.CallbackDiscover || events[0].ObjectClass != f.parent {
		t.Fatalf("overlap discovery=%+v", events)
	}
	knownOK(t, f.ddm.SetRangeBounds(knownFed, 2, sub, f.dimension, ddm.Range{Lower: 20, Upper: 30}))
	knownOK(t, f.ddm.CommitRegionModifications(f.ctx, knownFed, 2, []ddm.RegionHandle{sub}))
	f.reg.RefreshDiscoveries(f.ctx, knownFed)
	f.assertClass(t, 2, obj, f.parent)
	if len(f.drain(2)) != 0 {
		t.Fatal("refresh duplicated historical discovery")
	}
}

func TestKnownClassNoDiscoveryForUnownedAttribute(t *testing.T) {
	f := newKnownFixture(t)
	obj := f.register(t)
	knownOK(t, f.owners.UnconditionalDivest(f.ctx, knownFed, 1, obj, []core.AttributeHandle{f.inherited}))
	f.subscribe(t, 2, f.parent, f.base)
	f.reg.RefreshDiscoveries(f.ctx, knownFed)
	if _, _, known := f.reg.LookupKnownObjectInstance(knownFed, 2, obj); known {
		t.Fatal("unowned subscribed attribute caused discovery")
	}
}

func TestKnownClassRemovalRetainsRecipientClass(t *testing.T) {
	f := newKnownFixture(t)
	f.subscribe(t, 2, f.parent, f.base)
	obj := f.register(t)
	f.drain(2)
	knownOK(t, f.reg.Delete(f.ctx, knownFed, 1, obj, nil, nil))
	events := f.drain(2)
	if len(events) != 1 || events[0].Kind != object.CallbackRemove || events[0].ObjectClass != f.parent {
		t.Fatalf("known-class removal=%+v", events)
	}
}
