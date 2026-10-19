package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestDDMInactiveAssociationWireShape(t *testing.T) {
	entry := File_rti_v1_ddm_proto.Messages().ByName("DDMInactiveAssociationEntry")
	if entry == nil {
		t.Fatal("missing additive DDMInactiveAssociationEntry")
	}
	if entry.Fields().Len() != 4 {
		t.Fatal("expected four inactive association fields")
	}
	for i, name := range []protoreflect.Name{"object_handle", "attribute_handle", "associating_federate", "region_handles"} {
		field := entry.Fields().ByName(name)
		if field == nil || field.Number() != protoreflect.FieldNumber(i+1) || field.Kind() != protoreflect.Uint64Kind || field.IsList() != (i == 3) {
			t.Fatalf("unexpected inactive association field %s: %v", name, field)
		}
	}
	state := File_rti_v1_ddm_proto.Messages().ByName("DDMManagerState")
	field := state.Fields().ByName("inactive_associations")
	if field == nil || field.Number() != 6 || !field.IsList() || field.Kind() != protoreflect.MessageKind || field.Message() != entry {
		t.Fatalf("unexpected inactive association state field: %v", field)
	}
}

func TestDDMInactiveAssociationWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "90f891e99224d8abc10c38b949e36674307d9138ee22467800aad2daae3f74f6", rpcCurrentGoldenSHA256, projectBeforeDDMInactiveAssociations)
}

func projectBeforeDDMInactiveAssociations(file *descriptorpb.FileDescriptorProto) {
	projectBeforeExactTimePairs(file)
	if file.GetName() != ddmProtoFile {
		return
	}
	file.MessageType = slices.DeleteFunc(file.MessageType, func(message *descriptorpb.DescriptorProto) bool {
		return message.GetName() == "DDMInactiveAssociationEntry"
	})
	for _, message := range file.MessageType {
		if message.GetName() == "DDMManagerState" {
			message.Field = slices.DeleteFunc(message.Field, func(field *descriptorpb.FieldDescriptorProto) bool {
				return field.GetNumber() == 6 && field.GetName() == "inactive_associations"
			})
		}
	}
}
