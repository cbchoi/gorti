package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/types/descriptorpb"
)

func TestTimeEnableCallbackWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "9ca9e2b1e2448fc1f9bd63633316b91aad322d4a8e2049c667b6977df09a0b94", rpcGoldenSHA256, projectBeforeTimeEnableCallbacks)
}

func projectBeforeTimeEnableCallbacks(file *descriptorpb.FileDescriptorProto) {
	projectBeforeSaveRestoreFailureReasons(file)
	if file.GetName() != streamProtoFile {
		return
	}
	file.MessageType = slices.DeleteFunc(file.MessageType, func(m *descriptorpb.DescriptorProto) bool {
		return m.GetName() == "TimeRegulationEnabled" || m.GetName() == "TimeConstrainedEnabled"
	})
	for _, message := range file.MessageType {
		if message.GetName() == federateEventMessageName {
			message.Field = slices.DeleteFunc(message.Field, func(f *descriptorpb.FieldDescriptorProto) bool {
				return f.GetNumber() == 70 || f.GetNumber() == 71
			})
		}
	}
}
