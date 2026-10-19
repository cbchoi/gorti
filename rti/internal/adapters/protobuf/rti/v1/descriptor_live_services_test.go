package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// This layer retains the immediately preceding service-completion baselines.
// Only enumerated live-service additions are removed, not arbitrary new fields.
func TestLiveServiceWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "9f33b1806c94efdad23a7fd130480688c7d4da6b7c32f06f7c8e03614fca03cc", "0d249495b8ba4906688d1794dba9d0a8e1f5ac82bae8975dd6a7c79d4cfdceb3", projectBeforeLiveServices)
}

func projectBeforeLiveServices(file *descriptorpb.FileDescriptorProto) {
	projectBeforeRetractionWatermark(file)
	var messages, methods, enums, dependencies []string
	var fields map[string][]int32
	switch file.GetName() {
	case federationProtoFile:
		messages = []string{"GetFOMViewRequest", "FOMView", "FOMClassBinding", "FOMMemberBinding", "FOMDimensionBinding", "SetAutomaticResignDirectiveRequest", "GetAutomaticResignDirectiveRequest", "GetAutomaticResignDirectiveResponse"}
		methods = []string{"GetFOMView", "SetAutomaticResignDirective", "GetAutomaticResignDirective"}
		fields = map[string][]int32{"JoinFederationRequest": {6}, "JoinFederationResponse": {4}}
	case "rti/v1/eventlog.proto":
		fields = map[string][]int32{"FederateJoined": {3, 4, 5, 6}}
	case supportProtoFile:
		methods = []string{"GetUpdateRateValue", "GetUpdateRateValueForAttribute", "GetAvailableDimensionsForClassAttribute", "GetAvailableDimensionsForInteractionClass", "GetDimensionHandleSet"}
		for _, method := range methods {
			messages = append(messages, method+"Request", method+"Response")
		}
	case objectProtoFile:
		methods = []string{"ChangeAttributeOrderType", "ChangeInteractionOrderType"}
		messages = []string{"ChangeAttributeOrderRequest", "ChangeInteractionOrderRequest"}
		enums = []string{"OrderType"}
		fields = map[string][]int32{"UpdateAttributeValuesRequest": {8}, "SendInteractionRequest": {8}, "DeleteObjectInstanceRequest": {7}}
	case streamProtoFile:
		dependencies = []string{objectProtoFile}
		messages = []string{"ObjectCallbackMetadata", "ReceivedRegions", "TurnUpdatesOnForObjectInstance", "TurnUpdatesOffForObjectInstance"}
		fields = map[string][]int32{federateEventMessageName: {67, 68}, "DiscoverObjectInstance": {4}, "ReflectAttributeValues": {5, 6}, "RemoveObjectInstance": {4}, "ReceiveInteraction": {4, 5}, "FederationRestored": {2, 3}}
	case ddmProtoFile:
		fields = map[string][]int32{"SendInteractionWithRegionsRequest": {8, 9}}
	}
	file.Dependency = slices.DeleteFunc(file.Dependency, func(name string) bool { return slices.Contains(dependencies, name) })
	file.MessageType = slices.DeleteFunc(file.MessageType, func(m *descriptorpb.DescriptorProto) bool { return slices.Contains(messages, m.GetName()) })
	file.EnumType = slices.DeleteFunc(file.EnumType, func(e *descriptorpb.EnumDescriptorProto) bool { return slices.Contains(enums, e.GetName()) })
	for _, service := range file.Service {
		service.Method = slices.DeleteFunc(service.Method, func(m *descriptorpb.MethodDescriptorProto) bool { return slices.Contains(methods, m.GetName()) })
	}
	for _, message := range file.MessageType {
		removed := fields[message.GetName()]
		if len(removed) == 0 {
			continue
		}
		var removedOneofs []int32
		message.Field = slices.DeleteFunc(message.Field, func(f *descriptorpb.FieldDescriptorProto) bool {
			if !slices.Contains(removed, f.GetNumber()) {
				return false
			}
			if f.GetProto3Optional() {
				removedOneofs = append(removedOneofs, f.GetOneofIndex())
			}
			return true
		})
		// Preserve every remaining oneof and reindex only around removed synthetic
		// optional-field entries. Real event oneofs are never discarded.
		var kept []*descriptorpb.OneofDescriptorProto
		indices := map[int32]int32{}
		for i, oneof := range message.OneofDecl {
			if !slices.Contains(removedOneofs, int32(i)) {
				indices[int32(i)] = int32(len(kept))
				kept = append(kept, oneof)
			}
		}
		message.OneofDecl = kept
		for _, field := range message.Field {
			if field.OneofIndex != nil {
				index := indices[field.GetOneofIndex()]
				field.OneofIndex = &index
			}
		}
	}
}

func TestLiveServiceWireFieldInvariants(t *testing.T) {
	cases := []struct {
		message        protoreflect.MessageDescriptor
		name           protoreflect.Name
		number         protoreflect.FieldNumber
		kind           protoreflect.Kind
		list, presence bool
	}{
		{(&JoinFederationRequest{}).ProtoReflect().Descriptor(), "additional_fom_modules", 6, protoreflect.MessageKind, true, false},
		{(&JoinFederationResponse{}).ProtoReflect().Descriptor(), "fom_view", 4, protoreflect.MessageKind, false, true},
		{(&FederateJoined{}).ProtoReflect().Descriptor(), "additional_fom_modules", 3, protoreflect.MessageKind, true, false},
		{(&FederateJoined{}).ProtoReflect().Descriptor(), "fom_revision", 4, protoreflect.Uint64Kind, false, false},
		{(&FederateJoined{}).ProtoReflect().Descriptor(), "fom_digest", 5, protoreflect.StringKind, false, false},
		{(&FederateJoined{}).ProtoReflect().Descriptor(), "federate_type", 6, protoreflect.StringKind, false, false},
		{(&UpdateAttributeValuesRequest{}).ProtoReflect().Descriptor(), "user_supplied_tag", 8, protoreflect.BytesKind, false, false},
		{(&SendInteractionRequest{}).ProtoReflect().Descriptor(), "user_supplied_tag", 8, protoreflect.BytesKind, false, false},
		{(&DeleteObjectInstanceRequest{}).ProtoReflect().Descriptor(), "message_retraction_handle", 7, protoreflect.Uint64Kind, false, false},
		{(&SendInteractionWithRegionsRequest{}).ProtoReflect().Descriptor(), "user_supplied_tag", 8, protoreflect.BytesKind, false, false},
		{(&SendInteractionWithRegionsRequest{}).ProtoReflect().Descriptor(), "message_retraction_handle", 9, protoreflect.Uint64Kind, false, false},
		{(&DiscoverObjectInstance{}).ProtoReflect().Descriptor(), "producing_federate", 4, protoreflect.Uint64Kind, false, true},
		{(&ReflectAttributeValues{}).ProtoReflect().Descriptor(), "metadata", 6, protoreflect.MessageKind, false, true},
		{(&ReceiveInteraction{}).ProtoReflect().Descriptor(), "metadata", 5, protoreflect.MessageKind, false, true},
		{(&RemoveObjectInstance{}).ProtoReflect().Descriptor(), "metadata", 4, protoreflect.MessageKind, false, true},
		{(&ObjectCallbackMetadata{}).ProtoReflect().Descriptor(), "sent_order", 1, protoreflect.EnumKind, false, true},
		{(&ObjectCallbackMetadata{}).ProtoReflect().Descriptor(), "received_order", 2, protoreflect.EnumKind, false, true},
		{(&ObjectCallbackMetadata{}).ProtoReflect().Descriptor(), "transportation_type", 3, protoreflect.EnumKind, false, true},
		{(&ObjectCallbackMetadata{}).ProtoReflect().Descriptor(), "producing_federate", 4, protoreflect.Uint64Kind, false, true},
		{(&ObjectCallbackMetadata{}).ProtoReflect().Descriptor(), "message_retraction_handle", 5, protoreflect.Uint64Kind, false, true},
		{(&ObjectCallbackMetadata{}).ProtoReflect().Descriptor(), "received_regions", 6, protoreflect.MessageKind, false, true},
		{(&TurnUpdatesOnForObjectInstance{}).ProtoReflect().Descriptor(), "update_rate_designator", 3, protoreflect.StringKind, false, true},
		{(&FederationRestored{}).ProtoReflect().Descriptor(), "federate_handle", 2, protoreflect.Uint64Kind, false, true},
		{(&FederationRestored{}).ProtoReflect().Descriptor(), "callback_epoch", 3, protoreflect.Uint64Kind, false, true},
	}
	for _, tc := range cases {
		t.Run(string(tc.message.Name())+"/"+string(tc.name), func(t *testing.T) {
			f := tc.message.Fields().ByName(tc.name)
			if f == nil || f.Number() != tc.number || f.Kind() != tc.kind || f.IsList() != tc.list || f.HasPresence() != tc.presence {
				t.Fatalf("field contract mismatch: %v", f)
			}
		})
	}
}
