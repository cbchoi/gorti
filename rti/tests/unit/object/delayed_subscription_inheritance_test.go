package object_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestG13TypedInheritedProjection(t *testing.T) {
	t.Run("Float64", g13InheritedProjection[core.LogicalTime])
	t.Run("Integer64", g13InheritedProjection[int64])
}

func g13InheritedProjection[T core.LogicalTimeScalar](t *testing.T) {
	xml := strings.Replace(delayedSubscriptionReviewXML, "</attribute>\n</objectClass>", "</attribute><objectClass><name>Child</name><attribute><name>childOnly</name><order>TimeStamp</order></attribute></objectClass>\n</objectClass>", 1)
	xml = strings.Replace(xml, "</parameter></interactionClass>", "</parameter><interactionClass><name>Child</name><order>TimeStamp</order><parameter><name>childOnly</name></parameter></interactionClass></interactionClass>", 1)
	f := newDelayedSubscriptionFixtureOf[T](t, xml)
	ctx := context.Background()
	child, _ := f.fom.LookupObjectClass("HLAobjectRoot.Item.Child")
	first, _ := f.fom.LookupAttribute(child, "first")
	second, _ := f.fom.LookupAttribute(child, "second")
	childOnly, _ := f.fom.LookupAttribute(child, "childOnly")
	childEvent, _ := f.fom.LookupInteractionClass("HLAinteractionRoot.Event.Child")
	payload, _ := f.fom.LookupParameter(childEvent, "payload")
	onlyParameter, _ := f.fom.LookupParameter(childEvent, "childOnly")
	if child == 0 || childEvent == 0 || first == f.first {
		t.Fatal("fixture does not distinguish inherited coordinates")
	}
	if err := f.declarations.PublishObjectClassAttributes(ctx, "delay", f.producer, child, []core.AttributeHandle{first, second, childOnly}); err != nil {
		t.Fatal(err)
	}
	if err := f.declarations.PublishInteractionClass(ctx, "delay", f.producer, childEvent); err != nil {
		t.Fatal(err)
	}
	if err := f.declarations.SubscribeObjectClassAttributes(ctx, "delay", f.receiver, f.class, []core.AttributeHandle{f.first}); err != nil {
		t.Fatal(err)
	}
	instance, _, err := f.objects.Register(ctx, "delay", f.producer, child, "")
	if err != nil {
		t.Fatal(err)
	}
	discovered := f.callback(object.CallbackDiscover)
	if discovered == nil || discovered.ObjectClass != f.class {
		t.Fatal("expected ancestor discovery", discovered)
	}
	f.out.deliveries = nil
	at := T(10)
	if err := f.objects.UpdateAttributes(ctx, "delay", f.producer, instance, map[core.AttributeHandle][]byte{first: []byte("first"), second: []byte("second"), childOnly: []byte("hidden")}, &at); err != nil {
		t.Fatal(err)
	}
	if err := f.objects.SendInteraction(ctx, "delay", f.producer, childEvent, map[core.ParameterHandle][]byte{payload: []byte("payload"), onlyParameter: []byte("hidden")}, &at); err != nil {
		t.Fatal(err)
	}
	codec := savebundle.TimeOf[T]{Manager: f.times}
	data, err := codec.Marshal("delay")
	if err != nil {
		t.Fatal(err)
	}
	if err := codec.Unmarshal("delay", data); err != nil {
		t.Fatal(err)
	}
	if err := f.declarations.SubscribeObjectClassAttributes(ctx, "delay", f.receiver, f.class, []core.AttributeHandle{f.second}); err != nil {
		t.Fatal(err)
	}
	if err := f.declarations.SubscribeInteractionClass(ctx, "delay", f.receiver, f.interaction); err != nil {
		t.Fatal(err)
	}
	f.releaseTSO(t)
	reflection := f.callback(object.CallbackReflect)
	if reflection == nil || reflection.ObjectClass != f.class || len(reflection.Attributes) != 2 || string(reflection.Attributes[uint64(f.first)]) != "first" || string(reflection.Attributes[uint64(f.second)]) != "second" {
		t.Fatal("canonical attributes not projected to historical ancestor", reflection)
	}
	interaction := f.callback(object.CallbackReceive)
	if interaction == nil || interaction.InteractionClass != f.interaction || len(interaction.Parameters) != 1 || string(interaction.Parameters[uint64(f.parameter)]) != "payload" {
		t.Fatal("interaction not projected to current subscribed ancestor", interaction)
	}
}
