package savebundle

import (
	"encoding/json"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestMOMPointMetadataQueuedCodecBothFamilies(t *testing.T) {
	t.Run("Float", func(t *testing.T) { testMOMPointMetadataCodec[core.LogicalTime, timeSnapshotNumber](t) })
	t.Run("Integer", func(t *testing.T) { testMOMPointMetadataCodec[int64, int64](t) })
}

func testMOMPointMetadataCodec[T core.LogicalTimeScalar, N core.LogicalTimeScalar](t *testing.T) {
	t.Helper()
	point := core.MOMAttributePoint{Subject: 211<<32 | 19, Dimension: 7, Coordinate: 19}
	metadata := &core.ObjectCallbackMetadata{SentOrder: core.OrderReceive, ReceivedOrder: core.OrderReceive, MOMAttributePoint: &point}
	event := &object.CallbackEventOf[T]{Kind: object.CallbackReflect, Object: 5, ObjectClass: 7, Attributes: map[uint64][]byte{12: {1}, 27: {2}}, Metadata: metadata}
	encoded, err := encodeTimeEventModeOf[T, N](event, true)
	if err != nil {
		t.Fatal(err)
	}
	want := point
	point.Coordinate = 99
	if encoded.Metadata.MOMAttributePoint == metadata.MOMAttributePoint || *encoded.Metadata.MOMAttributePoint != want {
		t.Fatal("encoded point aliases live callback")
	}
	data, err := json.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var restored timeEventSnapshotOf[N]
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeTimeEventModeOf[T, N](restored, true)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Metadata.MOMAttributePoint == restored.Metadata.MOMAttributePoint || *decoded.Metadata.MOMAttributePoint != want || decoded.ObjectClass != 7 || len(decoded.Attributes) != 2 || decoded.LogicalTime != nil {
		t.Fatal("restored point reflection changed semantics or aliases save data")
	}
	decoded.Metadata.MOMAttributePoint.Coordinate = 100
	if *restored.Metadata.MOMAttributePoint != want {
		t.Fatal("decoded point aliases retry snapshot")
	}
}

func TestMOMPointMetadataRejectsMalformedAndMixedRegionState(t *testing.T) {
	valid := core.MOMAttributePoint{Subject: 15, Dimension: 1, Coordinate: 15}
	for _, point := range []core.MOMAttributePoint{{Dimension: 1}, {Subject: 15, Coordinate: 15}, {Subject: 15, Dimension: 1, Coordinate: 16}} {
		if _, err := cloneCallbackMetadata(&core.ObjectCallbackMetadata{SentOrder: core.OrderReceive, ReceivedOrder: core.OrderReceive, MOMAttributePoint: &point}); err == nil {
			t.Fatalf("malformed point accepted: %+v", point)
		}
	}
	for _, mutate := range []func(*core.ObjectCallbackMetadata){
		func(m *core.ObjectCallbackMetadata) { m.Regions = []core.DDMRegionHandleCore{1} },
		func(m *core.ObjectCallbackMetadata) { m.SentOrder = core.OrderTimestamp },
		func(m *core.ObjectCallbackMetadata) { m.DelayedSubscription = true },
	} {
		metadata := &core.ObjectCallbackMetadata{SentOrder: core.OrderReceive, ReceivedOrder: core.OrderReceive, MOMAttributePoint: &valid}
		mutate(metadata)
		if _, err := cloneCallbackMetadata(metadata); err == nil {
			t.Fatalf("mixed metadata accepted: %+v", metadata)
		}
	}
}

func TestMOMPointMetadataRejectsNonReflectionAndTimestampQueue(t *testing.T) {
	point := core.MOMAttributePoint{Subject: 15, Dimension: 1, Coordinate: 15}
	metadata := &core.ObjectCallbackMetadata{SentOrder: core.OrderReceive, ReceivedOrder: core.OrderReceive, MOMAttributePoint: &point}
	for _, kind := range []string{"receive", "remove", "delete_marker"} {
		if _, err := decodeTimeEventModeOf[core.LogicalTime, timeSnapshotNumber](timeEventSnapshot{Kind: kind, Object: 1, InteractionClass: 1, Metadata: metadata}, true); err == nil {
			t.Fatalf("point attached to %s accepted", kind)
		}
	}
	stamp := timeSnapshotNumber(3)
	if _, err := decodeTimeEventModeOf[core.LogicalTime, timeSnapshotNumber](timeEventSnapshot{Kind: "reflect", Object: 1, LogicalTime: &stamp, Metadata: metadata}, false); err == nil {
		t.Fatal("MOM point accepted in timestamp queue")
	}
}
