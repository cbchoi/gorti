// Package protobuf provides checked adapters for RTI protocol values.
package protobuf

import (
	"errors"
	"math"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

var ErrInvalidLogicalTimeValue = errors.New("protobuf: logical time requires exactly one known value selector")

// EncodeLogicalTime requires the value to match the selected implementation.
// Availability of this codec does not enable Integer64 federation services.
func EncodeLogicalTime(expected logicaltime.Representation, value logicaltime.Time) (*rtiv1.LogicalTimeValue, error) {
	return encodeLogicalValue(expected, value)
}

func EncodeLogicalTimeInterval(expected logicaltime.Representation, value logicaltime.Interval) (*rtiv1.LogicalTimeValue, error) {
	return encodeLogicalValue(expected, value)
}

type logicalValue interface {
	Representation() logicaltime.Representation
	Float64() (float64, error)
	Integer64() (int64, error)
}

func knownRepresentation(kind logicaltime.Representation) bool {
	return kind == logicaltime.HLAfloat64Time || kind == logicaltime.HLAinteger64Time
}

func encodeLogicalValue(expected logicaltime.Representation, value logicalValue) (*rtiv1.LogicalTimeValue, error) {
	if !knownRepresentation(expected) || !knownRepresentation(value.Representation()) {
		return nil, logicaltime.ErrUnknownRepresentation
	}
	if expected != value.Representation() {
		return nil, logicaltime.ErrRepresentationMismatch
	}
	if expected == logicaltime.HLAinteger64Time {
		integer, err := value.Integer64()
		if err != nil {
			return nil, err
		}
		return &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: integer}}, nil
	}
	floating, err := value.Float64()
	if err != nil {
		return nil, err
	}
	return &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: floating}}, nil
}

// DecodeLogicalTime validates a constructed message. If the source is untrusted
// bytes, use UnmarshalLogicalTime: ordinary protobuf unmarshalling discards
// duplicate oneof selectors before this function can inspect them.
func DecodeLogicalTime(expected logicaltime.Representation, value *rtiv1.LogicalTimeValue) (logicaltime.Time, error) {
	if err := validateLogicalValue(expected, value); err != nil {
		return logicaltime.Time{}, err
	}
	if expected == logicaltime.HLAinteger64Time {
		return logicaltime.Integer64(value.GetInteger64Value())
	}
	return logicaltime.Float64(value.GetFloat64Value())
}

// DecodeLogicalTimeInterval has the same message/byte validation boundary as
// DecodeLogicalTime. Use UnmarshalLogicalTimeInterval for untrusted bytes.
func DecodeLogicalTimeInterval(expected logicaltime.Representation, value *rtiv1.LogicalTimeValue) (logicaltime.Interval, error) {
	if err := validateLogicalValue(expected, value); err != nil {
		return logicaltime.Interval{}, err
	}
	if expected == logicaltime.HLAinteger64Time {
		return logicaltime.Integer64Interval(value.GetInteger64Value())
	}
	return logicaltime.Float64Interval(value.GetFloat64Value())
}

func validateLogicalValue(expected logicaltime.Representation, value *rtiv1.LogicalTimeValue) error {
	if !knownRepresentation(expected) {
		return logicaltime.ErrUnknownRepresentation
	}
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 {
		return ErrInvalidLogicalTimeValue
	}
	var actual logicaltime.Representation
	switch selected := value.Value.(type) {
	case *rtiv1.LogicalTimeValue_Integer64Value:
		if selected == nil {
			return ErrInvalidLogicalTimeValue
		}
		actual = logicaltime.HLAinteger64Time
	case *rtiv1.LogicalTimeValue_Float64Value:
		if selected == nil {
			return ErrInvalidLogicalTimeValue
		}
		actual = logicaltime.HLAfloat64Time
	default:
		return ErrInvalidLogicalTimeValue
	}
	if expected != actual {
		return logicaltime.ErrRepresentationMismatch
	}
	return nil
}

func MarshalLogicalTime(expected logicaltime.Representation, value logicaltime.Time) ([]byte, error) {
	message, err := EncodeLogicalTime(expected, value)
	if err != nil {
		return nil, err
	}
	return proto.Marshal(message)
}

func MarshalLogicalTimeInterval(expected logicaltime.Representation, value logicaltime.Interval) ([]byte, error) {
	message, err := EncodeLogicalTimeInterval(expected, value)
	if err != nil {
		return nil, err
	}
	return proto.Marshal(message)
}

// UnmarshalLogicalTime rejects unknown fields and repeated selectors, including
// repetitions of the same selector, rather than applying protobuf last-one-wins.
func UnmarshalLogicalTime(expected logicaltime.Representation, wire []byte) (logicaltime.Time, error) {
	message, err := unmarshalLogicalValue(wire)
	if err != nil {
		return logicaltime.Time{}, err
	}
	return DecodeLogicalTime(expected, message)
}

func UnmarshalLogicalTimeInterval(expected logicaltime.Representation, wire []byte) (logicaltime.Interval, error) {
	message, err := unmarshalLogicalValue(wire)
	if err != nil {
		return logicaltime.Interval{}, err
	}
	return DecodeLogicalTimeInterval(expected, message)
}

func unmarshalLogicalValue(wire []byte) (*rtiv1.LogicalTimeValue, error) {
	// The largest valid value is one tag byte plus a ten-byte signed varint.
	if len(wire) == 0 || len(wire) > 11 {
		return nil, ErrInvalidLogicalTimeValue
	}
	number, kind, tagBytes := protowire.ConsumeTag(wire)
	if tagBytes < 0 {
		return nil, ErrInvalidLogicalTimeValue
	}
	payload := wire[tagBytes:]
	switch {
	case number == 1 && kind == protowire.Fixed64Type:
		bits, consumed := protowire.ConsumeFixed64(payload)
		if consumed < 0 || consumed != len(payload) {
			return nil, ErrInvalidLogicalTimeValue
		}
		return &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: math.Float64frombits(bits)}}, nil
	case number == 2 && kind == protowire.VarintType:
		bits, consumed := protowire.ConsumeVarint(payload)
		if consumed < 0 || consumed != len(payload) {
			return nil, ErrInvalidLogicalTimeValue
		}
		return &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: protowire.DecodeZigZag(bits)}}, nil
	default:
		return nil, ErrInvalidLogicalTimeValue
	}
}
