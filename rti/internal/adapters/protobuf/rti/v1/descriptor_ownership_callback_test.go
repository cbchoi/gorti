package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestOwnershipCallbackProtocolWireShape(t *testing.T) {
	file, err := protoregistry.GlobalFiles.FindFileByPath("rti/v1/callback.proto")
	if err != nil {
		t.Fatal("missing callback protocol:", err)
	}
	service := file.Services().ByName("OwnershipCallbackService")
	if service == nil || service.Methods().Len() != 3 {
		t.Fatal("missing explicit receipt capability protocol")
	}
	for _, name := range []protoreflect.Name{"EnableOwnershipCallbackReceipts", "ReportOwnershipCallbackInvocation", "RetryOwnershipCallbacks"} {
		method := service.Methods().ByName(name)
		input := protoreflect.FullName("rti.v1.OwnershipCallbackScopeRequest")
		if name == "ReportOwnershipCallbackInvocation" {
			input = "rti.v1.ReportOwnershipCallbackInvocationRequest"
		}
		if method == nil || method.Input().FullName() != input || method.Output().FullName() != "rti.v1.Empty" || method.IsStreamingClient() || method.IsStreamingServer() {
			t.Fatalf("invalid callback method: %s", name)
		}
	}
	for messageName, fields := range map[protoreflect.Name][]protoreflect.Name{
		"OwnershipCallbackScopeRequest":            {"wire_version", "federation_name", "federate_handle", "federation_generation", "callback_epoch"},
		"OwnershipCallbackReceipt":                 {"federation_generation", "callback_epoch", "callback_id", "delivery_attempt"},
		"ReportOwnershipCallbackInvocationRequest": {"wire_version", "federation_name", "federate_handle", "receipt"},
	} {
		message := file.Messages().ByName(messageName)
		if message == nil || message.Fields().Len() != len(fields) {
			t.Fatalf("invalid message: %s", messageName)
		}
		for index, name := range fields {
			field := message.Fields().ByName(name)
			kind := protoreflect.Uint64Kind
			if name == "wire_version" {
				kind = protoreflect.EnumKind
			}
			if name == "federation_name" {
				kind = protoreflect.StringKind
			}
			if name == "receipt" {
				kind = protoreflect.MessageKind
			}
			if field == nil || field.Number() != protoreflect.FieldNumber(index+1) || field.Kind() != kind {
				t.Fatalf("invalid field: %s.%s", messageName, name)
			}
			if name == "receipt" && (field.Message().FullName() != "rti.v1.OwnershipCallbackReceipt" || !field.HasPresence()) {
				t.Fatal("invalid receipt presence/type")
			}
		}
	}
	event := File_rti_v1_stream_proto.Messages().ByName(federateEventMessageName)
	receipt := event.Fields().ByName("ownership_receipt")
	epoch := event.Fields().ByName("object_knowledge_epoch")
	cancelled := event.Fields().ByName("ownership_acquisition_cancelled")
	if receipt == nil || receipt.Number() != 4 || receipt.Kind() != protoreflect.MessageKind || receipt.Message().FullName() != "rti.v1.OwnershipCallbackReceipt" || !receipt.HasPresence() || receipt.ContainingOneof() != nil {
		t.Fatal("invalid semantic ownership receipt")
	}
	if epoch == nil || epoch.Number() != 5 || epoch.Kind() != protoreflect.Uint64Kind || epoch.ContainingOneof() != nil {
		t.Fatal("invalid knowledge epoch")
	}
	if cancelled == nil || cancelled.Number() != 35 || cancelled.Kind() != protoreflect.MessageKind || cancelled.Message().FullName() != "rti.v1.ConfirmAttributeOwnershipAcquisitionCancellation" || cancelled.ContainingOneof() == nil || cancelled.ContainingOneof().Name() != "event" {
		t.Fatal("invalid cancellation callback")
	}
	payload := cancelled.Message()
	object := payload.Fields().ByName("object_handle")
	attributes := payload.Fields().ByName("attribute_handles")
	if payload.Fields().Len() != 2 || object == nil || object.Number() != 1 || object.Kind() != protoreflect.Uint64Kind || attributes == nil || attributes.Number() != 2 || attributes.Kind() != protoreflect.Uint64Kind || !attributes.IsList() {
		t.Fatal("invalid cancellation payload")
	}
	fence := File_rti_v1_object_proto.Messages().ByName("LocalDeleteObjectInstanceFence")
	if fence == nil || fence.Fields().Len() != 3 {
		t.Fatal("missing local-delete fence")
	}
	for index, name := range []protoreflect.Name{"retired_knowledge_epoch", "federation_generation", "callback_epoch"} {
		field := fence.Fields().ByName(name)
		if field == nil || field.Number() != protoreflect.FieldNumber(index+1) || field.Kind() != protoreflect.Uint64Kind {
			t.Fatal("invalid local-delete fence field")
		}
	}
	objectService := File_rti_v1_object_proto.Services().ByName("ObjectService")
	method := objectService.Methods().ByName("LocalDeleteObjectInstanceFenced")
	if method == nil || method.Input().FullName() != "rti.v1.LocalDeleteObjectInstanceRequest" || method.Output().FullName() != fence.FullName() {
		t.Fatal("invalid fenced local-delete RPC")
	}
	if objectService.Methods().ByName("LocalDeleteObjectInstance").Output().FullName() != "rti.v1.Empty" {
		t.Fatal("legacy local-delete changed")
	}
}

func TestOwnershipCallbackProtocolWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "33578b7bca0891c77490bc2cd96be9e78ce744d6dae5280f2dcda04d2b1befdf", rpcCurrentGoldenSHA256, projectBeforeOwnershipCallbackProtocol)
}

func projectBeforeOwnershipCallbackProtocol(file *descriptorpb.FileDescriptorProto) {
	projectBeforeUnpublishAttributeSetPresence(file)
	if file.GetName() == "rti/v1/callback.proto" {
		file.Reset()
		return
	}
	if file.GetName() == streamProtoFile {
		file.Dependency = slices.DeleteFunc(file.Dependency, func(name string) bool { return name == "rti/v1/callback.proto" })
		file.MessageType = slices.DeleteFunc(file.MessageType, func(message *descriptorpb.DescriptorProto) bool {
			return message.GetName() == "ConfirmAttributeOwnershipAcquisitionCancellation"
		})
		for _, message := range file.MessageType {
			if message.GetName() == federateEventMessageName {
				message.Field = slices.DeleteFunc(message.Field, func(field *descriptorpb.FieldDescriptorProto) bool {
					return field.GetName() == "ownership_receipt" && field.GetNumber() == 4 || field.GetName() == "object_knowledge_epoch" && field.GetNumber() == 5 || field.GetName() == "ownership_acquisition_cancelled" && field.GetNumber() == 35
				})
			}
		}
	}
	if file.GetName() == objectProtoFile {
		file.MessageType = slices.DeleteFunc(file.MessageType, func(message *descriptorpb.DescriptorProto) bool {
			return message.GetName() == "LocalDeleteObjectInstanceFence"
		})
		for _, service := range file.Service {
			if service.GetName() == "ObjectService" {
				service.Method = slices.DeleteFunc(service.Method, func(method *descriptorpb.MethodDescriptorProto) bool {
					return method.GetName() == "LocalDeleteObjectInstanceFenced"
				})
			}
		}
	}
}
