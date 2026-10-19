package savebundle

import (
	"encoding/json"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestConveyedCallbackCodecDetachedCopies(t *testing.T) {
	snapshot, err := core.NewRegionRealizationSnapshot(7, []core.RegionDimensionRange{{Dimension: 1, Bounds: core.DDMRange{Lower: 1<<53 + 1, Upper: ^uint64(0)}}})
	if err != nil {
		t.Fatal(err)
	}
	metadata := &core.ObjectCallbackMetadata{Regions: []core.DDMRegionHandleCore{7}, ConveyedRegions: []core.ConveyedRegionCopy{{Designator: 8, Realization: snapshot}}, AttributeConveyedRegions: map[core.AttributeHandle][]core.ConveyedRegionCopy{1: {{Designator: 8, Realization: snapshot}}}}
	clone, err := cloneCallbackMetadata(metadata)
	if err != nil {
		t.Fatal(err)
	}
	clone.ConveyedRegions[0].Designator = 99
	clone.AttributeConveyedRegions[1][0].Designator = 99
	clone.AttributeConveyedRegions[2] = nil
	if metadata.ConveyedRegions[0].Designator != 8 || metadata.AttributeConveyedRegions[1][0].Designator != 8 || len(metadata.AttributeConveyedRegions) != 1 {
		t.Fatal("codec clone aliases live callback copies")
	}
}

func TestConveyedIntegerCallbackCodecExactPortableValues(t *testing.T) {
	snapshot, err := core.NewRegionRealizationSnapshot(7, []core.RegionDimensionRange{{Dimension: 1, Bounds: core.DDMRange{Lower: 1<<53 + 1, Upper: ^uint64(0)}}})
	if err != nil {
		t.Fatal(err)
	}
	const stamp int64 = 1<<53 + 1
	logical := stamp
	event := &object.CallbackEventOf[int64]{Kind: object.CallbackReceive, InteractionClass: 1, LogicalTime: &logical, Metadata: &core.ObjectCallbackMetadata{Regions: []core.DDMRegionHandleCore{7}, ConveyedRegions: []core.ConveyedRegionCopy{{Designator: 8, Realization: snapshot}}}}
	wire, err := encodeTimeEventOf[int64, int64](event)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var restored timeEventSnapshotOf[int64]
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeTimeEventOf[int64, int64](restored)
	if err != nil {
		t.Fatal(err)
	}
	bounds, found := decoded.Metadata.ConveyedRegions[0].Realization.RangeBounds(1)
	if !found || bounds.Lower != 1<<53+1 || bounds.Upper != ^uint64(0) || *decoded.LogicalTime != stamp {
		t.Fatalf("lost exact queued state: %v", bounds)
	}
}

func TestConveyedCallbackCodecPreservesPresentEmpty(t *testing.T) {
	event := &object.CallbackEvent{Kind: object.CallbackReceive, InteractionClass: 1, Metadata: &core.ObjectCallbackMetadata{ReceivedOrder: core.OrderReceive, ConveyedRegions: []core.ConveyedRegionCopy{}}}
	wire, err := encodeTimeEventModeOf[core.LogicalTime, timeSnapshotNumber](event, true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var restored timeEventSnapshot
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeTimeEventModeOf[core.LogicalTime, timeSnapshotNumber](restored, true)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Metadata.ConveyedRegions == nil {
		t.Fatal("present-empty conveyed region set became absent")
	}
}

func TestConveyedCallbackCodecRejectsInvalidIdentity(t *testing.T) {
	snapshot, err := core.NewRegionRealizationSnapshot(7, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, designator := range []core.DDMRegionHandleCore{0, 7} {
		if _, err := cloneCallbackMetadata(&core.ObjectCallbackMetadata{ConveyedRegions: []core.ConveyedRegionCopy{{Designator: designator, Realization: snapshot}}}); err == nil {
			t.Fatalf("accepted invalid copied designator %v", designator)
		}
	}
	if _, err := cloneCallbackMetadata(&core.ObjectCallbackMetadata{AttributeConveyedRegions: map[core.AttributeHandle][]core.ConveyedRegionCopy{1: {{Designator: 8, Realization: snapshot}}}}); err == nil {
		t.Fatal("accepted per-attribute copies absent from conveyed union")
	}
}
