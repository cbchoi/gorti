package rtiv1

import (
	"google.golang.org/protobuf/types/descriptorpb"
	"slices"
	"testing"
)

func TestCallbackRecoveryPreservesPreviousWire(t *testing.T) {
	checkDescriptorGolden(t, "f1b7677d6433548db6ef69ea0688376580616bd002393fe2a592a5875646275a", rpcOwnershipCallbackGoldenSHA256, projectBeforeCallbackRecovery)
}

func projectBeforeCallbackRecovery(file *descriptorpb.FileDescriptorProto) {
	if file.GetName() != streamProtoFile {
		return
	}
	file.MessageType = slices.DeleteFunc(file.MessageType, func(m *descriptorpb.DescriptorProto) bool { return m.GetName() == "CallbackRecovery" })
	for _, m := range file.MessageType {
		if m.GetName() == federateEventMessageName {
			m.Field = slices.DeleteFunc(m.Field, func(f *descriptorpb.FieldDescriptorProto) bool {
				return f.GetName() == "callback_recovery" && f.GetNumber() == 6
			})
		}
	}
}

const streamProtoFile = "rti/v1/stream.proto"

const federateEventMessageName = "FederateEvent"
