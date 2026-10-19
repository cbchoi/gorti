package rtiv1

import (
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"slices"
	"testing"
)

func TestCallbackInvocationEntryCapabilityWireShape(t *testing.T) {
	field := (&JoinFederationResponse{}).ProtoReflect().Descriptor().Fields().ByName("callback_invocation_entry_supported")
	if field == nil || field.Number() != 7 || field.Kind() != protoreflect.BoolKind {
		t.Fatalf("missing entry capability tag7: %v", field)
	}
	if (&JoinFederationResponse{}).ProtoReflect().Get(field).Bool() {
		t.Fatal("legacy server advertised entry")
	}
}

func TestCallbackInvocationEntryCapabilityWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "20038e182cd5e540652417aa00f81065ed43989198245e73a56fdf735019deb7", rpcOwnershipCallbackGoldenSHA256, projectBeforeCallbackInvocationEntryCapability)
}

func projectBeforeCallbackInvocationEntryCapability(file *descriptorpb.FileDescriptorProto) {
	if file.GetName() != federationProtoFile {
		return
	}
	for _, message := range file.MessageType {
		if message.GetName() == "JoinFederationResponse" {
			message.Field = slices.DeleteFunc(message.Field, func(field *descriptorpb.FieldDescriptorProto) bool {
				return field.GetName() == "callback_invocation_entry_supported" && field.GetNumber() == 7
			})
		}
	}
}

const federationProtoFile = "rti/v1/federation.proto"
