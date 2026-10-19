package object_test

import (
	"encoding/json"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestConveyedReflectionPreservesKnownAttributeAndSenderBounds(t *testing.T) {
	f := newKnownFixture(t)
	f.subscribe(t, 2, f.parent, f.base)
	obj := f.register(t)
	f.drain(2)
	pub := f.region(t, 1, 10, 20)
	sub := f.region(t, 2, 10, 20)
	knownOK(t, f.ddm.AssociateRegionsForUpdates(f.ctx, knownFed, 1, obj, map[core.AttributeHandle][]core.DDMRegionHandleCore{f.inherited: {pub}}))
	knownOK(t, f.ddm.SubscribeObjectClassAttributesWithRegions(f.ctx, knownFed, 2, f.parent, []core.AttributeHandle{f.base}, []ddm.RegionHandle{sub}))
	knownOK(t, f.decl.UnsubscribeObjectClassAttributes(f.ctx, knownFed, 2, f.parent, []core.AttributeHandle{f.base}))
	knownOK(t, f.reg.UpdateAttributes(f.ctx, knownFed, 1, obj, map[core.AttributeHandle][]byte{f.inherited: {1}}, nil))
	var event *object.CallbackEvent
	for _, candidate := range f.drain(2) {
		if candidate.Kind == object.CallbackReflect {
			event = candidate
		}
	}
	if event == nil {
		t.Fatal("no reflection")
	}
	if event.ObjectClass != f.parent || len(event.Attributes) != 1 || event.Attributes[uint64(f.base)] == nil {
		t.Fatalf("incorrect known-class view: %+v", event)
	}
	copies, ok := event.Metadata.AttributeConveyedRegions[f.base]
	if !ok || len(copies) != 1 || copies[0].Realization.SourceRegion() != pub {
		t.Fatalf("mapped metadata=%+v", event.Metadata)
	}
	if _, exists := event.Metadata.AttributeConveyedRegions[f.inherited]; exists {
		t.Fatal("canonical attribute leaked into recipient metadata")
	}
	knownOK(t, f.ddm.SetRangeBounds(knownFed, 1, pub, f.dimension, ddm.Range{Lower: 30, Upper: 40}))
	knownOK(t, f.ddm.CommitRegionModifications(f.ctx, knownFed, 1, []ddm.RegionHandle{pub}))
	if got := f.reg.FilterQueuedMessage(f.ctx, knownFed, 2, event); got == nil {
		t.Fatal("reflection reread changed sender bounds")
	}
	// The portable event survives a real JSON encode/decode without requiring
	// source identity or exact Go object identity for its retained realization.
	encoded, err := json.Marshal(event)
	knownOK(t, err)
	var restored object.CallbackEvent
	knownOK(t, json.Unmarshal(encoded, &restored))
	knownOK(t, f.ddm.UnassociateRegionsForUpdates(f.ctx, knownFed, 1, obj, nil))
	knownOK(t, f.ddm.DeleteRegion(f.ctx, knownFed, 1, pub))
	if got := f.reg.FilterQueuedMessage(f.ctx, knownFed, 2, &restored); got == nil {
		t.Fatal("portable reflection requires live sender region")
	}
	knownOK(t, f.ddm.SetRangeBounds(knownFed, 2, sub, f.dimension, ddm.Range{Lower: 30, Upper: 40}))
	knownOK(t, f.ddm.CommitRegionModifications(f.ctx, knownFed, 2, []ddm.RegionHandle{sub}))
	if got := f.reg.FilterQueuedMessage(f.ctx, knownFed, 2, &restored); got != nil {
		t.Fatal("reflection ignored current receiver subscription bounds")
	}
}
