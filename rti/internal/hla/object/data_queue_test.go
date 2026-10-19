package object

import (
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func checkDataQueueMarker[T core.LogicalTimeScalar](t *testing.T) {
	t.Helper()
	for _, kind := range []CallbackKind{CallbackReflect, CallbackReceive, CallbackRemove} {
		for _, order := range []core.OrderType{core.OrderReceive, core.OrderTimestamp, core.OrderUnspecified} {
			event := &CallbackEventOf[T]{Kind: kind, Metadata: &core.ObjectCallbackMetadata{ReceivedOrder: order}}
			if got, data := event.DataReceivedOrder(); got != order || !data {
				t.Fatalf("kind=%d order=%d: got %d data=%t", kind, order, got, data)
			}
		}
		if got, data := (&CallbackEventOf[T]{Kind: kind}).DataReceivedOrder(); got != core.OrderUnspecified || !data {
			t.Fatal("missing data metadata fabricated classification")
		}
	}
	for kind := CallbackDiscover; kind <= CallbackDeleteMarker; kind++ {
		if kind == CallbackReflect || kind == CallbackReceive || kind == CallbackRemove {
			continue
		}
		if _, data := (&CallbackEventOf[T]{Kind: kind, Metadata: &core.ObjectCallbackMetadata{ReceivedOrder: core.OrderReceive}}).DataReceivedOrder(); data {
			t.Fatalf("control kind %d counted as data", kind)
		}
	}
	var absent *CallbackEventOf[T]
	if _, data := absent.DataReceivedOrder(); data {
		t.Fatal("nil event counted as data")
	}
}

func TestDataQueueMessageMarker(t *testing.T) {
	t.Run("Float64", checkDataQueueMarker[core.LogicalTime])
	t.Run("Integer64", checkDataQueueMarker[int64])
}
