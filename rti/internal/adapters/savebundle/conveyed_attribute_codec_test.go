package savebundle

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestConveyedReflectionCodecPreservesDetachedAttributeProjection(t *testing.T) {
	t.Run("Float", func(t *testing.T) { testConveyedReflectionCodec[core.LogicalTime, timeSnapshotNumber](t, 10) })
	t.Run("Integer", func(t *testing.T) { testConveyedReflectionCodec[int64, int64](t, 1<<53+17) })
}

func testConveyedReflectionCodec[T core.LogicalTimeScalar, N core.LogicalTimeScalar](t *testing.T, stamp T) {
	t.Helper()
	var copies []core.ConveyedRegionCopy
	for i, source := range []core.DDMRegionHandleCore{101, 102} {
		snapshot, err := core.NewRegionRealizationSnapshot(source, []core.RegionDimensionRange{{Dimension: 1, Bounds: core.DDMRange{Lower: 1<<53 + uint64(i+1), Upper: ^uint64(0)}}})
		if err != nil {
			t.Fatal(err)
		}
		copies = append(copies, core.ConveyedRegionCopy{Designator: source + 100, Realization: snapshot})
	}
	metadata := &core.ObjectCallbackMetadata{Regions: []core.DDMRegionHandleCore{101, 102}, ConveyedRegions: copies,
		AttributeConveyedRegions: map[core.AttributeHandle][]core.ConveyedRegionCopy{12: {copies[0]}, 27: {copies[1]}}}
	event := &object.CallbackEventOf[T]{Kind: object.CallbackReflect, Object: 5, ObjectClass: 7, Attributes: map[uint64][]byte{12: {1}, 27: {2}}, LogicalTime: &stamp, Metadata: metadata}
	wire, err := encodeTimeEventOf[T, N](event)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	// Encoding a queued event must detach maps and wrappers before persistence.
	metadata.AttributeConveyedRegions[12][0].Designator = 999
	metadata.ConveyedRegions[0].Designator = 999
	var restored timeEventSnapshotOf[N]
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeTimeEventOf[T, N](restored)
	if err != nil {
		t.Fatal(err)
	}
	if *decoded.LogicalTime != stamp || decoded.ObjectClass != 7 || !reflect.DeepEqual(decoded.Attributes, event.Attributes) {
		t.Fatal("codec changed timestamp, known class or attribute payload")
	}
	for attr, source := range map[core.AttributeHandle]core.DDMRegionHandleCore{12: 101, 27: 102} {
		mapped := decoded.Metadata.AttributeConveyedRegions[attr]
		if len(mapped) != 1 || mapped[0].Designator != source+100 || mapped[0].Realization.SourceRegion() != source {
			t.Fatalf("known attribute %v lost provenance: %+v", attr, mapped)
		}
		bounds, found := mapped[0].Realization.RangeBounds(1)
		if !found || bounds.Lower != 1<<53+uint64(source-100) || bounds.Upper != ^uint64(0) {
			t.Fatalf("attribute %v lost exact bounds: %+v", attr, bounds)
		}
	}
	if wire.Metadata.ConveyedRegions[0].Designator != 201 || wire.Metadata.AttributeConveyedRegions[12][0].Designator != 201 {
		t.Fatal("encoded queue snapshot aliases source metadata")
	}
	decoded.Metadata.AttributeConveyedRegions[12][0].Designator = 888
	if restored.Metadata.AttributeConveyedRegions[12][0].Designator != 201 {
		t.Fatal("decoded callback aliases saved state needed for retry")
	}
}
