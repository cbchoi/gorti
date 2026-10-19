package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestLogicalTimeValueWireShape(t *testing.T) {
	message := File_rti_v1_common_proto.Messages().ByName("LogicalTimeValue")
	if message == nil {
		t.Fatal("missing additive LogicalTimeValue message")
	}
	if message.Fields().Len() != 2 || message.Oneofs().Len() != 1 || message.Oneofs().Get(0).Name() != "value" {
		t.Fatal("expected exactly one value selector with two alternatives")
	}
	for _, tc := range []struct {
		name   protoreflect.Name
		number protoreflect.FieldNumber
		kind   protoreflect.Kind
	}{
		{"float64_value", 1, protoreflect.DoubleKind},
		{"integer64_value", 2, protoreflect.Sint64Kind},
	} {
		field := message.Fields().ByName(tc.name)
		if field == nil || field.Number() != tc.number || field.Kind() != tc.kind || field.IsList() || field.ContainingOneof() != message.Oneofs().Get(0) {
			t.Fatalf("unexpected selector %s: %v", tc.name, field)
		}
	}
}

func TestLogicalTimeValueWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "cb296e4132f995969ea299ad0d075576ce5e1f265f4ae358f4db729492703aa4", rpcCurrentGoldenSHA256, projectBeforeLogicalTimeValue)
}

func projectBeforeLogicalTimeValue(file *descriptorpb.FileDescriptorProto) {
	projectBeforeDDMInactiveAssociations(file)
	if file.GetName() == "rti/v1/common.proto" {
		file.MessageType = slices.DeleteFunc(file.MessageType, func(message *descriptorpb.DescriptorProto) bool {
			return message.GetName() == "LogicalTimeValue"
		})
	}
}
