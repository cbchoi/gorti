package rtiv1

import (
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"slices"
	"testing"
)

func TestDDMRegionTemplateWireShape(t *testing.T) {
	field := (&DDMRegionEntry{}).ProtoReflect().Descriptor().Fields().ByName("template")
	if field == nil || field.Number() != 7 || field.Kind() != protoreflect.BoolKind {
		t.Fatalf("missing region template tag7: %v", field)
	}
	if (&DDMRegionEntry{}).ProtoReflect().Get(field).Bool() {
		t.Fatal("legacy region became template")
	}
}

func TestDDMRegionTemplateWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "3a4eeb6a6bdc0b834757b5c5661cc94ebf54a5fbd7cb680853cebd46fb3f71ce", rpcOwnershipCallbackGoldenSHA256, projectBeforeDDMRegionTemplate)
}

func projectBeforeDDMRegionTemplate(file *descriptorpb.FileDescriptorProto) {
	if file.GetName() != ddmProtoFile {
		return
	}
	for _, message := range file.MessageType {
		if message.GetName() == "DDMRegionEntry" {
			message.Field = slices.DeleteFunc(message.Field, func(field *descriptorpb.FieldDescriptorProto) bool {
				return field.GetName() == "template" && field.GetNumber() == 7
			})
		}
	}
}
