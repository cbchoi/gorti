package grpc

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestSaveRestoreFailureReasonSurvivesWire(t *testing.T) {
	check := func(message proto.Message, reason int32) {
		t.Helper()
		reflection := message.ProtoReflect()
		field := reflection.Descriptor().Fields().ByName("reason")
		if field == nil || field.Number() != 2 || !field.HasPresence() || field.Kind() != protoreflect.Int32Kind {
			t.Fatal("failure callback must carry the optional reason in field 2")
		}
		if !reflection.Has(field) || reflection.Get(field).Int() != int64(reason) {
			t.Fatalf("lost failure reason %d: %v", reason, message)
		}
		encoded, err := proto.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		cloned := reflection.New().Interface()
		if err := proto.Unmarshal(encoded, cloned); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(message, cloned) {
			t.Fatal("failure reason changed in round trip")
		}
	}
	for reason := savepoint.RTIUnableToSave; reason <= savepoint.SaveAborted; reason++ {
		event, err := EncodeOutboundEvent(&savepoint.NotSavedEvent{Reason: reason})
		if err != nil {
			t.Fatal(err)
		}
		check(event.GetSaveFailed(), int32(reason))
	}
	for reason := savepoint.RTIUnableToRestore; reason <= savepoint.RestoreAborted; reason++ {
		event, err := EncodeOutboundEvent(&savepoint.NotRestoredEvent{Reason: reason})
		if err != nil {
			t.Fatal(err)
		}
		check(event.GetRestoreFailed(), int32(reason))
	}
}

func TestSaveRestoreFailureReasonLegacyPresence(t *testing.T) {
	for _, message := range []proto.Message{
		&rtiv1.FederationNotSaved{Label: "legacy-save"},
		&rtiv1.FederationNotRestored{Label: "legacy-restore"},
	} {
		t.Run(string(message.ProtoReflect().Descriptor().Name()), func(t *testing.T) {
			reflection := message.ProtoReflect()
			field := reflection.Descriptor().Fields().ByName("reason")
			if field == nil {
				t.Fatal("failure callback reason field missing")
			}
			check := func(present bool) {
				t.Helper()
				encoded, err := proto.Marshal(message)
				if err != nil {
					t.Fatal(err)
				}
				decoded := reflection.New().Interface()
				if err := proto.Unmarshal(encoded, decoded); err != nil {
					t.Fatal(err)
				}
				if decoded.ProtoReflect().Has(field) != present || !proto.Equal(message, decoded) {
					t.Fatalf("failure reason presence changed in round trip: %v", decoded)
				}
			}
			check(false)
			reflection.Set(field, protoreflect.ValueOfInt32(0))
			check(true)
		})
	}
}
