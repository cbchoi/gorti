package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestDDMConveyedRegionsPersistenceWireShape(t *testing.T) {
	field := (&DDMManagerState{}).ProtoReflect().Descriptor().Fields().ByName("conveyed_regions")
	if field == nil || field.Number() != 7 || field.Cardinality() != protoreflect.Repeated || field.Kind() != protoreflect.MessageKind || field.Message().FullName() != "rti.v1.DDMConveyedRegionEntry" {
		t.Fatalf("missing copied-region persistence tag7: %v", field)
	}
	for _, spec := range []struct {
		message string
		name    string
		tag     protoreflect.FieldNumber
		kind    protoreflect.Kind
	}{
		{"DDMConveyedRegionEntry", "designator", 1, protoreflect.Uint64Kind},
		{"DDMConveyedRegionEntry", "source_region_handle", 2, protoreflect.Uint64Kind},
		{"DDMConveyedRegionEntry", "realization", 3, protoreflect.MessageKind},
		{"DDMConveyedRegionEntry", "recipients", 4, protoreflect.MessageKind},
		{"DDMConveyedRegionRecipientEntry", "federate_handle", 1, protoreflect.Uint64Kind},
		{"DDMConveyedRegionRecipientEntry", "active", 2, protoreflect.BoolKind},
	} {
		message := File_rti_v1_ddm_proto.Messages().ByName(protoreflect.Name(spec.message))
		if message == nil {
			t.Fatalf("missing %s", spec.message)
		}
		got := message.Fields().ByName(protoreflect.Name(spec.name))
		if got == nil || got.Number() != spec.tag || got.Kind() != spec.kind {
			t.Fatalf("%s.%s: %v", spec.message, spec.name, got)
		}
	}
}

func TestDDMConveyedRegionsHistory44Preserves43(t *testing.T) {
	checkDescriptorGolden(t, "cca85ee814fa5c39a39d93f54aca749c2cb05a9646df4f68f633c8783b61d8c8", rpcOwnershipCallbackGoldenSHA256, projectBeforeDDMConveyedRegions)
}

func projectBeforeDDMConveyedRegions(file *descriptorpb.FileDescriptorProto) {
	if file.GetName() != ddmProtoFile {
		return
	}
	file.MessageType = slices.DeleteFunc(file.MessageType, func(m *descriptorpb.DescriptorProto) bool {
		return m.GetName() == "DDMConveyedRegionEntry" || m.GetName() == "DDMConveyedRegionRecipientEntry"
	})
	for _, message := range file.MessageType {
		if message.GetName() == "DDMManagerState" {
			message.Field = slices.DeleteFunc(message.Field, func(f *descriptorpb.FieldDescriptorProto) bool {
				return f.GetName() == "conveyed_regions" && f.GetNumber() == 7
			})
		}
	}
}

const ddmProtoFile = "rti/v1/ddm.proto"
