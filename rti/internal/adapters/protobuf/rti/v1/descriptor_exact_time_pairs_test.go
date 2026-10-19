package rtiv1

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

type exactTimePair struct {
	file, message, legacy, exact string
	legacyTag, exactTag          protoreflect.FieldNumber
}

var exactTimePairs = []exactTimePair{
	{"rti/v1/common.proto", "LogicalTime", "value", "logical_time_value", 1, 2},
	{"rti/v1/time.proto", "EnableRegulationRequest", "lookahead", "lookahead_value", 4, 5},
	{"rti/v1/time.proto", "ModifyLookaheadRequest", "lookahead", "lookahead_value", 4, 5},
	{"rti/v1/time.proto", "NERRequest", "logical_time", "logical_time_value", 4, 5},
	{"rti/v1/time.proto", "NMRARequest", "logical_time", "logical_time_value", 4, 5},
	{"rti/v1/time.proto", "TARRequest", "logical_time", "logical_time_value", 4, 5},
	{"rti/v1/time.proto", "TARARequest", "logical_time", "logical_time_value", 4, 5},
	{"rti/v1/time.proto", "FQRRequest", "logical_time", "logical_time_value", 4, 5},
	{"rti/v1/time.proto", "QueryFederateTimeResponse", "logical_time", "logical_time_value", 1, 2},
	{"rti/v1/time.proto", "QueryLookaheadResponse", "lookahead", "lookahead_value", 1, 2},
	{"rti/v1/time.proto", "QueryLBTSResponse", "lbts", "lbts_value", 1, 3},
	{"rti/v1/time.proto", "QueryGALTResponse", "galt", "galt_value", 1, 3},
	{"rti/v1/time.proto", "QueryLITSResponse", "lits", "lits_value", 1, 3},
	{objectProtoFile, "UpdateAttributeValuesRequest", "logical_time", "logical_time_value", 6, 9},
	{objectProtoFile, "SendInteractionRequest", "logical_time", "logical_time_value", 6, 9},
	{objectProtoFile, "DeleteObjectInstanceRequest", "logical_time", "logical_time_value", 5, 8},
	{streamProtoFile, "ReflectAttributeValues", "logical_time", "logical_time_value", 4, 7},
	{streamProtoFile, "RemoveObjectInstance", "logical_time", "logical_time_value", 2, 5},
	{streamProtoFile, "ReceiveInteraction", "logical_time", "logical_time_value", 3, 6},
	{streamProtoFile, "TimeAdvanceGrant", "logical_time", "logical_time_value", 1, 2},
	{streamProtoFile, "TimeRegulationEnabled", "logical_time", "logical_time_value", 1, 2},
	{streamProtoFile, "TimeConstrainedEnabled", "logical_time", "logical_time_value", 1, 2},
	{streamProtoFile, "InitiateFederateSave", "save_time", "save_time_value", 2, 3},
	{"rti/v1/admin.proto", "FederateSnapshot", "current_time", "current_time_value", 3, 20},
	{"rti/v1/admin.proto", "FederateSnapshot", "pending_request_time", "pending_request_time_value", 4, 21},
	{"rti/v1/admin.proto", "FederateSnapshot", "lookahead", "lookahead_value", 5, 22},
	{"rti/v1/admin.proto", "TimeSnapshot", "lbts", "lbts_value", 1, 5},
	{"rti/v1/admin.proto", "PendingGrant", "requested_time", "requested_time_value", 2, 3},
	{"rti/v1/eventlog.proto", "ObjectDeleted", "logical_time", "logical_time_value", 2, 3},
	{"rti/v1/eventlog.proto", "AttributeUpdated", "logical_time", "logical_time_value", 4, 5},
	{"rti/v1/eventlog.proto", "InteractionSent", "logical_time", "logical_time_value", 4, 5},
	{"rti/v1/eventlog.proto", "TimeAdvanceRequested", "logical_time", "logical_time_value", 2, 3},
	{"rti/v1/eventlog.proto", "TimeAdvanceGranted", "logical_time", "logical_time_value", 2, 3},
	{"rti/v1/mom.proto", "MOMFederateRecord", "lookahead", "lookahead_value", 6, 14},
	{"rti/v1/mom.proto", "MOMFederateRecord", "logical_time", "logical_time_value", 7, 15},
	{ddmProtoFile, "SendInteractionWithRegionsRequest", "logical_time", "logical_time_value", 6, 10},
	{"rti/v1/savepoint.proto", "RequestFederationSaveRequest", "save_time", "save_time_value", 5, 6},
}

func TestExactTimePairWireShape(t *testing.T) {
	for _, pair := range exactTimePairs {
		t.Run(pair.message+"/"+pair.exact, func(t *testing.T) {
			file, err := protoregistry.GlobalFiles.FindFileByPath(pair.file)
			if err != nil {
				t.Fatal(err)
			}
			message := file.Messages().ByName(protoreflect.Name(pair.message))
			legacy := message.Fields().ByName(protoreflect.Name(pair.legacy))
			exact := message.Fields().ByName(protoreflect.Name(pair.exact))
			if legacy == nil || legacy.Number() != pair.legacyTag || legacy.Kind() != protoreflect.DoubleKind {
				t.Fatalf("legacy field changed: %v", legacy)
			}
			if exact == nil || exact.Number() != pair.exactTag || exact.Kind() != protoreflect.MessageKind || !exact.HasPresence() || exact.Message().FullName() != "rti.v1.LogicalTimeValue" || exact.ContainingOneof() != nil {
				t.Fatalf("missing or invalid exact field: %v", exact)
			}
		})
	}
}

func TestExactTimePairWireChangesAreAdditive(t *testing.T) {
	checkDescriptorGolden(t, "2753a4f878875fc9a1c778b6ef1bd8252f5e899b498c5058e17e3f4f578c5e09", rpcCurrentGoldenSHA256, projectBeforeExactTimePairs)
}

func projectBeforeExactTimePairs(file *descriptorpb.FileDescriptorProto) {
	projectBeforeOwnershipCallbackProtocol(file)
	for _, message := range file.MessageType {
		message.Field = slices.DeleteFunc(message.Field, func(field *descriptorpb.FieldDescriptorProto) bool {
			for _, pair := range exactTimePairs {
				if file.GetName() == pair.file && message.GetName() == pair.message && field.GetName() == pair.exact && field.GetNumber() == int32(pair.exactTag) {
					return true
				}
			}
			return false
		})
	}
}

const objectProtoFile = "rti/v1/object.proto"
