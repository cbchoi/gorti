package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestHydrationAndRemainingServicesWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "fc18f9e3a1d0b5acd5690661759f53f12aba18811233303a51d7fdb08b0b96d5", "667962c787127aee334ca438ea899e846487c6b060fd4c4a89d18615880d21ed", projectBeforeHydrationAndRemainingServices)
}

// Remove only the reviewed session/hydration/receipt, explicit restore-source,
// ownership release denial, sync history and atomic name-release additions.
// All preexisting declarations, signatures and field options remain byte-exact.
func projectBeforeHydrationAndRemainingServices(file *descriptorpb.FileDescriptorProto) {
	projectBeforeMOMMeasurements(file)
	var messages, methods, dependencies []string
	var fields map[string][]int32
	switch file.GetName() {
	case federationProtoFile:
		fields = map[string][]int32{"JoinFederationResponse": {5}}
	case "rti/v1/savepoint.proto":
		fields = map[string][]int32{"RequestFederationRestoreRequest": {5}}
	case "rti/v1/ownership.proto":
		messages = []string{"ReleaseDeniedRequest"}
		methods = []string{"AttributeOwnershipReleaseDenied"}
	case "rti/v1/local_lrc.proto":
		messages = []string{"GetLocalValidationStateRequest", "LocalValidationState", "PublishedObjectClass", "KnownObjectState"}
		methods = []string{"GetLocalValidationState"}
		dependencies = []string{federationProtoFile}
	case streamProtoFile:
		messages = []string{"LocalValidationBarrier"}
		fields = map[string][]int32{federateEventMessageName: {2, 69}}
	case supportProtoFile:
		messages = []string{"ReportCallbackInvocationRequest", "NormalizeFederateHandleRequest", "NormalizeFederateHandleResponse", "NormalizeServiceGroupRequest", "NormalizeServiceGroupResponse"}
		methods = []string{"ReportCallbackInvocation", "NormalizeFederateHandle", "NormalizeServiceGroup"}
	case "rti/v1/sync.proto":
		fields = map[string][]int32{"SyncPointStateEntry": {7, 8}}
	case objectProtoFile:
		messages = []string{"ReleaseMultipleObjectInstanceNamesRequest"}
		methods = []string{"ReleaseMultipleObjectInstanceNames"}
	}
	file.Dependency = slices.DeleteFunc(file.Dependency, func(name string) bool { return slices.Contains(dependencies, name) })
	file.MessageType = slices.DeleteFunc(file.MessageType, func(m *descriptorpb.DescriptorProto) bool { return slices.Contains(messages, m.GetName()) })
	for _, service := range file.Service {
		service.Method = slices.DeleteFunc(service.Method, func(m *descriptorpb.MethodDescriptorProto) bool { return slices.Contains(methods, m.GetName()) })
	}
	for _, message := range file.MessageType {
		removed := fields[message.GetName()]
		if len(removed) == 0 {
			continue
		}
		var oneofs []int32
		message.Field = slices.DeleteFunc(message.Field, func(f *descriptorpb.FieldDescriptorProto) bool {
			if !slices.Contains(removed, f.GetNumber()) {
				return false
			}
			if f.GetProto3Optional() {
				oneofs = append(oneofs, f.GetOneofIndex())
			}
			return true
		})
		indices := make(map[int32]int32)
		var kept []*descriptorpb.OneofDescriptorProto
		for i, o := range message.OneofDecl {
			if !slices.Contains(oneofs, int32(i)) {
				indices[int32(i)] = int32(len(kept))
				kept = append(kept, o)
			}
		}
		message.OneofDecl = kept
		for _, f := range message.Field {
			if f.OneofIndex != nil {
				index := indices[f.GetOneofIndex()]
				f.OneofIndex = &index
			}
		}
	}
}

func TestHydrationAndRemainingServicesWireInvariants(t *testing.T) {
	cases := []struct {
		message        protoreflect.MessageDescriptor
		name           protoreflect.Name
		number         protoreflect.FieldNumber
		kind           protoreflect.Kind
		list, presence bool
	}{
		{(&JoinFederationResponse{}).ProtoReflect().Descriptor(), "session_token", 5, protoreflect.BytesKind, false, false},
		{(&RequestFederationRestoreRequest{}).ProtoReflect().Descriptor(), "saved_federation_generation", 5, protoreflect.Uint64Kind, false, true},
		{(&GetLocalValidationStateRequest{}).ProtoReflect().Descriptor(), "snapshot_id", 6, protoreflect.Uint64Kind, false, false},
		{(&LocalValidationState{}).ProtoReflect().Descriptor(), "snapshot_id", 8, protoreflect.Uint64Kind, false, false},
		{(&LocalValidationState{}).ProtoReflect().Descriptor(), "fom_view", 4, protoreflect.MessageKind, false, true},
		{(&FederateEvent{}).ProtoReflect().Descriptor(), "callback_receipt", 2, protoreflect.BytesKind, false, false},
		{(&FederateEvent{}).ProtoReflect().Descriptor(), "local_validation_barrier", 69, protoreflect.MessageKind, false, true},
		{(&LocalValidationBarrier{}).ProtoReflect().Descriptor(), "callback_epoch", 3, protoreflect.Uint64Kind, false, false},
		{(&ReportCallbackInvocationRequest{}).ProtoReflect().Descriptor(), "callback_receipt", 5, protoreflect.BytesKind, false, false},
		{(&NormalizeFederateHandleResponse{}).ProtoReflect().Descriptor(), "value", 1, protoreflect.Uint64Kind, false, false},
		{(&ReleaseDeniedRequest{}).ProtoReflect().Descriptor(), "attribute_handles", 5, protoreflect.Uint64Kind, true, false},
		{(&ReleaseMultipleObjectInstanceNamesRequest{}).ProtoReflect().Descriptor(), "object_names", 4, protoreflect.StringKind, true, false},
		{(&SyncPointStateEntry{}).ProtoReflect().Descriptor(), "failed", 7, protoreflect.Uint64Kind, true, false},
		{(&SyncPointStateEntry{}).ProtoReflect().Descriptor(), "failure_history_complete", 8, protoreflect.BoolKind, false, true},
	}
	for _, tc := range cases {
		t.Run(string(tc.message.Name())+"/"+string(tc.name), func(t *testing.T) {
			f := tc.message.Fields().ByName(tc.name)
			if f == nil || f.Number() != tc.number || f.Kind() != tc.kind || f.IsList() != tc.list || f.HasPresence() != tc.presence {
				t.Fatalf("field contract mismatch: %v", f)
			}
		})
	}
	if !(&ReportCallbackInvocationRequest{}).ProtoReflect().Descriptor().ReservedRanges().Has(4) {
		t.Fatal("client-supplied callback class slot must stay reserved")
	}
	if (&GetLocalValidationStateRequest{}).ProtoReflect().Descriptor().Fields().ByName("session_token") != nil {
		t.Fatal("session authentication must remain transport metadata")
	}
}
