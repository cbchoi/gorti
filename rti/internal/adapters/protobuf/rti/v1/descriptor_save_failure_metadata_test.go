package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestSaveRestoreFailureReasonWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "1bf28fb546d47372f3bc88a12995c87eb33f0943456b6b7dab398375cc3ad6a4", rpcGoldenSHA256, projectBeforeSaveRestoreFailureReasons)
}

// Remove only the two reviewed fields and their proto3-optional synthetic oneofs.
// The previous complete descriptor and all older projection baselines stay exact.
func projectBeforeSaveRestoreFailureReasons(file *descriptorpb.FileDescriptorProto) {
	projectBeforeJavaCompletion(file)
	if file.GetName() != streamProtoFile {
		return
	}
	for _, message := range file.MessageType {
		if message.GetName() != "FederationNotSaved" && message.GetName() != "FederationNotRestored" {
			continue
		}
		message.Field = slices.DeleteFunc(message.Field, func(field *descriptorpb.FieldDescriptorProto) bool {
			return field.GetNumber() == 2 && field.GetName() == "reason"
		})
		message.OneofDecl = slices.DeleteFunc(message.OneofDecl, func(oneof *descriptorpb.OneofDescriptorProto) bool {
			return oneof.GetName() == "_reason"
		})
	}
}

func TestSaveRestoreFailureReasonWireFieldPresence(t *testing.T) {
	for _, message := range []protoreflect.MessageDescriptor{
		(&FederationNotSaved{}).ProtoReflect().Descriptor(),
		(&FederationNotRestored{}).ProtoReflect().Descriptor(),
	} {
		t.Run(string(message.Name()), func(t *testing.T) {
			field := message.Fields().ByName("reason")
			if field == nil || field.Number() != 2 || field.Kind() != protoreflect.Int32Kind || field.IsList() || !field.HasPresence() || !field.HasOptionalKeyword() {
				t.Fatalf("failure reason must remain an optional scalar int32 at tag 2: %v", field)
			}
			oneof := field.ContainingOneof()
			if oneof == nil || !oneof.IsSynthetic() || oneof.Name() != "_reason" || oneof.Fields().Len() != 1 {
				t.Fatalf("failure reason must retain its proto3-optional synthetic oneof: %v", oneof)
			}
		})
	}
}
