package object_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

// Oracle: IEEE 1516.1-2010 6.1.1, 6.9.3-4 and 8.1.8 (PDF 107, 122, 191).
// The receiver is already joined, but knows no object when the update is sent.
// Do not require discovery to occur inside Subscribe: require it before reflect.
func TestG13PreviouslyUnknownObjectLateAttributeInterest(t *testing.T) {
	t.Run("Float64", g13PreviouslyUnknownObject[core.LogicalTime])
	t.Run("Integer64", g13PreviouslyUnknownObject[int64])
}

func g13PreviouslyUnknownObject[T core.LogicalTimeScalar](t *testing.T) {
	for _, variant := range []string{"ordinary", "time-restore", "ancestor"} {
		t.Run(variant, func(t *testing.T) {
			xml := delayedSubscriptionReviewXML
			if variant == "ancestor" {
				xml = strings.Replace(xml, "</attribute>\n</objectClass>", "</attribute><objectClass><name>Child</name><attribute><name>childOnly</name><order>TimeStamp</order></attribute></objectClass>\n</objectClass>", 1)
			}
			f := newDelayedSubscriptionFixtureOf[T](t, xml)
			ctx := context.Background()
			registered, actualFirst, actualSecond := f.class, f.first, f.second
			if variant == "ancestor" {
				registered, _ = f.fom.LookupObjectClass("HLAobjectRoot.Item.Child")
				actualFirst, _ = f.fom.LookupAttribute(registered, "first")
				actualSecond, _ = f.fom.LookupAttribute(registered, "second")
				if err := f.declarations.PublishObjectClassAttributes(ctx, "delay", f.producer, registered, []core.AttributeHandle{actualFirst, actualSecond}); err != nil {
					t.Fatal(err)
				}
			}
			instance, name, err := f.objects.Register(ctx, "delay", f.producer, registered, "")
			if err != nil {
				t.Fatal(err)
			}
			if f.callback(object.CallbackDiscover) != nil {
				t.Fatal("unsubscribed receiver discovered at registration")
			}
			at := T(10)
			if err := f.objects.UpdateAttributes(ctx, "delay", f.producer, instance, map[core.AttributeHandle][]byte{actualFirst: []byte("first"), actualSecond: []byte("late-second")}, &at); err != nil {
				t.Fatal(err)
			}
			if f.callback(object.CallbackDiscover) != nil || f.callback(object.CallbackReflect) != nil {
				t.Fatal("generation-time candidate retention announced an uninterested object")
			}
			if variant == "time-restore" {
				codec := savebundle.TimeOf[T]{Manager: f.times}
				image, err := codec.Marshal("delay")
				if err != nil {
					t.Fatal(err)
				}
				if err := codec.Unmarshal("delay", image); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.declarations.SubscribeObjectClassAttributes(ctx, "delay", f.receiver, f.class, []core.AttributeHandle{f.second}); err != nil {
				t.Fatal(err)
			}
			f.releaseTSO(t)
			discoveryIndex, reflectionIndex, discoveries, reflections := -1, -1, 0, 0
			for i, delivery := range f.out.deliveries {
				if delivery.Recipient != f.receiver {
					continue
				}
				event, ok := delivery.Event.(*object.CallbackEventOf[T])
				if !ok {
					continue
				}
				switch event.Kind {
				case object.CallbackDiscover:
					discoveries++
					discoveryIndex = i
					if event.Object != instance || event.ObjectName != name || event.ObjectClass != f.class {
						t.Fatal("wrong discovery identity/candidate class", event)
					}
				case object.CallbackReflect:
					reflections++
					reflectionIndex = i
					if event.Object != instance || event.ObjectClass != f.class || len(event.Attributes) != 1 || string(event.Attributes[uint64(f.second)]) != "late-second" {
						t.Fatal("late current interest not projected at known class", event)
					}
				}
			}
			if discoveries != 1 || reflections != 1 || discoveryIndex >= reflectionIndex {
				t.Fatalf("discover=%d reflect=%d order=%d,%d", discoveries, reflections, discoveryIndex, reflectionIndex)
			}
			before := len(f.out.deliveries)
			if err := f.declarations.SubscribeObjectClassAttributes(ctx, "delay", f.receiver, registered, []core.AttributeHandle{actualFirst}); err != nil {
				t.Fatal(err)
			}
			for _, delivery := range f.out.deliveries[before:] {
				if event, ok := delivery.Event.(*object.CallbackEventOf[T]); ok && delivery.Recipient == f.receiver && event.Kind == object.CallbackDiscover {
					t.Fatal("known object rediscovered solely for changed subscription", event)
				}
			}
		})
	}
}

func TestG13PreviouslyUnknownObjectWithoutInterest(t *testing.T) {
	f := newDelayedSubscriptionFixtureOf[core.LogicalTime](t, delayedSubscriptionReviewXML)
	ctx := context.Background()
	instance, _, err := f.objects.Register(ctx, "delay", f.producer, f.class, "")
	if err != nil {
		t.Fatal(err)
	}
	at := core.LogicalTime(10)
	if err := f.objects.UpdateAttributes(ctx, "delay", f.producer, instance, map[core.AttributeHandle][]byte{f.second: []byte("value")}, &at); err != nil {
		t.Fatal(err)
	}
	f.releaseTSO(t)
	if f.callback(object.CallbackDiscover) != nil || f.callback(object.CallbackReflect) != nil {
		t.Fatal("retention alone satisfies neither discovery nor reflection preconditions")
	}
}
