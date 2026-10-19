package grpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func controlCheckpointReviewEvent(t *testing.T, name string) *rtiv1.FederateEvent {
	t.Helper()
	event := &rtiv1.FederateEvent{Seq: 1}
	message := event.ProtoReflect()
	field := message.Descriptor().Fields().ByName(protoreflect.Name(name))
	if field == nil {
		t.Fatalf("callback field %s missing", name)
	}
	message.Set(field, message.NewField(field))
	if name == callbackTransportationConfirmed {
		change := message.Get(field).Message()
		change.Set(change.Descriptor().Fields().ByName("interaction_class_handle"), protoreflect.ValueOfUint64(7))
	}
	return event
}

func TestControlCheckpointReviewAllPublicControlsCaptured(t *testing.T) {
	descriptor := (&rtiv1.FederateEvent{}).ProtoReflect().Descriptor()
	fields := descriptor.Oneofs().ByName("event").Fields()
	for i := 0; i < fields.Len(); i++ {
		name := string(fields.Get(i).Name())
		switch name {
		case "discover", "reflect", callbackReceive, "remove", callbackLocalValidationBarrier, callbackHalted:
			continue
		}
		t.Run(name, func(t *testing.T) {
			event := controlCheckpointReviewEvent(t, name)
			transportation := transportationCheckpointInventoryProjection(event)
			if callbackServiceName(event) == "" {
				t.Fatal("public callback lacks invocation accounting identity")
			}
			ledger, err := NewCallbackReceipts(CallbackReceiptOptions{
				Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
				Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			ticket, sent, err := ledger.PrepareRequired(context.Background(), "f", 1, event)
			if err != nil {
				t.Fatal(err)
			}
			sent(true)
			data, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return ledger }, Transportation: transportation}).Marshal("f")
			if err != nil {
				t.Fatal("required public control still cannot be captured", err)
			}
			var image struct {
				Version    uint32 `json:"version"`
				Deliveries []struct {
					Identity string `json:"identity"`
					Phase    string `json:"phase"`
					Event    []byte `json:"event"`
				} `json:"deliveries"`
			}
			if err := json.Unmarshal(data, &image); err != nil {
				t.Fatal(err)
			}
			if image.Version != 2 || len(image.Deliveries) != 1 {
				t.Fatal("public control missing from v2 checkpoint")
			}
			identity := sha256.Sum256(ticket)
			decoded, err := hex.DecodeString(image.Deliveries[0].Identity)
			if err != nil || hex.EncodeToString(decoded) != image.Deliveries[0].Identity || image.Deliveries[0].Phase != callbackPhasePending || !bytes.Equal(decoded, identity[:]) || len(image.Deliveries[0].Event) == 0 {
				t.Fatal("checkpoint lost pending phase, stable invocation identity or original body")
			}
		})
	}
}

func TestControlCheckpointReviewRecoveryWireContract(t *testing.T) {
	field := (&rtiv1.FederateEvent{}).ProtoReflect().Descriptor().Fields().ByName("callback_recovery")
	if field == nil || field.Number() != 6 || field.Kind() != protoreflect.MessageKind {
		t.Fatal("additive callback recovery field 6 is not generated")
	}
	for _, want := range []struct {
		name   protoreflect.Name
		number protoreflect.FieldNumber
		kind   protoreflect.Kind
	}{{"invocation_identity", 1, protoreflect.BytesKind}, {"outcome_only", 2, protoreflect.BoolKind}, {callbackPhaseCompleted, 3, protoreflect.BoolKind}} {
		got := field.Message().Fields().ByName(want.name)
		if got == nil || got.Number() != want.number || got.Kind() != want.kind {
			t.Fatalf("recovery field %s differs from coordinated wire contract", want.name)
		}
	}
}
