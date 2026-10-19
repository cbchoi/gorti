package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestCallbackInvocationEntryWireShape(t *testing.T) {
	field := (&ReportCallbackInvocationRequest{}).ProtoReflect().Descriptor().Fields().ByName("invocation_entry")
	if field == nil || field.Number() != 8 || field.Kind() != protoreflect.BoolKind {
		t.Fatalf("missing additive callback invocation entry at tag 8: %v", field)
	}
	if (&ReportCallbackInvocationRequest{}).ProtoReflect().Get(field).Bool() {
		t.Fatal("legacy callback report became entry")
	}
}

func TestCallbackInvocationEntryWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "add122619c527558fac55e3451db983529c315b8e8c83bc52674f07967590147", rpcOwnershipCallbackGoldenSHA256, projectBeforeCallbackInvocationEntry)
}

func projectBeforeCallbackInvocationEntry(file *descriptorpb.FileDescriptorProto) {
	if file.GetName() != supportProtoFile {
		return
	}
	for _, message := range file.MessageType {
		if message.GetName() == "ReportCallbackInvocationRequest" {
			message.Field = slices.DeleteFunc(message.Field, func(field *descriptorpb.FieldDescriptorProto) bool {
				return field.GetName() == "invocation_entry" && field.GetNumber() == 8
			})
		}
	}
}

const supportProtoFile = "rti/v1/support.proto"
