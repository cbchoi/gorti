package object_test

import (
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

const delayedSubscriptionReviewXML = `<objectModel>
<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Item</name>
<attribute><name>first</name><order>TimeStamp</order></attribute>
<attribute><name>second</name><order>TimeStamp</order></attribute>
</objectClass></objectClass></objects>
<interactions><interactionClass><name>HLAinteractionRoot</name>
<interactionClass><name>Event</name><order>TimeStamp</order>
<parameter><name>payload</name></parameter></interactionClass>
</interactionClass></interactions>
<switches><delaySubscriptionEvaluation isEnabled="true"/></switches>
</objectModel>`

type delayedSubscriptionFixtureOf[T core.LogicalTimeScalar] struct {
	objects            *object.RegistryOf[T]
	times              *timepkg.ManagerOf[T]
	declarations       *declaration.Manager
	out                *regionDeliveryOutbox
	producer, receiver core.FederateHandle
	class              core.ObjectClassHandle
	interaction        core.InteractionClassHandle
	parameter          core.ParameterHandle
	first, second      core.AttributeHandle
	fom                core.FOMHandle
	regions            *ddm.Manager
}

type delayedSubscriptionReviewFixture = delayedSubscriptionFixtureOf[core.LogicalTime]

func newDelayedSubscriptionReviewFixture(t *testing.T) delayedSubscriptionReviewFixture {
	return newDelayedSubscriptionFixtureOf[core.LogicalTime](t, delayedSubscriptionReviewXML)
}

func newDelayedSubscriptionFixtureOf[T core.LogicalTimeScalar](t *testing.T, xml string) delayedSubscriptionFixtureOf[T] {
	t.Helper()
	ctx := context.Background()
	parsed, err := parser.Parse([]parser.Module{{Path: "delay.xml", XML: []byte(xml)}})
	if err != nil || len(parsed.Diagnostics) != 0 {
		t.Fatal("invalid source-oracle FOM", err, parsed.Diagnostics)
	}
	enabled := false
	for _, setting := range parsed.FOM.(*model.FOM).AdvisorySwitches() {
		if setting.Name == "delaySubscriptionEvaluation" {
			enabled = setting.Enabled
		}
	}
	_ = enabled
	repo := composition.NewFOMRepository()
	modules := []core.FOMModule{{Path: "delay.xml", XML: []byte(xml)}}
	handle, err := repo.Load(ctx, modules)
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("delay", handle)
	clock := core.NewManualClock(time.Unix(0, 0))
	roster, err := federation.New(federation.Options{Clock: clock, FOMs: repo})
	if err != nil {
		t.Fatal(err)
	}
	if err := roster.CreateFederation(ctx, core.CreateFederationRequest{Name: "delay", FOMModules: modules}); err != nil {
		t.Fatal(err)
	}
	producer, err := roster.JoinFederation(ctx, core.JoinFederationRequest{Federation: "delay", FederateName: "producer"})
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := roster.JoinFederation(ctx, core.JoinFederationRequest{Federation: "delay", FederateName: "receiver"})
	if err != nil {
		t.Fatal(err)
	}
	decl := declaration.New()
	decl.SetFOMRepository(repo)
	out := &regionDeliveryOutbox{}
	tm, err := timepkg.NewOf[T](timepkg.OptionsOf[T]{Clock: clock, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	regions, err := ddm.New(ddm.Options{FOMs: repo, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := object.NewOf[T](object.OptionsOf[T]{Clock: clock, FOMs: repo, Federations: roster, Declarations: decl, Outbox: out, TSOGate: tm, TSOValidator: tm, Retractions: tm, RegistrationRegions: regions})
	if err != nil {
		t.Fatal(err)
	}
	tm.SetQueuedMessageFilter(reg)
	decl.SetOnSubscribeObjectClass(reg.ObjectClassSubscribed)
	if err := tm.EnableRegulation(ctx, "delay", producer, 1); err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableConstrained(ctx, "delay", receiver); err != nil {
		t.Fatal(err)
	}
	class, _ := handle.LookupObjectClass("HLAobjectRoot.Item")
	first, _ := handle.LookupAttribute(class, "first")
	second, _ := handle.LookupAttribute(class, "second")
	interaction, _ := handle.LookupInteractionClass("HLAinteractionRoot.Event")
	parameter, _ := handle.LookupParameter(interaction, "payload")
	if class == 0 || first == 0 || second == 0 || interaction == 0 || parameter == 0 {
		t.Fatal("fixture model handles missing")
	}
	if err := decl.PublishInteractionClass(ctx, "delay", producer, interaction); err != nil {
		t.Fatal(err)
	}
	if err := decl.PublishObjectClassAttributes(ctx, "delay", producer, class, []core.AttributeHandle{first, second}); err != nil {
		t.Fatal(err)
	}
	out.deliveries = nil
	return delayedSubscriptionFixtureOf[T]{reg, tm, decl, out, producer, receiver, class, interaction, parameter, first, second, handle, regions}
}

func (f delayedSubscriptionFixtureOf[T]) releaseTSO(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if err := f.times.TimeAdvanceRequest(ctx, "delay", f.receiver, 10); err != nil {
		t.Fatal(err)
	}
	if err := f.times.TimeAdvanceRequest(ctx, "delay", f.producer, 10); err != nil {
		t.Fatal(err)
	}
}

func (f delayedSubscriptionFixtureOf[T]) callback(kind object.CallbackKind) *object.CallbackEventOf[T] {
	for _, delivery := range f.out.deliveries {
		if delivery.Recipient == f.receiver {
			if callback, ok := delivery.Event.(*object.CallbackEventOf[T]); ok && callback.Kind == kind {
				return callback
			}
		}
	}
	return nil
}

// IEEE 1516.1-2010 8.1.8: this recipient is already joined at generation.
// The oracle does not assert retrospective delivery to post-generation joins.
func TestG13EnabledDelaySubscriptionLateInteractionInterest(t *testing.T) {
	for _, initiallySubscribed := range []bool{false, true} {
		name := "late-subscription"
		if initiallySubscribed {
			name = "already-subscribed-control"
		}
		t.Run(name, func(t *testing.T) {
			f := newDelayedSubscriptionReviewFixture(t)
			ctx := context.Background()
			if initiallySubscribed {
				if err := f.declarations.SubscribeInteractionClass(ctx, "delay", f.receiver, f.interaction); err != nil {
					t.Fatal(err)
				}
			}
			at := core.LogicalTime(10)
			if err := f.objects.SendInteraction(ctx, "delay", f.producer, f.interaction, map[core.ParameterHandle][]byte{f.parameter: []byte("retained")}, &at); err != nil {
				t.Fatal(err)
			}
			if f.callback(object.CallbackReceive) != nil {
				t.Fatal("TSO message delivered before eligibility")
			}
			if !initiallySubscribed {
				if err := f.declarations.SubscribeInteractionClass(ctx, "delay", f.receiver, f.interaction); err != nil {
					t.Fatal(err)
				}
			}
			f.releaseTSO(t)
			callback := f.callback(object.CallbackReceive)
			if callback == nil {
				t.Fatal("G13: enabled delay switch lost interaction sent before current subscription")
			}
			if string(callback.Parameters[uint64(f.parameter)]) != "retained" {
				t.Fatal("queued interaction payload changed")
			}
		})
	}
}

func TestG13EnabledDelaySubscriptionLateAttributeInterest(t *testing.T) {
	f := newDelayedSubscriptionReviewFixture(t)
	ctx := context.Background()
	if err := f.declarations.SubscribeObjectClassAttributes(ctx, "delay", f.receiver, f.class, []core.AttributeHandle{f.first}); err != nil {
		t.Fatal(err)
	}
	instance, _, err := f.objects.Register(ctx, "delay", f.producer, f.class, "")
	if err != nil {
		t.Fatal(err)
	}
	if f.callback(object.CallbackDiscover) == nil {
		t.Fatal("receiver was not already aware of the object")
	}
	f.out.deliveries = nil
	at := core.LogicalTime(10)
	if err := f.objects.UpdateAttributes(ctx, "delay", f.producer, instance, map[core.AttributeHandle][]byte{f.first: []byte("first"), f.second: []byte("second")}, &at); err != nil {
		t.Fatal(err)
	}
	if f.callback(object.CallbackReflect) != nil {
		t.Fatal("TSO reflection delivered before eligibility")
	}
	if err := f.declarations.SubscribeObjectClassAttributes(ctx, "delay", f.receiver, f.class, []core.AttributeHandle{f.second}); err != nil {
		t.Fatal(err)
	}
	f.releaseTSO(t)
	callback := f.callback(object.CallbackReflect)
	if callback == nil || string(callback.Attributes[uint64(f.first)]) != "first" || string(callback.Attributes[uint64(f.second)]) != "second" {
		t.Fatalf("G13: send-time projection lost newly subscribed attribute: %+v", callback)
	}
}
