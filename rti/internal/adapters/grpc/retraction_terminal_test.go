package grpc

import (
	"testing"

	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	"google.golang.org/protobuf/proto"
)

func TestRestoredRetractionHintPresence(t *testing.T) {
	for _, next := range []uint64{0, 1, 92} {
		wire, err := EncodeOutboundEvent(&savepoint.RestoredEvent{FederateHandle: 7, CallbackEpoch: 2, NextRetractionHandle: next})
		if err != nil {
			t.Fatal(err)
		}
		body := wire.GetRestoreCompleted()
		if body == nil || (body.NextRetractionHandle != nil) != (next != 0) || body.GetNextRetractionHandle() != next {
			t.Fatalf("next=%d wire=%v", next, body)
		}
		encoded, err := proto.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		cloned := wire.ProtoReflect().New().Interface()
		if err := proto.Unmarshal(encoded, cloned); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(wire, cloned) {
			t.Fatal("terminal presence lost in wire roundtrip")
		}
	}
}
