package mom_test

import (
	"bytes"
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

const supportActorFed core.FederationName = "support-actor"
const supportActorRTI = ^core.FederateHandle(0)

type supportActorFixture struct {
	ctx        context.Context
	m          *mom.Manager
	d          *declaration.Manager
	o          *object.Registry
	own        *ownership.Manager
	dispatcher *mom.Dispatcher
	out        core.Outbox
	fom        core.FOMHandle
	queues     map[core.FederateHandle]<-chan []core.OutboundEvent
}

func supportOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newSupportActor(t *testing.T) *supportActorFixture {
	t.Helper()
	f := &supportActorFixture{ctx: context.Background(), queues: map[core.FederateHandle]<-chan []core.OutboundEvent{}}
	repo := composition.NewFOMRepository()
	xml := `<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>HLAmanager</name><objectClass><name>HLAfederate</name><objectClass><name>ParticipantFederate</name><attribute><name>participantValue</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objectClass></objectClass></objects><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>HLAmanager</name><interactionClass><name>HLAfederate</name><interactionClass><name>HLAreport</name><interactionClass><name>HLAreportException</name><interactionClass><name>ParticipantReport</name><parameter><name>childParameter</name><dataType>HLAinteger32BE</dataType></parameter></interactionClass></interactionClass></interactionClass></interactionClass></interactionClass></interactionClass></interactions></objectModel>`
	var err error
	f.fom, err = repo.Load(f.ctx, []core.FOMModule{{XML: []byte(xml)}})
	supportOK(t, err)
	repo.RememberFor(supportActorFed, f.fom)
	out := delivery.NewWithBatch(256, 1, 0)
	f.out = out
	for _, h := range []core.FederateHandle{1, 2, 3} {
		q, closeQueue, err := out.Subscribe(f.ctx, supportActorFed, h)
		supportOK(t, err)
		t.Cleanup(func() { _ = closeQueue() })
		f.queues[h] = q
	}
	f.d = declaration.New()
	f.d.SetFOMRepository(repo)
	f.own, err = ownership.New(ownership.Options{Outbox: out,
		PrepareAcquisition: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			return f.o.PrepareOwnershipAcquisition(ctx, fed, h, obj, attrs)
		},
		PrepareDivestiture: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			return f.o.PrepareOwnershipReleaseDenied(ctx, fed, h, obj, attrs)
		},
	})
	supportOK(t, err)
	f.m, err = mom.New(mom.Options{Outbox: out})
	supportOK(t, err)
	f.dispatcher = mom.NewDispatcher(f.m)
	f.o, err = object.New(object.Options{Clock: core.NewManualClock(time.Unix(0, 0)), FOMs: repo, Declarations: f.d, Outbox: out, Ownership: f.own, ManagementDispatch: f.dispatcher,
		OnRegister: func(fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			f.own.RegisterInitialOwnership(fed, h, obj, attrs)
		},
	})
	supportOK(t, err)
	f.m.EnableInstanceFanout(f.o, f.d, repo)
	f.d.SetOnSubscribeObjectClass(func(ctx context.Context, fed core.FederationName, h core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle) {
		f.o.ObjectClassSubscribed(ctx, fed, h, cls, attrs)
		f.m.ObjectClassSubscribed(ctx, fed, h, cls, attrs)
	})
	supportOK(t, f.m.FederationCreated(f.ctx, supportActorFed, nil))
	for _, h := range []core.FederateHandle{1, 2, 3} {
		supportOK(t, f.m.FederateJoined(f.ctx, supportActorFed, h, "subject", "test"))
	}
	return f
}

func (f *supportActorFixture) drain(h core.FederateHandle) []core.OutboundEvent {
	var result []core.OutboundEvent
	for {
		select {
		case batch := <-f.queues[h]:
			result = append(result, batch...)
		default:
			return result
		}
	}
}

func (f *supportActorFixture) class(t *testing.T, name string) core.ObjectClassHandle {
	t.Helper()
	h, ok := f.fom.LookupObjectClass(name)
	if !ok {
		t.Fatal(name)
	}
	return h
}
func (f *supportActorFixture) attr(t *testing.T, cls core.ObjectClassHandle, name string) core.AttributeHandle {
	t.Helper()
	h, ok := f.fom.LookupAttribute(cls, name)
	if !ok {
		t.Fatal(name)
	}
	return h
}

func TestApplicableSupportMOMExtensionOwnershipAndIsolation(t *testing.T) {
	f := newSupportActor(t)
	cls := f.class(t, mom.ClassHLAfederate)
	name := f.attr(t, cls, "HLAfederateName")
	supportOK(t, f.d.SubscribeObjectClassAttributes(f.ctx, supportActorFed, 1, cls, []core.AttributeHandle{name}))
	supportOK(t, f.d.SubscribeObjectClassAttributes(f.ctx, supportActorFed, 2, cls, []core.AttributeHandle{name}))
	obj := f.m.ExportState(supportActorFed).Federates[0].ObjectHandle
	if owner, owned := f.own.QueryOwnership(supportActorFed, obj, name); !owned || owner != supportActorRTI {
		t.Fatalf("predefined owner=%d/%t", owner, owned)
	}
	supportOK(t, f.d.PublishObjectClassAttributes(f.ctx, supportActorFed, 1, cls, []core.AttributeHandle{name}))
	supportOK(t, f.own.AcquireIfAvailable(f.ctx, supportActorFed, 1, obj, []core.AttributeHandle{name}, nil))
	if owner, _ := f.own.QueryOwnership(supportActorFed, obj, name); owner != supportActorRTI {
		t.Fatal("predefined ownership transferred")
	}
	subclass := f.class(t, "HLAmanager.HLAfederate.ParticipantFederate")
	inherited := f.attr(t, subclass, "HLAfederateName")
	extra := f.attr(t, subclass, "participantValue")
	supportOK(t, f.d.PublishObjectClassAttributes(f.ctx, supportActorFed, 1, subclass, []core.AttributeHandle{inherited, extra}))
	child, _, err := f.o.Register(f.ctx, supportActorFed, 1, subclass, "")
	supportOK(t, err)
	if child == obj {
		t.Fatal("extension instance reused RTI instance")
	}
	supportOK(t, f.d.SubscribeObjectClassAttributes(f.ctx, supportActorFed, 3, subclass, []core.AttributeHandle{inherited, extra}))
	f.drain(3)
	supportOK(t, f.o.UpdateAttributes(f.ctx, supportActorFed, 1, child, map[core.AttributeHandle][]byte{inherited: {0, 0, 0, 0}, extra: {0, 0, 0, 42}}, nil))
	found := false
	for _, event := range f.drain(3) {
		if e, ok := event.(*object.CallbackEvent); ok && e.Kind == object.CallbackReflect && e.Object == child {
			if value, present := e.Attributes[uint64(extra)]; present {
				if !bytes.Equal(value, []byte{0, 0, 0, 42}) {
					t.Fatal("extension payload changed")
				}
				found = true
			}
		}
	}
	if !found {
		t.Fatal("extension update not reflected")
	}
	f.drain(2)
	supportOK(t, f.m.RefreshMetrics(f.ctx, supportActorFed, 1))
	for _, event := range append(f.drain(2), f.drain(3)...) {
		switch e := event.(type) {
		case *object.CallbackEvent:
			if e.Kind == object.CallbackReflect && e.Object == child {
				t.Fatal("RTI updated participant subclass")
			}
		case *mom.ReflectEvent:
			if e.Object == child {
				t.Fatal("RTI updated participant subclass")
			}
		}
	}
	if owner, _ := f.own.QueryOwnership(supportActorFed, child, extra); owner != 1 {
		t.Fatal("RTI acquired participant extension")
	}
	state := f.d.ExportState(supportActorFed)
	for _, row := range state.ObjectSubscriptions {
		if slices.Contains(row.Federates, supportActorRTI) {
			t.Fatal("RTI subscribed to object class")
		}
	}
	for _, row := range state.ObjectPublications {
		if slices.Contains(row.Federates, supportActorRTI) && row.Class == subclass {
			t.Fatal("RTI published extension")
		}
	}
}

func TestApplicableSupportMOMPredefinedPublicationSet(t *testing.T) {
	f := newSupportActor(t)
	wantNames := map[string][]string{
		mom.ClassHLAfederate:   strings.Fields("HLAfederateHandle HLAfederateName HLAfederateType HLAfederateHost HLARTIversion HLAFOMmoduleDesignatorList HLAtimeConstrained HLAtimeRegulating HLAasynchronousDelivery HLAfederateState HLAtimeManagerState HLAlogicalTime HLAlookahead HLAGALT HLALITS HLAROlength HLATSOlength HLAreflectionsReceived HLAupdatesSent HLAinteractionsReceived HLAinteractionsSent HLAobjectInstancesThatCanBeDeleted HLAobjectInstancesUpdated HLAobjectInstancesReflected HLAobjectInstancesDeleted HLAobjectInstancesRemoved HLAobjectInstancesRegistered HLAobjectInstancesDiscovered HLAtimeGrantedTime HLAtimeAdvancingTime HLAconveyRegionDesignatorSets HLAconveyProducingFederate"),
		mom.ClassHLAfederation: strings.Fields("HLAfederationName HLAfederatesInFederation HLARTIversion HLAMIMdesignator HLAFOMmoduleDesignatorList HLAcurrentFDD HLAtimeImplementationName HLAlastSaveName HLAlastSaveTime HLAAnnexSaveName HLAnextSaveTime HLAautoProvide"),
	}
	for className, names := range wantNames {
		cls := f.class(t, className)
		want := []core.AttributeHandle{f.attr(t, cls, "HLAprivilegeToDeleteObject")}
		for _, name := range names {
			want = append(want, f.attr(t, cls, name))
		}
		slices.Sort(want)
		got := f.d.PublishedObjectAttributes(supportActorFed, supportActorRTI, cls)
		slices.Sort(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("predefined publications for %s: got %v want %v", className, got, want)
		}
	}
}

func TestApplicableSupportMOMRequestRefreshBothForms(t *testing.T) {
	for _, instance := range []bool{false, true} {
		name := "class"
		if instance {
			name = "instance"
		}
		t.Run(name, func(t *testing.T) {
			f := newSupportActor(t)
			cls := f.class(t, mom.ClassHLAfederate)
			attr := f.attr(t, cls, "HLAconveyProducingFederate")
			supportOK(t, f.d.SubscribeObjectClassAttributes(f.ctx, supportActorFed, 2, cls, []core.AttributeHandle{attr}))
			obj := f.m.ExportState(supportActorFed).Federates[0].ObjectHandle
			f.drain(2)
			for _, h := range []core.FederateHandle{1, 2, 3} {
				f.m.SetConveyProducingFederateSwitch(supportActorFed, h, true)
			}
			var err error
			if instance {
				err = f.o.RequestAttributeValueUpdate(f.ctx, supportActorFed, 2, obj, []core.AttributeHandle{attr}, []byte("request"))
			} else {
				err = f.o.RequestClassAttributeValueUpdate(f.ctx, supportActorFed, 2, cls, []core.AttributeHandle{attr}, []byte("request"))
			}
			supportOK(t, err)
			count := 0
			for _, event := range f.drain(2) {
				if e, ok := event.(*object.CallbackEvent); ok && e.Kind == object.CallbackReflect {
					if len(e.Attributes) != 1 || !bytes.Equal(e.Attributes[uint64(attr)], []byte{0, 0, 0, 1}) {
						t.Fatal("requested fresh value differs")
					}
					count++
				}
				if e, ok := event.(*mom.ReflectEvent); ok {
					if len(e.Attributes) != 1 || !bytes.Equal(e.Attributes[attr], []byte{0, 0, 0, 1}) {
						t.Fatal("requested fresh value differs")
					}
					count++
				}
			}
			want := 3
			if instance {
				want = 1
			}
			if count != want {
				t.Fatalf("request reflections=%d want %d", count, want)
			}
		})
	}
}

func TestApplicableSupportMOMReportExtensionPromotion(t *testing.T) {
	f := newSupportActor(t)
	leaf, ok := f.fom.LookupInteractionClass(mom.ClassReportException)
	if !ok {
		t.Fatal("report leaf missing")
	}
	child, ok := f.fom.LookupInteractionClass(mom.ClassReportException + ".ParticipantReport")
	if !ok {
		t.Fatal("report extension missing")
	}
	supportOK(t, f.d.PublishInteractionClass(f.ctx, supportActorFed, 1, child))
	supportOK(t, f.d.SubscribeInteractionClass(f.ctx, supportActorFed, 2, child))
	supportOK(t, f.d.SubscribeInteractionClass(f.ctx, supportActorFed, 3, leaf))
	values := map[string][]byte{"HLAfederate": {0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 1}, "HLAservice": {0, 0, 0, 0}, "HLAexception": {0, 0, 0, 0}, "childParameter": {0, 0, 0, 42}}
	params := map[core.ParameterHandle][]byte{}
	for name, value := range values {
		h, ok := f.fom.LookupParameter(child, name)
		if !ok {
			t.Fatal(name)
		}
		params[h] = value
	}
	f.drain(2)
	f.drain(3)
	supportOK(t, f.o.SendInteraction(f.ctx, supportActorFed, 1, child, params, nil))
	for _, recipient := range []core.FederateHandle{2, 3} {
		wantClass, wantCount := child, 4
		if recipient == 3 {
			wantClass, wantCount = leaf, 3
		}
		count := 0
		for _, event := range f.drain(recipient) {
			if e, ok := event.(*object.CallbackEvent); ok && e.Kind == object.CallbackReceive {
				count++
				if e.InteractionClass != wantClass || len(e.Parameters) != wantCount {
					t.Fatalf("recipient %d class/parameter promotion: %+v", recipient, e)
				}
				for name, value := range values {
					if recipient == 3 && name == "childParameter" {
						continue
					}
					h, _ := f.fom.LookupParameter(wantClass, name)
					if !bytes.Equal(e.Parameters[uint64(h)], value) {
						t.Fatalf("recipient %d value %s", recipient, name)
					}
				}
			}
		}
		if count != 1 {
			t.Fatalf("recipient %d callback count=%d", recipient, count)
		}
	}
	f.dispatcher.SetEmitter(mom.NewProductionEmitter(f.out))
	f.dispatcher.SetCatalogSources(mom.CatalogSources{ReportRecipients: func(_ context.Context, fed core.FederationName, subject core.FederateHandle, class, _ string) ([]core.FederateHandle, error) {
		if fed != supportActorFed || subject != 1 || class != mom.ClassReportException {
			t.Fatal("RTI reported extension or wrong subject")
		}
		return []core.FederateHandle{3}, nil
	}})
	f.m.SetExceptionReporting(supportActorFed, 1, true)
	supportOK(t, f.dispatcher.ReportServiceInvocation(f.ctx, supportActorFed, 1, mom.ServiceInvocation{Service: "RegisterObjectInstance", Success: false, Exception: "ObjectClassNotPublished"}, f.fom, f.fom.(core.FOMHandleNameLookup)))
	count := 0
	for _, event := range f.drain(3) {
		if e, ok := event.(*mom.InteractionEvent); ok {
			count++
			if e.Class != leaf || len(e.Parameters) != 3 {
				t.Fatal("RTI emitted subclass or extension parameter")
			}
			for _, name := range []string{"HLAfederate", "HLAservice", "HLAexception"} {
				h, _ := f.fom.LookupParameter(leaf, name)
				if _, exists := e.Parameters[h]; !exists {
					t.Fatal(name)
				}
			}
		}
	}
	if count != 1 {
		t.Fatalf("RTI report count=%d", count)
	}
}

func TestApplicableSupportMOMActorPreservesTransportAndOrder(t *testing.T) {
	f := newSupportActor(t)
	cls := f.class(t, mom.ClassHLAfederate)
	attr := f.attr(t, cls, "HLAfederateName")
	supportOK(t, f.d.SubscribeObjectClassAttributes(f.ctx, supportActorFed, 2, cls, []core.AttributeHandle{attr}))
	f.drain(2)
	before := f.o.ExportState(supportActorFed)
	supportOK(t, f.m.RefreshMetrics(f.ctx, supportActorFed, 1))
	reflections := 0
	for _, event := range f.drain(2) {
		if e, ok := event.(*object.CallbackEvent); ok && e.Kind == object.CallbackReflect {
			reflections++
			if e.Metadata == nil || e.Metadata.Transportation != core.TransportTypeReliable || e.Metadata.SentOrder != core.OrderReceive || e.LogicalTime != nil || len(e.UserSuppliedTag) != 0 {
				t.Fatalf("RTI MOM metadata: %+v", e)
			}
		}
	}
	if reflections == 0 {
		t.Fatal("no actual MOM reflection observed")
	}
	after := f.o.ExportState(supportActorFed)
	if !reflect.DeepEqual(before.AttributeTransports, after.AttributeTransports) || !reflect.DeepEqual(before.InteractionTransports, after.InteractionTransports) || !reflect.DeepEqual(before.AttributeOrders, after.AttributeOrders) || !reflect.DeepEqual(before.InteractionOrders, after.InteractionOrders) {
		t.Fatal("RTI changed MOM transport/order overrides")
	}
}
