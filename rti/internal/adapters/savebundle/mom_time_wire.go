package savebundle

import (
	"fmt"

	codec "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Ordinary protobuf decoding discards repeated oneof selectors. Inspect exact
// saved time messages before decoding so restore cannot hide a contradictory
// selector behind the final occurrence.
func validateMOMTimeWire(data []byte, descriptor protoreflect.MessageDescriptor, expected logicaltime.Representation) error {
	seen := make(map[protowire.Number]bool)
	for len(data) != 0 {
		number, kind, tagLength := protowire.ConsumeTag(data)
		if tagLength < 0 {
			return protowire.ParseError(tagLength)
		}
		data = data[tagLength:]
		field := descriptor.Fields().ByNumber(protoreflect.FieldNumber(number))
		if field != nil && field.Message() != nil {
			if kind != protowire.BytesType {
				return fmt.Errorf("MOM time field has invalid wire type")
			}
			payload, length := protowire.ConsumeBytes(data)
			if length < 0 {
				return protowire.ParseError(length)
			}
			if field.Message().FullName() == "rti.v1.LogicalTimeValue" {
				if seen[number] {
					return fmt.Errorf("MOM repeated exact time field %s", field.Name())
				}
				seen[number] = true
				if _, err := codec.UnmarshalLogicalTime(expected, payload); err != nil {
					return err
				}
			} else if err := validateMOMTimeWire(payload, field.Message(), expected); err != nil {
				return err
			}
			data = data[length:]
			continue
		}
		length := protowire.ConsumeFieldValue(number, kind, data)
		if length < 0 {
			return protowire.ParseError(length)
		}
		data = data[length:]
	}
	return nil
}
