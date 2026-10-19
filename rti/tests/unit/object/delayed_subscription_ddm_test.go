package object_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestG13TypedFrozenDDMLateInterest(t *testing.T) {
	t.Run("Float64", g13FrozenDDMLateInterest[core.LogicalTime])
	t.Run("Integer64", g13FrozenDDMLateInterest[int64])
}

func g13FrozenDDMLateInterest[T core.LogicalTimeScalar](t *testing.T) {
	xml := strings.Replace(delayedSubscriptionReviewXML, "<objectModel>", "<objectModel><dimensions><dimension><name>x</name><upperBound>100</upperBound></dimension></dimensions>", 1)
	xml = strings.ReplaceAll(xml, "<order>TimeStamp</order>", "<order>TimeStamp</order><dimensions><dimension>x</dimension></dimensions>")
	f := newDelayedSubscriptionFixtureOf[T](t, xml)
	ctx := context.Background()
	space, ok := f.regions.LookupRoutingSpace("delay", "default")
	if !ok {
		t.Fatal("routing space unavailable")
	}
	dim, ok := f.regions.LookupDimension("delay", space, "x")
	if !ok {
		t.Fatal("dimension unavailable")
	}
	create := func(owner core.FederateHandle) core.DDMRegionHandleCore {
		region, err := f.regions.CreateRegion(ctx, "delay", owner, space, []ddm.DimensionHandle{dim})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.regions.SetRangeBounds("delay", owner, region, dim, ddm.Range{Lower: 0, Upper: 10}); err != nil {
			t.Fatal(err)
		}
		if err := f.regions.CommitRegionModifications(ctx, "delay", owner, []ddm.RegionHandle{region}); err != nil {
			t.Fatal(err)
		}
		return region
	}
	publisher, subscriber := create(f.producer), create(f.receiver)
	at := T(10)
	if err := f.objects.SendInteractionWithRegionsMetadata(ctx, "delay", f.producer, f.interaction, map[core.ParameterHandle][]byte{f.parameter: []byte("frozen")}, &at, []core.DDMRegionHandleCore{publisher}, core.ObjectMessageOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := f.regions.SetRangeBounds("delay", f.producer, publisher, dim, ddm.Range{Lower: 50, Upper: 60}); err != nil {
		t.Fatal(err)
	}
	if err := f.regions.CommitRegionModifications(ctx, "delay", f.producer, []ddm.RegionHandle{publisher}); err != nil {
		t.Fatal(err)
	}
	if err := f.regions.SubscribeInteractionClassWithRegions(ctx, "delay", f.receiver, f.interaction, []ddm.RegionHandle{subscriber}); err != nil {
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
	f.releaseTSO(t)
	event := f.callback(object.CallbackReceive)
	if event == nil || string(event.Parameters[uint64(f.parameter)]) != "frozen" {
		t.Fatal("late DDM interest followed modified publisher or pruned generation interest", event)
	}
	if len(event.Metadata.ConveyedRegions) != 1 || event.Metadata.ConveyedRegions[0].Realization.DimensionRanges()[0].Bounds.Upper != 10 {
		t.Fatal("conveyed realization changed", event.Metadata)
	}
}
