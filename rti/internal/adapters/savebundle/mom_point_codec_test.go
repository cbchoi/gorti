package savebundle

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestMOMPointCodecDetachedMetadata(t *testing.T) {
	metadata := &core.ObjectCallbackMetadata{SentOrder: core.OrderReceive, ReceivedOrder: core.OrderReceive, MOMAttributePoint: &core.MOMAttributePoint{Subject: 1<<53 + 7, Dimension: 3, Coordinate: 7}}
	cloned, err := cloneCallbackMetadata(metadata)
	if err != nil {
		t.Fatal(err)
	}
	cloned.MOMAttributePoint.Coordinate = 9
	if metadata.MOMAttributePoint.Coordinate != 7 {
		t.Fatal("MOM point aliases source metadata")
	}
}

func TestMOMPointCodecMalformedMetadata(t *testing.T) {
	for name, point := range map[string]core.MOMAttributePoint{
		"zero subject":     {Dimension: 3},
		"zero dimension":   {Subject: 7, Coordinate: 7},
		"wrong coordinate": {Subject: 7, Dimension: 3, Coordinate: 8},
		"internal subject": {Subject: ^core.FederateHandle(0), Dimension: 3, Coordinate: uint64(^uint32(0))},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := cloneCallbackMetadata(&core.ObjectCallbackMetadata{SentOrder: core.OrderReceive, ReceivedOrder: core.OrderReceive, MOMAttributePoint: &point}); err == nil {
				t.Fatal("malformed point admitted")
			}
		})
	}
}

func testMOMPointEventRoundTrip[T, N core.LogicalTimeScalar](t *testing.T) {
	t.Helper()
	event := &object.CallbackEventOf[T]{Kind: object.CallbackReflect, Object: 7, ObjectClass: 1, Metadata: &core.ObjectCallbackMetadata{SentOrder: core.OrderReceive, ReceivedOrder: core.OrderReceive, MOMAttributePoint: &core.MOMAttributePoint{Subject: 1<<53 + 7, Dimension: 3, Coordinate: 7}}}
	wire, err := encodeTimeEventModeOf[T, N](event, true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var saved timeEventSnapshotOf[N]
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeTimeEventModeOf[T, N](saved, true)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.LogicalTime != nil || decoded.Metadata.MOMAttributePoint == nil || *decoded.Metadata.MOMAttributePoint != *event.Metadata.MOMAttributePoint {
		t.Fatal("queued MOM point or receive order lost")
	}
	decoded.Metadata.MOMAttributePoint.Subject = 9
	if saved.Metadata.MOMAttributePoint.Subject != 1<<53+7 {
		t.Fatal("decoded point aliases saved state")
	}
}

func TestMOMPointCodecBothTimeFamilies(t *testing.T) {
	t.Run("Float64", func(t *testing.T) { testMOMPointEventRoundTrip[core.LogicalTime, timeSnapshotNumber](t) })
	t.Run("Integer64", func(t *testing.T) { testMOMPointEventRoundTrip[int64, integerSnapshotNumber](t) })
}

func TestMOMPointCodecIdentityRebind(t *testing.T) {
	metadata := &core.ObjectCallbackMetadata{SentOrder: core.OrderReceive, ReceivedOrder: core.OrderReceive, MOMAttributePoint: &core.MOMAttributePoint{Subject: 7, Dimension: 7, Coordinate: 7}}
	if err := rewriteTypedFederates(reflect.ValueOf(metadata), func(h core.FederateHandle) (core.FederateHandle, error) { return h + 100, nil }); err != nil {
		t.Fatal(err)
	}
	if got := metadata.MOMAttributePoint; got.Subject != 107 || got.Coordinate != 107 || got.Dimension != 7 {
		t.Fatalf("point identity rebind corrupted geometry or coordinate: %+v", got)
	}
	if _, err := cloneCallbackMetadata(metadata); err != nil {
		t.Fatalf("rebound point rejected: %v", err)
	}
}
