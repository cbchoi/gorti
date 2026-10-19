package savebundle

import (
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"reflect"
	"testing"
)

func TestTimeCallbackMetadataRoundTripAndValidation(t *testing.T) {
	ts := core.LogicalTime(4)
	event := &object.CallbackEvent{Kind: object.CallbackReceive, InteractionClass: 1, LogicalTime: &ts, Metadata: &core.ObjectCallbackMetadata{SentOrder: core.OrderTimestamp, ReceivedOrder: core.OrderTimestamp, Transportation: core.TransportTypeReliable, Producer: 2, RetractionHandle: 77, Regions: []core.DDMRegionHandleCore{3}}}
	cloned, err := cloneTimeEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cloned, event) {
		t.Fatalf("metadata lost: %+v", cloned)
	}
	cloned.(*object.CallbackEvent).Metadata.Regions[0] = 9
	if event.Metadata.Regions[0] != 3 {
		t.Fatal("metadata regions aliased")
	}
	event.Metadata.SentOrder = 99
	if _, err := cloneTimeEvent(event); err == nil {
		t.Fatal("invalid saved order accepted")
	}
}
