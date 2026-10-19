package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestRetractionWatermarkWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "84107e635b0ed76af7809dbb7be28afef9d7869c6951bc076c593056c08e3639", "667962c787127aee334ca438ea899e846487c6b060fd4c4a89d18615880d21ed", projectBeforeRetractionWatermark)
}

func projectBeforeRetractionWatermark(file *descriptorpb.FileDescriptorProto) {
	projectBeforeHydrationAndRemainingServices(file)
	if file.GetName() != streamProtoFile {
		return
	}
	for _, message := range file.MessageType {
		if message.GetName() != "FederationRestored" {
			continue
		}
		message.Field = slices.DeleteFunc(message.Field, func(field *descriptorpb.FieldDescriptorProto) bool { return field.GetNumber() == 4 })
		message.OneofDecl = slices.DeleteFunc(message.OneofDecl, func(oneof *descriptorpb.OneofDescriptorProto) bool {
			return oneof.GetName() == "_next_retraction_handle"
		})
	}
}

func TestRetractionWatermarkWireFieldPresence(t *testing.T) {
	field := (&FederationRestored{}).ProtoReflect().Descriptor().Fields().ByName("next_retraction_handle")
	if field == nil || field.Number() != 4 || field.Kind() != protoreflect.Uint64Kind || !field.HasPresence() || field.IsList() {
		t.Fatalf("optional restored retraction allocation hint missing or incompatible: %v", field)
	}
}
