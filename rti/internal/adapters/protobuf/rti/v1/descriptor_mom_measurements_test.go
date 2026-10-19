package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestMOMMeasurementWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "0c8f23b32fca2dc3732e7ea8139b5dc878c14483820336e92870990f352e06d7", "fd99e5c36f80c1455fa366405701f2921fe7a2eb9ab7845b68552afd51b70532", projectBeforeMOMMeasurements)
}

func projectBeforeMOMMeasurements(file *descriptorpb.FileDescriptorProto) {
	projectBeforeSelectedMIM(file)
	if file.GetName() != "rti/v1/mom.proto" {
		return
	}
	for _, m := range file.MessageType {
		var fields []int32
		switch m.GetName() {
		case "QueryFederationAttributesResponse":
			fields = []int32{10, 11}
		case "QueryFederateAttributesResponse":
			fields = []int32{30, 31}
		default:
			continue
		}
		m.Field = slices.DeleteFunc(m.Field, func(f *descriptorpb.FieldDescriptorProto) bool { return slices.Contains(fields, f.GetNumber()) })
		m.NestedType = slices.DeleteFunc(m.NestedType, func(n *descriptorpb.DescriptorProto) bool {
			return n.GetName() == "AttributeValuesEntry" || n.GetName() == "UnavailableAttributesEntry"
		})
	}
}

func TestMOMMeasurementMapInvariants(t *testing.T) {
	for _, tc := range []struct {
		message         protoreflect.MessageDescriptor
		values, reasons protoreflect.FieldNumber
	}{
		{(&QueryFederationAttributesResponse{}).ProtoReflect().Descriptor(), 10, 11},
		{(&QueryFederateAttributesResponse{}).ProtoReflect().Descriptor(), 30, 31},
	} {
		for _, field := range []struct {
			name   protoreflect.Name
			number protoreflect.FieldNumber
			value  protoreflect.Kind
		}{{"attribute_values", tc.values, protoreflect.BytesKind}, {"unavailable_attributes", tc.reasons, protoreflect.StringKind}} {
			f := tc.message.Fields().ByName(field.name)
			if f == nil || f.Number() != field.number || !f.IsMap() || f.MapKey().Kind() != protoreflect.StringKind || f.MapValue().Kind() != field.value {
				t.Fatalf("%s.%s: %v", tc.message.Name(), field.name, f)
			}
		}
	}
}
