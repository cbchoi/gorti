package rtiv1

import (
	"math"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestEveryExactTimePairInteger64RoundTrip(t *testing.T) {
	for _, pair := range exactTimePairs {
		t.Run(pair.message+"/"+pair.exact, func(t *testing.T) {
			file, err := protoregistry.GlobalFiles.FindFileByPath(pair.file)
			if err != nil {
				t.Fatal(err)
			}
			descriptor := file.Messages().ByName(protoreflect.Name(pair.message))
			exactField := descriptor.Fields().ByNumber(pair.exactTag)
			if exactField == nil {
				t.Fatal("missing exact field")
			}
			legacyField := descriptor.Fields().ByNumber(pair.legacyTag)
			for _, value := range []int64{0, 1<<53 + 1, math.MaxInt64} {
				message := dynamicpb.NewMessage(descriptor)
				exact := &LogicalTimeValue{Value: &LogicalTimeValue_Integer64Value{Integer64Value: value}}
				message.Set(exactField, protoreflect.ValueOfMessage(exact.ProtoReflect()))
				wire, err := proto.Marshal(message)
				if err != nil {
					t.Fatal(err)
				}
				decoded := dynamicpb.NewMessage(descriptor)
				if err := proto.Unmarshal(wire, decoded); err != nil {
					t.Fatal(err)
				}
				if decoded.Has(legacyField) || decoded.Get(legacyField).Float() != 0 {
					t.Fatal("integer populated legacy scalar")
				}
				if !decoded.Has(exactField) {
					t.Fatal("exact zero lost message presence")
				}
				selected := decoded.Get(exactField).Message()
				integerField := selected.Descriptor().Fields().ByName("integer64_value")
				if !selected.Has(integerField) || selected.Get(integerField).Int() != value {
					t.Fatal("integer precision/selector lost")
				}
			}
		})
	}
}

func TestEveryExactTimePairLegacyFloat64RoundTrip(t *testing.T) {
	for _, pair := range exactTimePairs {
		t.Run(pair.message+"/"+pair.legacy, func(t *testing.T) {
			file, err := protoregistry.GlobalFiles.FindFileByPath(pair.file)
			if err != nil {
				t.Fatal(err)
			}
			descriptor := file.Messages().ByName(protoreflect.Name(pair.message))
			legacyField := descriptor.Fields().ByNumber(pair.legacyTag)
			exactField := descriptor.Fields().ByNumber(pair.exactTag)
			if exactField == nil {
				t.Fatal("missing exact field")
			}
			for _, value := range []float64{0, 0.5, math.MaxFloat64} {
				message := dynamicpb.NewMessage(descriptor)
				message.Set(legacyField, protoreflect.ValueOfFloat64(value))
				wire, err := proto.Marshal(message)
				if err != nil {
					t.Fatal(err)
				}
				decoded := dynamicpb.NewMessage(descriptor)
				if err := proto.Unmarshal(wire, decoded); err != nil {
					t.Fatal(err)
				}
				if decoded.Has(exactField) || decoded.Get(legacyField).Float() != value {
					t.Fatal("legacy Float64 changed")
				}
				if legacyField.HasPresence() && !decoded.Has(legacyField) {
					t.Fatal("legacy optional zero lost presence")
				}
			}
		})
	}
}

func TestEveryLogicalTimeScalarHasExactSibling(t *testing.T) {
	exceptions := map[protoreflect.FullName]bool{
		"rti.v1.LogicalTimeValue.float64_value":                     true,
		"rti.v1.CreateFederationRequest.stall_timeout_seconds":      true,
		"rti.v1.GetUpdateRateValueResponse.update_rate":             true,
		"rti.v1.GetUpdateRateValueForAttributeResponse.update_rate": true,
	}
	protoregistry.GlobalFiles.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		if file.Package() != "rti.v1" {
			return true
		}
		for i := 0; i < file.Messages().Len(); i++ {
			message := file.Messages().Get(i)
			for j := 0; j < message.Fields().Len(); j++ {
				field := message.Fields().Get(j)
				if field.Kind() != protoreflect.DoubleKind || exceptions[field.FullName()] {
					continue
				}
				covered := false
				for _, pair := range exactTimePairs {
					if file.Path() == pair.file && string(message.Name()) == pair.message && field.Number() == pair.legacyTag {
						covered = true
						break
					}
				}
				if !covered {
					t.Errorf("unmapped double requires time/non-time classification: %s", field.FullName())
				}
			}
		}
		return true
	})
}
