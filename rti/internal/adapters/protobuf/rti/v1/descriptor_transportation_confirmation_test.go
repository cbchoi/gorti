package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestTransportationConfirmationWireShape(t *testing.T) {
	field := (&FederateEvent{}).ProtoReflect().Descriptor().Fields().ByName("transportation_change_confirmed")
	if field == nil || field.Number() != 72 || field.Kind() != protoreflect.MessageKind ||
		field.Message().FullName() != "rti.v1.TransportationChange" || field.ContainingOneof().Name() != "event" {
		t.Fatalf("missing transportation confirmation oneof tag72: %v", field)
	}
}

func TestTransportationConfirmationWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "8d37cdc552aebe505bdebc9f72b9d6b5aa29cfb13729210f446fef91d0443fcf", rpcOwnershipCallbackGoldenSHA256, projectBeforeTransportationConfirmation)
}

func projectBeforeTransportationConfirmation(file *descriptorpb.FileDescriptorProto) {
	if file.GetName() != streamProtoFile {
		return
	}
	for _, message := range file.MessageType {
		if message.GetName() == federateEventMessageName {
			message.Field = slices.DeleteFunc(message.Field, func(field *descriptorpb.FieldDescriptorProto) bool {
				return field.GetName() == "transportation_change_confirmed" && field.GetNumber() == 72
			})
		}
	}
}
