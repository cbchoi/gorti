package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestJavaCompletionWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "ffe766aa9ce480025bb8f679881ff160d6b7db4d4b98a13500f3e56ada42bc4b", rpcGoldenSHA256, projectBeforeJavaCompletion)
}

func projectBeforeJavaCompletion(file *descriptorpb.FileDescriptorProto) {
	projectBeforeTimeSelection(file)
	var additions map[string]int32
	switch file.GetName() {
	case objectProtoFile:
		file.MessageType = slices.DeleteFunc(file.MessageType, func(message *descriptorpb.DescriptorProto) bool {
			return message.GetName() == "TransportationChange" || message.GetName() == "ActivateTransportationTypeChangeRequest"
		})
		for _, service := range file.Service {
			service.Method = slices.DeleteFunc(service.Method, func(method *descriptorpb.MethodDescriptorProto) bool {
				return method.GetName() == "PrepareAttributeTransportationTypeChange" || method.GetName() == "PrepareInteractionTransportationTypeChange" || method.GetName() == "ActivateTransportationTypeChange"
			})
		}
	case ddmProtoFile:
		additions = map[string]int32{"RegisterObjectWithRegionsRequest": 7, "SubscribeOCAWithRegionsRequest": 9, "AssociateRegionsForUpdatesRequest": 6,
			"UnassociateRegionsForUpdatesRequest": 6, "UnsubscribeOCAWithRegionsRequest": 7, "RequestAttributeValueUpdateWithRegionsRequest": 8}
		file.MessageType = slices.DeleteFunc(file.MessageType, func(message *descriptorpb.DescriptorProto) bool {
			return message.GetName() == "AttributeRegionGroup" || message.GetName() == "AttributeRegionGroups"
		})
	case "rti/v1/savepoint.proto":
		additions = map[string]int32{"FederateSaveResponseRequest": 4}
	case streamProtoFile:
		additions = map[string]int32{"InitiateFederateRestore": 4}
	}
	for _, message := range file.MessageType {
		if number, ok := additions[message.GetName()]; ok {
			message.Field = slices.DeleteFunc(message.Field, func(field *descriptorpb.FieldDescriptorProto) bool {
				return field.GetNumber() == number && (field.GetName() == "client_state" || field.GetName() == "attribute_region_groups")
			})
		}
	}
}

func TestJavaCompletionWireFieldShapes(t *testing.T) {
	for _, message := range []protoreflect.MessageDescriptor{
		(&FederateSaveResponseRequest{}).ProtoReflect().Descriptor(), (&InitiateFederateRestore{}).ProtoReflect().Descriptor(),
	} {
		field := message.Fields().ByName("client_state")
		if field == nil || field.Number() != 4 || field.Kind() != protoreflect.BytesKind || field.IsList() {
			t.Fatalf("invalid private runtime checkpoint field: %v", field)
		}
	}
	for _, item := range []struct {
		message protoreflect.MessageDescriptor
		number  protoreflect.FieldNumber
	}{
		{(&SubscribeOCAWithRegionsRequest{}).ProtoReflect().Descriptor(), 9},
		{(&RegisterObjectWithRegionsRequest{}).ProtoReflect().Descriptor(), 7},
		{(&AssociateRegionsForUpdatesRequest{}).ProtoReflect().Descriptor(), 6},
		{(&UnassociateRegionsForUpdatesRequest{}).ProtoReflect().Descriptor(), 6},
		{(&UnsubscribeOCAWithRegionsRequest{}).ProtoReflect().Descriptor(), 7},
		{(&RequestAttributeValueUpdateWithRegionsRequest{}).ProtoReflect().Descriptor(), 8},
	} {
		field := item.message.Fields().ByName("attribute_region_groups")
		if field == nil || field.Number() != item.number || field.Kind() != protoreflect.MessageKind || field.IsList() || !field.HasPresence() || field.Message().FullName() != "rti.v1.AttributeRegionGroups" {
			t.Fatalf("grouped requests must distinguish absent from explicit empty: %v", field)
		}
	}
}

func TestTransportationActivationWireIdentity(t *testing.T) {
	for _, item := range []struct {
		message protoreflect.MessageDescriptor
		name    protoreflect.Name
		number  protoreflect.FieldNumber
		kind    protoreflect.Kind
	}{
		{(&TransportationChange{}).ProtoReflect().Descriptor(), "ticket", 1, protoreflect.BytesKind},
		{(&TransportationChange{}).ProtoReflect().Descriptor(), "federation_generation", 2, protoreflect.Uint64Kind},
		{(&TransportationChange{}).ProtoReflect().Descriptor(), "callback_epoch", 3, protoreflect.Uint64Kind},
		{(&ActivateTransportationTypeChangeRequest{}).ProtoReflect().Descriptor(), "ticket", 4, protoreflect.BytesKind},
		{(&ActivateTransportationTypeChangeRequest{}).ProtoReflect().Descriptor(), "expected_federation_generation", 5, protoreflect.Uint64Kind},
		{(&ActivateTransportationTypeChangeRequest{}).ProtoReflect().Descriptor(), "expected_callback_epoch", 6, protoreflect.Uint64Kind},
	} {
		field := item.message.Fields().ByName(item.name)
		if field == nil || field.Number() != item.number || field.Kind() != item.kind || field.IsList() {
			t.Fatalf("invalid transportation activation identity field: %s.%s = %v", item.message.FullName(), item.name, field)
		}
	}
}
