package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestUnpublishAttributeSetPresenceWireShape(t *testing.T) {
	message := (&UnpubObjAttrsRequest{}).ProtoReflect().Descriptor()
	field := message.Fields().ByName("attribute_set_present")
	if field == nil || field.Number() != 6 || field.Kind() != protoreflect.BoolKind || !field.HasPresence() || !field.HasOptionalKeyword() {
		t.Fatalf("missing optional attribute set discriminator at tag 6: %v", field)
	}
	for _, present := range []bool{false, true} {
		request := &UnpubObjAttrsRequest{WireVersion: 1, FederationName: "f", FederateHandle: 3, ObjectClassHandle: 5}
		request.ProtoReflect().Set(field, protoreflect.ValueOfBool(present))
		data, err := proto.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		decoded := &UnpubObjAttrsRequest{}
		if err := proto.Unmarshal(data, decoded); err != nil {
			t.Fatal(err)
		}
		if !decoded.ProtoReflect().Has(field) || decoded.ProtoReflect().Get(field).Bool() != present || len(decoded.GetAttributeHandles()) != 0 {
			t.Fatalf("explicit empty discriminator lost: %v", decoded)
		}
	}
	legacy := &UnpubObjAttrsRequest{}
	if legacy.ProtoReflect().Has(field) {
		t.Fatal("legacy request must omit the discriminator")
	}
}

func TestUnpublishAttributeSetPresenceWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "7b2d6ab9e6b9377bca690047d7d5fda731f347d25d929e7a27117345bf6a5c8b", rpcOwnershipCallbackGoldenSHA256, projectBeforeUnpublishAttributeSetPresence)
}

func projectBeforeUnpublishAttributeSetPresence(file *descriptorpb.FileDescriptorProto) {
	projectBeforeUnsubscribeAttributeSetPresence(file)
	if file.GetName() != declarationProtoFile {
		return
	}
	for _, message := range file.MessageType {
		if message.GetName() != "UnpubObjAttrsRequest" {
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
