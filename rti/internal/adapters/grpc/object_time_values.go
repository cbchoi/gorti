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

func decodeOptionalObjectTime[T core.LogicalTimeScalar](legacy *float64, exact *rtiv1.LogicalTimeValue) (*T, error) {
	// Preserve the legacy Float64 final-time sentinel, outside the finite codec.
	if exact == nil && legacy != nil && core.ScalarRepresentation[T]() == logicaltime.HLAfloat64Time && math.IsInf(*legacy, 1) {
		value := T(*legacy)
		return &value, nil
	}
	return decodeOptionalScalarTime[T](legacy, exact)
}

func encodeOptionalObjectTime[T core.LogicalTimeScalar](value *T) (*float64, *rtiv1.LogicalTimeValue, error) {
	if value == nil {
		return nil, nil, nil
	}
	if scalarUnbounded(*value) {
		legacy := float64(*value)
		return &legacy, nil, nil
	}
	typed, err := core.ScalarTimeValue(*value)
	if err != nil {
		return nil, nil, status.Error(codes.Internal, err.Error())
	}
	legacy, exact, err := wire.EncodeOptionalLogicalTimePair(core.ScalarRepresentation[T](), &typed)
	if err != nil {
		return nil, nil, status.Error(codes.Internal, err.Error())
	}
	return legacy, exact, nil
}
