package grpc

import (
	wire "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math"
)

func scalarUnbounded[T core.LogicalTimeScalar](value T) bool {
	return core.ScalarRepresentation[T]() == logicaltime.HLAfloat64Time && math.IsInf(float64(value), 1)
}

func decodeScalarTime[T core.LogicalTimeScalar](legacy float64, exact *rtiv1.LogicalTimeValue) (T, error) {
	// Preserve legacy domain classification: NaN is invalid, negative targets
	// are already passed, and +Inf is the product final-time sentinel.
	if core.ScalarRepresentation[T]() == logicaltime.HLAfloat64Time && exact == nil {
		return T(legacy), nil
	}
	value, err := wire.DecodeLogicalTimePair(core.ScalarRepresentation[T](), legacy, exact)
	if err != nil {
		return 0, status.Error(codes.InvalidArgument, err.Error())
	}
	return core.ScalarFromTimeValue[T](value)
}
func decodeOptionalScalarTime[T core.LogicalTimeScalar](legacy *float64, exact *rtiv1.LogicalTimeValue) (*T, error) {
	value, err := wire.DecodeOptionalLogicalTimePair(core.ScalarRepresentation[T](), legacy, exact)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if value == nil {
		return nil, nil
	}
	result, err := core.ScalarFromTimeValue[T](*value)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &result, nil
}
func decodeScalarInterval[T core.LogicalTimeScalar](legacy float64, exact *rtiv1.LogicalTimeValue) (T, error) {
	value, err := wire.DecodeLogicalTimeIntervalPair(core.ScalarRepresentation[T](), legacy, exact)
	if err != nil {
		return 0, status.Error(codes.InvalidArgument, err.Error())
	}
	return core.ScalarFromIntervalValue[T](value)
}
func encodeScalarTime[T core.LogicalTimeScalar](value T) (float64, *rtiv1.LogicalTimeValue, error) {
	if scalarUnbounded(value) {
		return float64(value), nil, nil
	}
	typed, err := core.ScalarTimeValue(value)
	if err != nil {
		return 0, nil, status.Error(codes.Internal, err.Error())
	}
	legacy, exact, err := wire.EncodeLogicalTimePair(core.ScalarRepresentation[T](), typed)
	if err != nil {
		return 0, nil, status.Error(codes.Internal, err.Error())
	}
	return legacy, exact, nil
}
func encodeScalarInterval[T core.LogicalTimeScalar](value T) (float64, *rtiv1.LogicalTimeValue, error) {
	typed, err := core.ScalarIntervalValue(value)
	if err != nil {
		return 0, nil, status.Error(codes.Internal, err.Error())
	}
	legacy, exact, err := wire.EncodeLogicalTimeIntervalPair(core.ScalarRepresentation[T](), typed)
	if err != nil {
		return 0, nil, status.Error(codes.Internal, err.Error())
	}
	return legacy, exact, nil
}
