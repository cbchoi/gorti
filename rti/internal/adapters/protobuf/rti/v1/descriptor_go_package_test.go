package rtiv1

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

const descriptorBeforeGoPackageAlignmentSHA256 = "d3b228d4727dee18bca53124a2820b4be0584c243656a5f83192d32a07631043"

func TestGoPackageAlignmentChangesOnlyGeneratorMetadata(t *testing.T) {
	checkDescriptorGolden(t, descriptorBeforeGoPackageAlignmentSHA256, rpcCallbackReportGoldenSHA256, func(file *descriptorpb.FileDescriptorProto) {
		if file.GetName() == "rti/v1/callback_report.proto" {
			file.Options.GoPackage = proto.String("github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1;rtiv1")
		}
	})
}
