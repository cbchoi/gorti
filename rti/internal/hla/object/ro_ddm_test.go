package object

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

type roDDMFilter struct{ *ddm.Manager }

func (d roDDMFilter) PublisherRegionsFor(f core.FederationName, obj core.ObjectHandle, attr core.AttributeHandle) []DDMRegionHandle {
	var result []DDMRegionHandle
	for _, region := range d.Manager.PublisherRegionsFor(f, obj, attr) {
		result = append(result, DDMRegionHandle(region))
	}
	return result
}

func (d roDDMFilter) SubscribersForUpdate(f core.FederationName, cls core.ObjectClassHandle, attr core.AttributeHandle, regions []DDMRegionHandle) []core.FederateHandle {
	var converted []core.DDMRegionHandleCore
	for _, region := range regions {
		converted = append(converted, core.DDMRegionHandleCore(region))
	}
	return d.Manager.SubscribersForUpdate(f, cls, attr, converted)
}

func roRegions(t *testing.T, r *Registry) (*ddm.Manager, func(core.FederateHandle, uint64, uint64) core.DDMRegionHandleCore) {
	t.Helper()
	ctx := context.Background()
	m, err := ddm.New(ddm.Options{Outbox: r.opts.Outbox, FOMs: r.opts.FOMs, Objects: r})
	if err != nil {
		t.Fatal(err)
	}
	r.opts.DDM = roDDMFilter{m}
	r.opts.RegistrationRegions = m
	space, _ := m.LookupRoutingSpace("fed", "default")
	dimension, _ := m.LookupDimension("fed", space, "x")
	return m, func(owner core.FederateHandle, lower, upper uint64) core.DDMRegionHandleCore {
		t.Helper()
		region, err := m.CreateRegion(ctx, "fed", owner, space, []ddm.DimensionHandle{dimension})
		if err != nil {
			t.Fatal(err)
		}
		if err := m.SetRangeBounds("fed", owner, region, dimension, ddm.Range{Lower: lower, Upper: upper}); err != nil {
			t.Fatal(err)
		}
		if err := m.CommitRegionModifications(ctx, "fed", owner, []ddm.RegionHandle{region}); err != nil {
			t.Fatal(err)
		}
		return region
	}
}

func TestROQueuedDDMReflectionRetainsPerAttributeRegions(t *testing.T) {
	ctx := context.Background()
	r, tm, out, gate, obj := roFixture(t, 2)
	m, region := roRegions(t, r)
	pub1, pub2, sub := region(1, 0, 10), region(1, 20, 30), region(2, 0, 40)
	if err := m.AssociateRegionsForUpdates(ctx, "fed", 1, obj, map[core.AttributeHandle][]core.DDMRegionHandleCore{1: {pub1}, 2: {pub2}}); err != nil {
		t.Fatal(err)
	}
	if err := m.SubscribeObjectClassAttributesWithRegions(ctx, "fed", 2, 1, []core.AttributeHandle{1, 2}, []ddm.RegionHandle{sub}); err != nil {
		t.Fatal(err)
	}
	tm.SetQueuedMessageFilter(roFilter(t, r))
	if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {1}, 2: {2}}, nil); err != nil {
		t.Fatal(err)
	}
	if gate.calls != 1 || len(gate.deliveries) != 2 {
		t.Fatalf("regional RO groups must share one reservation: calls=%d messages=%v", gate.calls, gate.deliveries)
	}
	seen := make(map[uint64]bool)
	for _, delivery := range gate.deliveries {
		event := delivery.Event.(*CallbackEvent)
		if delivery.Recipient != 2 || !delivery.ReceiveOrder || len(event.Attributes) != 1 {
			t.Fatalf("regional RO partition=%+v", delivery)
		}
		for attr, value := range event.Attributes {
			if seen[attr] || (attr != 1 && attr != 2) || len(value) != 1 || value[0] != byte(attr) {
				t.Fatalf("duplicate or changed regional attribute %d: %v", attr, value)
			}
			seen[attr] = true
			wantRegion := pub1
			if attr == 2 {
				wantRegion = pub2
			}
			if len(event.Metadata.Regions) != 1 || event.Metadata.Regions[0] != wantRegion {
				t.Fatalf("attribute %d lost its region: %+v", attr, event.Metadata)
			}
		}
	}
	decl := r.opts.Declarations.(*declaration.Manager)
	if err := decl.UnsubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetRangeBounds("fed", 2, sub, 1, ddm.Range{Lower: 0, Upper: 10}); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitRegionModifications(ctx, "fed", 2, []ddm.RegionHandle{sub}); err != nil {
		t.Fatal(err)
	}
	if err := m.UnassociateRegionsForUpdates(ctx, "fed", 1, obj, nil); err != nil {
		t.Fatal(err)
	}
	out.records = nil
	if err := tm.EnableAsynchronousDelivery(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	var attributes []uint64
	for _, record := range out.records {
		if event, ok := record.Event.(*CallbackEvent); ok && event.Kind == CallbackReflect {
			for attr := range event.Attributes {
				attributes = append(attributes, attr)
			}
		}
	}
	if len(attributes) != 1 || attributes[0] != 1 {
		t.Fatalf("regional recheck received attributes %v, want only 1", attributes)
	}
}

func TestROQueuedDDMInteractionRechecksOverlap(t *testing.T) {
	ctx := context.Background()
	r, tm, out, gate, _ := roFixture(t, 2)
	m, region := roRegions(t, r)
	pub, sub := region(1, 0, 10), region(2, 0, 10)
	decl := r.opts.Declarations.(*declaration.Manager)
	if err := decl.UnsubscribeInteractionClass(ctx, "fed", 2, 7); err != nil {
		t.Fatal(err)
	}
	if err := m.SubscribeInteractionClassWithRegions(ctx, "fed", 2, 7, []ddm.RegionHandle{sub}); err != nil {
		t.Fatal(err)
	}
	tm.SetQueuedMessageFilter(roFilter(t, r))
	if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	out.records = nil
	if err := r.SendInteractionWithRegionsMetadata(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {1}}, nil, []core.DDMRegionHandleCore{pub}, core.ObjectMessageOptions{Tag: []byte(testRegional)}); err != nil {
		t.Fatal(err)
	}
	if gate.calls != 1 || len(gate.deliveries) != 1 || !gate.deliveries[0].ReceiveOrder || len(out.records) != 0 {
		t.Fatalf("regional RO admission=%v callbacks=%v", gate.deliveries, out.records)
	}
	if err := m.SetRangeBounds("fed", 2, sub, 1, ddm.Range{Lower: 20, Upper: 30}); err != nil {
		t.Fatal(err)
	}
	if err := m.CommitRegionModifications(ctx, "fed", 2, []ddm.RegionHandle{sub}); err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableAsynchronousDelivery(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if len(out.records) != 0 {
		t.Fatalf("out-of-region interaction delivered: %v", out.records)
	}
}
