package rtiv1

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

const descriptorBeforeCallbackReportSHA256 = "d96d5162bbaf25a45848172f0de8b5fbd75c81b2c69770f41ef4b614a9d5ff67"

func TestCallbackReportStreamPreservesPreviousWire(t *testing.T) {
	checkDescriptorGolden(t, descriptorBeforeCallbackReportSHA256, rpcOwnershipCallbackGoldenSHA256, projectBeforeCallbackReport)
}

func projectBeforeCallbackReport(file *descriptorpb.FileDescriptorProto) {
	if file.GetName() == "rti/v1/callback_report.proto" {
		proto.Reset(file)
	}
}
