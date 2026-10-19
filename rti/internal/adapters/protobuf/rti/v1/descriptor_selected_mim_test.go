package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestSelectedMIMWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "f17fb42299c07a885b91a8458d5b2cddd76c913563d8808aa5fde58360591ace", rpcGoldenSHA256, projectBeforeSelectedMIM)
}

func projectBeforeSelectedMIM(file *descriptorpb.FileDescriptorProto) {
	projectBeforeTimeEnableCallbacks(file)
	if file.GetName() == "rti/v1/common.proto" {
		for _, enum := range file.EnumType {
			if enum.GetName() == "WireVersion" {
				enum.Value = slices.DeleteFunc(enum.Value, func(v *descriptorpb.EnumValueDescriptorProto) bool {
					return v.GetName() == "WIRE_VERSION_V2" && v.GetNumber() == 2
				})
			}
		}
	}
	if file.GetName() != federationProtoFile {
		return
	}
	for _, message := range file.MessageType {
		if message.GetName() != "CreateFederationRequest" && message.GetName() != "FOMView" && message.GetName() != "JoinFederationRequest" {
			continue
		}
		message.Field = slices.DeleteFunc(message.Field, func(field *descriptorpb.FieldDescriptorProto) bool {
			return field.GetName() == "mim_module" || field.GetName() == "expected_mim_module"
		})
	}
}

func TestSelectedMIMFieldsHaveMessagePresence(t *testing.T) {
	for _, tc := range []struct {
		message protoreflect.MessageDescriptor
		name    protoreflect.Name
		number  protoreflect.FieldNumber
	}{
		{(&CreateFederationRequest{}).ProtoReflect().Descriptor(), "mim_module", 8},
		{(&FOMView{}).ProtoReflect().Descriptor(), "mim_module", 7},
		{(&JoinFederationRequest{}).ProtoReflect().Descriptor(), "expected_mim_module", 7},
	} {
		field := tc.message.Fields().ByName(tc.name)
		if field == nil || field.Number() != tc.number || !field.HasPresence() || field.IsList() || field.Message().FullName() != "rti.v1.FOMModule" {
			t.Fatalf("invalid MIM field: %v", field)
		}
	}
}
