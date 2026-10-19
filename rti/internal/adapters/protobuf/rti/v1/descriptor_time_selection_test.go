package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestTimeSelectionWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "4732fe65bcbb935f49b3dc98e4b6b7c3f08ec0ae13f8917db999684204aef136", rpcCurrentGoldenSHA256, projectBeforeTimeSelection)
}

func projectBeforeTimeSelection(file *descriptorpb.FileDescriptorProto) {
	projectBeforeLogicalTimeValue(file)
	if file.GetName() != federationProtoFile {
		return
	}
	numbers := map[string]int32{"CreateFederationRequest": 9, "CreateFederationResponse": 3, "JoinFederationResponse": 6, "FederationSummary": 5}
	for _, message := range file.MessageType {
		if number, ok := numbers[message.GetName()]; ok {
			message.Field = slices.DeleteFunc(message.Field, func(field *descriptorpb.FieldDescriptorProto) bool {
				return field.GetNumber() == number && field.GetName() == "logical_time_implementation_name"
			})
		}
	}
}

func TestTimeSelectionWireFieldShapes(t *testing.T) {
	for _, tc := range []struct {
		message protoreflect.MessageDescriptor
		number  protoreflect.FieldNumber
	}{
		{(&CreateFederationRequest{}).ProtoReflect().Descriptor(), 9},
		{(&CreateFederationResponse{}).ProtoReflect().Descriptor(), 3},
		{(&JoinFederationResponse{}).ProtoReflect().Descriptor(), 6},
		{(&FederationSummary{}).ProtoReflect().Descriptor(), 5},
	} {
		field := tc.message.Fields().ByName("logical_time_implementation_name")
		if field == nil || field.Number() != tc.number || field.Kind() != protoreflect.StringKind || field.IsList() {
			t.Fatalf("unexpected representation field: %v", field)
		}
	}
}
