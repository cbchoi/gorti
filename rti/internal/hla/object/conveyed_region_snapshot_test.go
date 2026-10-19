package object

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

// IEEE 1516.1-2010 9.1: a conveyed designator refers to the realization
// used for overlap, not the sender's mutable region specification.
func TestConveyedRegionDesignatorDoesNotFollowSenderCommit(t *testing.T) {
	ctx := context.Background()
	r, _, out, gate, _ := roFixture(t, 2)
	m, create := roRegions(t, r)
	pub := create(1, 0, 10)
	sub := create(2, 0, 10)
	if err := r.opts.Declarations.(*declaration.Manager).UnsubscribeInteractionClass(ctx, "fed", 2, 7); err != nil {
		t.Fatal(err)
	}
	if err := m.SubscribeInteractionClassWithRegions(ctx, "fed", 2, 7, []ddm.RegionHandle{sub}); err != nil {
		t.Fatal(err)
	}
	out.records = nil
	if err := r.SendInteractionWithRegionsMetadata(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {1}}, nil, []core.DDMRegionHandleCore{pub}, core.ObjectMessageOptions{}); err != nil {
		t.Fatal(err)
	}
	var received *CallbackEvent
	for _, delivery := range gate.deliveries {
		if event, ok := delivery.Event.(*CallbackEvent); ok && event.Kind == CallbackReceive {
			received = event
		}
	}
	if received == nil {
		for _, record := range out.records {
			if event, ok := record.Event.(*CallbackEvent); ok && event.Kind == CallbackReceive {
				received = event
			}
		}
	}
	if received == nil || received.Metadata == nil || len(received.Metadata.Regions) != 1 {
		t.Fatal("regional callback metadata missing")
	}
	conveyed := received.Metadata.ConveyedRegionDesignators()[0]
	if err := m.ConveyCopies(ctx, "fed", 2, received.ConveyedRegionCopies()); err != nil {
		t.Fatal(err)
	}
	if err := m.EnterConveyedRegions(ctx, "fed", 2, []ddm.RegionHandle{conveyed}); err != nil {
		t.Fatal(err)
	}
	before, err := m.QueryBoundsFor("fed", 2, conveyed, 1)
	if err != nil || before != (ddm.Range{Lower: 0, Upper: 10}) {
		t.Fatalf("initial conveyed bounds=%+v err=%v", before, err)
	}
	if err := m.SetRangeBounds("fed", 1, pub, 1, ddm.Range{Lower: 20, Upper: 30}); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitRegionModifications(ctx, "fed", 1, []ddm.RegionHandle{pub}); err != nil {
		t.Fatal(err)
	}
	after, err := m.QueryBoundsFor("fed", 2, conveyed, 1)
	if err != nil || after != before {
		t.Fatalf("conveyed designator is a live source reference: before=%+v after=%+v err=%v source=%v conveyed=%v", before, after, err, pub, conveyed)
	}
	if err := m.ReleaseConveyedRegions(ctx, "fed", 2, []ddm.RegionHandle{conveyed}); err != nil {
		t.Fatal(err)
	}
	state := m.ExportState("fed")
	state.ConveyedRegions = nil
	fresh, err := ddm.New(ddm.Options{Outbox: out, FOMs: r.opts.FOMs})
	if err != nil {
		t.Fatal(err)
	}
	fresh.ImportState("fed", state)
	r.opts.RegistrationRegions = fresh
	r.opts.DDM = roDDMFilter{fresh}
	if _, err := fresh.QueryBoundsFor("fed", 2, conveyed, 1); err == nil {
		t.Fatal("fresh manager unexpectedly holds a copied-region lease")
	}
	if filtered := r.FilterQueuedMessage(ctx, "fed", 2, received); filtered == nil {
		t.Fatal("queued interaction depends on live source bounds or copy-cache membership")
	}
}
