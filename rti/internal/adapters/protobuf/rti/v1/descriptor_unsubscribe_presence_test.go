package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestUnsubscribeAttributeSetPresenceWireShape(t *testing.T) {
	field := (&UnsubObjAttrsRequest{}).ProtoReflect().Descriptor().Fields().ByName("attribute_set_present")
	if field == nil || field.Number() != 6 || field.Kind() != protoreflect.BoolKind || !field.HasOptionalKeyword() || !field.HasPresence() {
		t.Fatalf("missing optional attribute-set discriminator at tag 6: %v", field)
	}
	for _, present := range []bool{false, true} {
		request := &UnsubObjAttrsRequest{WireVersion: 1, FederationName: "f", FederateHandle: 2, ObjectClassHandle: 7}
		request.ProtoReflect().Set(field, protoreflect.ValueOfBool(present))
		data, err := proto.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		decoded := &UnsubObjAttrsRequest{}
		if err := proto.Unmarshal(data, decoded); err != nil {
			t.Fatal(err)
		}
		if !decoded.ProtoReflect().Has(field) || decoded.ProtoReflect().Get(field).Bool() != present || len(decoded.AttributeHandles) != 0 {
			t.Fatalf("presence lost: %v", decoded)
		}
	}
	if (&UnsubObjAttrsRequest{}).ProtoReflect().Has(field) {
		t.Fatal("legacy request acquired presence")
	}
}

func TestUnsubscribeAttributeSetPresenceWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "cf525f50338f62cd94b8c4b91da0e13a4b732caa65de1adbe20eb1ea654776f3", rpcOwnershipCallbackGoldenSHA256, projectBeforeUnsubscribeAttributeSetPresence)
}

func projectBeforeUnsubscribeAttributeSetPresence(file *descriptorpb.FileDescriptorProto) {
	if file.GetName() != declarationProtoFile {
		return
	}
	for _, message := range file.MessageType {
		if message.GetName() != "UnsubObjAttrsRequest" {
			continue
		}
		message.Field = slices.DeleteFunc(message.Field, func(field *descriptorpb.FieldDescriptorProto) bool {
			return field.GetName() == "attribute_set_present" && field.GetNumber() == 6
		})
		message.OneofDecl = slices.DeleteFunc(message.OneofDecl, func(oneof *descriptorpb.OneofDescriptorProto) bool {
			return oneof.GetName() == "_attribute_set_present"
		})
	}
}
