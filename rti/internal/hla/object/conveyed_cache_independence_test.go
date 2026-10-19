package object

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

func TestConveyedReflectionFiltersWithoutCopiedCache(t *testing.T) {
	ctx := context.Background()
	r, _, out, gate, obj := roFixture(t, 2)
	m, region := roRegions(t, r)
	p1, p2, sub := region(1, 0, 10), region(1, 20, 30), region(2, 0, 40)
	if err := m.AssociateRegionsForUpdates(ctx, "fed", 1, obj, map[core.AttributeHandle][]core.DDMRegionHandleCore{1: {p1}, 2: {p2}}); err != nil {
		t.Fatal(err)
	}
	if err := m.SubscribeObjectClassAttributesWithRegions(ctx, "fed", 2, 1, []core.AttributeHandle{1, 2}, []ddm.RegionHandle{sub}); err != nil {
		t.Fatal(err)
	}
	if err := r.opts.Declarations.(*declaration.Manager).UnsubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatal(err)
	}
	stamp := core.LogicalTime(10)
	if err := r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {1}, 2: {2}}, &stamp); err != nil {
		t.Fatal(err)
	}
	var original *CallbackEvent
	for _, delivery := range gate.deliveries {
		if event, ok := delivery.Event.(*CallbackEvent); ok && event.Kind == CallbackReflect {
			original = event
		}
	}
	if original == nil || len(original.Attributes) != 2 || len(original.Metadata.AttributeConveyedRegions) != 2 {
		t.Fatalf("actual TSO callback lost per-attribute values: %+v", original)
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored CallbackEvent
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if err := m.SetRangeBounds("fed", 2, sub, 1, ddm.Range{Lower: 0, Upper: 10}); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitRegionModifications(ctx, "fed", 2, []ddm.RegionHandle{sub}); err != nil {
		t.Fatal(err)
	}
	state := m.ExportState("fed")
	state.ConveyedRegions = nil
	fresh, err := ddm.New(ddm.Options{Outbox: out, FOMs: r.opts.FOMs})
	if err != nil {
		t.Fatal(err)
	}
	fresh.ImportState("fed", state)
	r.opts.RegistrationRegions, r.opts.DDM = fresh, roDDMFilter{fresh}
	for _, copy := range restored.ConveyedRegionCopies() {
		if _, found := fresh.QueryBounds("fed", copy.Designator, 1); found {
			t.Fatal("fixture retained a copied realization cache")
		}
	}
	filtered, ok := r.FilterQueuedMessage(ctx, "fed", 2, &restored).(*CallbackEvent)
	if !ok || len(filtered.Attributes) != 1 || filtered.Attributes[1] == nil {
		t.Fatalf("portable per-attribute filtering failed: %+v", filtered)
	}
	if len(filtered.Metadata.ConveyedRegions) != 1 || len(filtered.Metadata.AttributeConveyedRegions) != 1 || filtered.Metadata.AttributeConveyedRegions[1][0].Realization.SourceRegion() != p1 {
		t.Fatalf("filtered metadata=%+v", filtered.Metadata)
	}
	if len(restored.Attributes) != 2 || len(restored.Metadata.ConveyedRegions) != 2 || len(restored.Metadata.AttributeConveyedRegions) != 2 {
		t.Fatal("filter mutated accepted source needed for retry")
	}
}
