package object

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

type momPointTestFOM struct{ roFOM }

func (*momPointTestFOM) AttributeHandles(cls core.ObjectClassHandle) []core.AttributeHandle {
	if cls == 1 {
		return []core.AttributeHandle{1, 2}
	}
	if cls == 9 {
		return []core.AttributeHandle{101}
	}
	return nil
}

type momPointTestRepo struct{ roFOMRepository }

func (*momPointTestRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return &momPointTestFOM{}, nil
}

func momPointFixture(t *testing.T) (*Registry, *ddm.Manager, *atomicUpdateOutbox, core.ObjectHandle) {
	t.Helper()
	r, _, out, _, obj := roFixture(t)
	repo := &momPointTestRepo{}
	r.opts.FOMs = repo
	r.opts.Declarations.(*declaration.Manager).SetFOMRepository(repo)
	m, region := roRegions(t, r)
	ctx := context.Background()
	d := r.opts.Declarations.(*declaration.Manager)
	if err := d.SubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		h            core.FederateHandle
		lower, upper uint64
	}{{3, 10, 20}, {4, 20, 30}} {
		reg := region(row.h, row.lower, row.upper)
		if err := m.SubscribeObjectClassAttributesWithRegions(ctx, "fed", row.h, 1, []core.AttributeHandle{2}, []ddm.RegionHandle{reg}); err != nil {
			t.Fatal(err)
		}
	}
	out.records = nil
	return r, m, out, obj
}

func TestMOMPointDeliveryOrdinaryRegionalUnionAndOrdering(t *testing.T) {
	r, _, out, obj := momPointFixture(t)
	point := core.MOMAttributePoint{Subject: 15, Dimension: 1, Coordinate: 15}
	attrs := map[core.AttributeHandle][]byte{1: {1}, 2: {2}}
	if err := r.PublishMOMAttributePoint(context.Background(), "fed", obj, 1, attrs, point); err != nil {
		t.Fatal(err)
	}
	seen := map[core.FederateHandle][]CallbackKind{}
	for _, delivery := range out.records {
		e := delivery.Event.(*CallbackEvent)
		seen[delivery.Federate] = append(seen[delivery.Federate], e.Kind)
		if e.Kind == CallbackReflect {
			want := uint64(1)
			if delivery.Federate == 3 {
				want = 2
			}
			if len(e.Attributes) != 1 || !slices.Equal(e.Attributes[want], []byte{byte(want)}) {
				t.Fatalf("recipient %d attributes=%v", delivery.Federate, e.Attributes)
			}
		}
	}
	for _, h := range []core.FederateHandle{2, 3} {
		if !slices.Equal(seen[h], []CallbackKind{CallbackDiscover, CallbackReflect}) {
			t.Fatalf("recipient %d callback order=%v", h, seen[h])
		}
	}
	if len(seen) != 2 {
		t.Fatalf("point leaked to outside subscriber: %v", seen)
	}
}

func TestMOMPointDeliveryTargetValidatesInterestAndProjectsKnownClass(t *testing.T) {
	r, _, out, obj := momPointFixture(t)
	ctx := context.Background()
	point := core.MOMAttributePoint{Subject: 15, Dimension: 1, Coordinate: 15}
	attrs := map[core.AttributeHandle][]byte{1: {1}, 2: {2}}
	if err := r.ReflectMOMAttributePoint(ctx, "fed", 4, obj, 1, attrs, point); err != nil {
		t.Fatal(err)
	}
	if len(out.records) != 0 {
		t.Fatal("targeted late publication bypassed point interest")
	}
	d := r.opts.Declarations.(*declaration.Manager)
	if err := d.SubscribeObjectClassAttributes(ctx, "fed", 5, 9, []core.AttributeHandle{101}); err != nil {
		t.Fatal(err)
	}
	if err := r.ReflectMOMAttributePoint(ctx, "fed", 5, obj, 1, attrs, point); err != nil {
		t.Fatal(err)
	}
	if len(out.records) != 2 {
		t.Fatalf("late callbacks=%v", out.records)
	}
	for i, delivery := range out.records {
		e := delivery.Event.(*CallbackEvent)
		if delivery.Federate != 5 || e.ObjectClass != 9 {
			t.Fatalf("known-class projection=%+v", e)
		}
		if i == 0 && e.Kind != CallbackDiscover || i == 1 && (e.Kind != CallbackReflect || len(e.Attributes) != 1 || !slices.Equal(e.Attributes[101], []byte{1})) {
			t.Fatalf("callback %d=%+v", i, e)
		}
	}
}

func TestMOMPointDeliveryReservationFailurePreservesDiscovery(t *testing.T) {
	r, _, out, obj := momPointFixture(t)
	reject := &rejectingUpdateOutbox{err: core.ErrFederateOverflow}
	r.opts.Outbox = reject
	point := core.MOMAttributePoint{Subject: 15, Dimension: 1, Coordinate: 15}
	attrs := map[core.AttributeHandle][]byte{1: {1}, 2: {2}}
	if err := r.PublishMOMAttributePoint(context.Background(), "fed", obj, 1, attrs, point); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("admission error=%v", err)
	}
	st := r.stateFor("fed")
	st.mu.Lock()
	known := st.discovered[discoverKey{sub: 2, obj: obj}] != 0 || st.discovered[discoverKey{sub: 3, obj: obj}] != 0
	st.mu.Unlock()
	if known {
		t.Fatal("failed admission published object knowledge")
	}
	if reject.sendCalls != 0 {
		t.Fatal("fanout bypassed reservation")
	}
	r.opts.Outbox = out
	if err := r.PublishMOMAttributePoint(context.Background(), "fed", obj, 1, attrs, point); err != nil {
		t.Fatal(err)
	}
	if len(out.records) != 4 {
		t.Fatalf("retry callbacks=%v", out.records)
	}
}

func TestMOMPointDeliveryQueuedRechecksPointNotDefaultRegion(t *testing.T) {
	r, m, out, obj := momPointFixture(t)
	ctx := context.Background()
	gate := r.opts.TSOGate.(*roObservedGate)
	gate.Manager.SetQueuedMessageFilter(r)
	if err := gate.Manager.EnableConstrained(ctx, "fed", 3); err != nil {
		t.Fatal(err)
	}
	point := core.MOMAttributePoint{Subject: 15, Dimension: 1, Coordinate: 15}
	attrs := map[core.AttributeHandle][]byte{1: {1}, 2: {2}}
	if err := r.PublishMOMAttributePoint(ctx, "fed", obj, 1, attrs, point); err != nil {
		t.Fatal(err)
	}
	var queued *CallbackEvent
	for _, delivery := range gate.deliveries {
		if delivery.Recipient == 3 {
			queued = delivery.Event.(*CallbackEvent)
		}
	}
	if queued == nil || queued.Metadata == nil || queued.Metadata.MOMAttributePoint == nil {
		t.Fatal("queued point metadata missing")
	}
	point.Coordinate = 99
	attrs[2][0] = 99
	if queued.Metadata.MOMAttributePoint.Coordinate != 15 || queued.Attributes[2][0] != 2 {
		t.Fatal("accepted queued point/payload aliases caller")
	}
	encoded, err := json.Marshal(queued)
	if err != nil {
		t.Fatal(err)
	}
	var portable CallbackEvent
	if err := json.Unmarshal(encoded, &portable); err != nil {
		t.Fatal(err)
	}
	if portable.Metadata.MOMAttributePoint == queued.Metadata.MOMAttributePoint || *portable.Metadata.MOMAttributePoint != *queued.Metadata.MOMAttributePoint {
		t.Fatal("portable point roundtrip changed snapshot")
	}
	for _, region := range m.ExportState("fed").Regions {
		if region.Owner == 3 {
			if err := m.SetRangeBounds("fed", 3, region.Handle, 1, ddm.Range{Lower: 20, Upper: 30}); err != nil {
				t.Fatal(err)
			}
			if filtered, handled := r.FilterMOMAttributePointMessage(ctx, "fed", 3, &portable); !handled || filtered == nil {
				t.Fatal("uncommitted receiver bounds changed point routing")
			}
			if err := m.CommitRegionModifications(ctx, "fed", 3, []ddm.RegionHandle{region.Handle}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if filtered, handled := r.FilterMOMAttributePointMessage(ctx, "fed", 3, &portable); !handled || filtered != nil {
		t.Fatal("committed receiver exclusion ignored")
	}
	if len(queued.Attributes) != 1 || queued.Attributes[2][0] != 2 {
		t.Fatal("filter mutated retry source")
	}
	out.records = nil
	if err := gate.Manager.EnableAsynchronousDelivery(ctx, "fed", 3); err != nil {
		t.Fatal(err)
	}
	for _, record := range out.records {
		if e, ok := record.Event.(*CallbackEvent); ok && e.Kind == CallbackReflect {
			t.Fatal("queued MOM point fell through to default-region delivery")
		}
	}
}

func TestMOMPointDeliveryRejectsInvalidPointAndUnknownObject(t *testing.T) {
	r, _, out, obj := momPointFixture(t)
	for _, point := range []core.MOMAttributePoint{{Dimension: 1}, {Subject: 15, Coordinate: 15}, {Subject: 15, Dimension: 1, Coordinate: 16}, {Subject: ^core.FederateHandle(0), Dimension: 1, Coordinate: uint64(^uint32(0))}} {
		if err := r.PublishMOMAttributePoint(context.Background(), "fed", obj, 1, map[core.AttributeHandle][]byte{1: {1}}, point); !errors.Is(err, core.ErrInvalidRegionContext) {
			t.Fatalf("invalid point=%+v error=%v", point, err)
		}
	}
	if err := r.PublishMOMAttributePoint(context.Background(), "fed", obj+100, 1, map[core.AttributeHandle][]byte{1: {1}}, core.MOMAttributePoint{Subject: 15, Dimension: 1, Coordinate: 15}); !errors.Is(err, core.ErrObjectNotFound) {
		t.Fatalf("unknown object error=%v", err)
	}
	if len(out.records) != 0 {
		t.Fatal("invalid point produced callbacks")
	}
}

func TestMOMPointDeliveryPreservesLow32BitSubjectCoordinate(t *testing.T) {
	r, _, out, obj := momPointFixture(t)
	point := core.MOMAttributePoint{Subject: 1<<32 | 15, Dimension: 1, Coordinate: 15}
	ctx := context.Background()
	if err := r.PublishMOMAttributePoint(ctx, "fed", obj, 1, map[core.AttributeHandle][]byte{2: {2}}, point); err != nil {
		t.Fatal(err)
	}
	reflected := false
	for _, delivery := range out.records {
		event := delivery.Event.(*CallbackEvent)
		if event.Kind != CallbackReflect {
			continue
		}
		reflected = true
		if event.Metadata.MOMAttributePoint.Subject != point.Subject {
			t.Fatal("subject identity was truncated with its coordinate")
		}
		if filtered, handled := r.FilterMOMAttributePointMessage(ctx, "fed", delivery.Federate, event); !handled || filtered == nil {
			t.Fatal("valid low-32-bit coordinate was rejected by queued filtering")
		}
	}
	if !reflected {
		t.Fatal("point did not reach its regional subscriber")
	}
}
