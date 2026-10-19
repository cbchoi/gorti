package protobuf

import (
	"errors"
	"math"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

var ErrConflictingLogicalTimePair = errors.New("protobuf: legacy and exact logical time fields conflict")

// DecodeLogicalTimePair reads a required proto3 scalar and its additive exact
// field. A required scalar has no presence: +0 is indistinguishable from omission.
// Use the optional variant whenever the legacy field has protobuf presence.
// As with DecodeLogicalTime, duplicate selectors require raw-payload validation
// before ordinary protobuf unmarshalling discards them.
func DecodeLogicalTimePair(expected logicaltime.Representation, legacy float64, exact *rtiv1.LogicalTimeValue) (logicaltime.Time, error) {
	message, err := checkedLogicalPair(expected, legacy, math.Float64bits(legacy) != 0, exact)
	if err != nil {
		return logicaltime.Time{}, err
	}
	return DecodeLogicalTime(expected, message)
}

func DecodeLogicalTimeIntervalPair(expected logicaltime.Representation, legacy float64, exact *rtiv1.LogicalTimeValue) (logicaltime.Interval, error) {
	message, err := checkedLogicalPair(expected, legacy, math.Float64bits(legacy) != 0, exact)
	if err != nil {
		return logicaltime.Interval{}, err
	}
	return DecodeLogicalTimeInterval(expected, message)
}

// DecodeOptionalLogicalTimePair preserves receive-order/save-now absence. A
// present legacy scalar, including zero, can never stand in for Integer64.
func DecodeOptionalLogicalTimePair(expected logicaltime.Representation, legacy *float64, exact *rtiv1.LogicalTimeValue) (*logicaltime.Time, error) {
	if !knownRepresentation(expected) {
		return nil, logicaltime.ErrUnknownRepresentation
	}
	if legacy == nil && exact == nil {
		return nil, nil
	}
	message, err := checkedOptionalLogicalPair(expected, legacy, exact)
	if err != nil {
		return nil, err
	}
	value, err := DecodeLogicalTime(expected, message)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func DecodeOptionalLogicalTimeIntervalPair(expected logicaltime.Representation, legacy *float64, exact *rtiv1.LogicalTimeValue) (*logicaltime.Interval, error) {
	if !knownRepresentation(expected) {
		return nil, logicaltime.ErrUnknownRepresentation
	}
	if legacy == nil && exact == nil {
		return nil, nil
	}
	message, err := checkedOptionalLogicalPair(expected, legacy, exact)
	if err != nil {
		return nil, err
	}
	value, err := DecodeLogicalTimeInterval(expected, message)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func checkedOptionalLogicalPair(expected logicaltime.Representation, legacy *float64, exact *rtiv1.LogicalTimeValue) (*rtiv1.LogicalTimeValue, error) {
	if legacy == nil {
		return checkedLogicalPair(expected, 0, false, exact)
	}
	return checkedLogicalPair(expected, *legacy, true, exact)
}

func checkedLogicalPair(expected logicaltime.Representation, legacy float64, legacyPresent bool, exact *rtiv1.LogicalTimeValue) (*rtiv1.LogicalTimeValue, error) {
	if !knownRepresentation(expected) {
		return nil, logicaltime.ErrUnknownRepresentation
	}
	if exact == nil {
		if expected != logicaltime.HLAfloat64Time {
			return nil, logicaltime.ErrRepresentationMismatch
		}
		return &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: legacy}}, nil
	}
	if err := validateLogicalValue(expected, exact); err != nil {
		return nil, err
	}
	if legacyPresent {
		if expected == logicaltime.HLAinteger64Time {
			return nil, ErrConflictingLogicalTimePair
		}
		if _, err := logicaltime.Float64(legacy); err != nil {
			return nil, err
		}
		if legacy != exact.GetFloat64Value() {
			return nil, ErrConflictingLogicalTimePair
		}
	}
	return exact, nil
}

// EncodeLogicalTimePair supplies both forms only for Float64 compatibility.
// Integer64 supplies an exact selector and leaves the legacy scalar at +0.
// Callers must not populate a legacy optional scalar from that zero result.
func EncodeLogicalTimePair(expected logicaltime.Representation, value logicaltime.Time) (float64, *rtiv1.LogicalTimeValue, error) {
	return encodeLogicalPair(expected, value)
}

func EncodeLogicalTimeIntervalPair(expected logicaltime.Representation, value logicaltime.Interval) (float64, *rtiv1.LogicalTimeValue, error) {
	return encodeLogicalPair(expected, value)
}

func encodeLogicalPair(expected logicaltime.Representation, value logicalValue) (float64, *rtiv1.LogicalTimeValue, error) {
	exact, err := encodeLogicalValue(expected, value)
	if err != nil {
		return 0, nil, err
	}
	if expected == logicaltime.HLAinteger64Time {
		return 0, exact, nil
	}
	return exact.GetFloat64Value(), exact, nil
}

func EncodeOptionalLogicalTimePair(expected logicaltime.Representation, value *logicaltime.Time) (*float64, *rtiv1.LogicalTimeValue, error) {
	if !knownRepresentation(expected) {
		return nil, nil, logicaltime.ErrUnknownRepresentation
	}
	if value == nil {
		return nil, nil, nil
	}
	return encodeOptionalLogicalPair(expected, *value)
}

func EncodeOptionalLogicalTimeIntervalPair(expected logicaltime.Representation, value *logicaltime.Interval) (*float64, *rtiv1.LogicalTimeValue, error) {
	if !knownRepresentation(expected) {
		return nil, nil, logicaltime.ErrUnknownRepresentation
	}
	if value == nil {
		return nil, nil, nil
	}
	return encodeOptionalLogicalPair(expected, *value)
}

func encodeOptionalLogicalPair(expected logicaltime.Representation, value logicalValue) (*float64, *rtiv1.LogicalTimeValue, error) {
	legacy, exact, err := encodeLogicalPair(expected, value)
	if err != nil {
		return nil, nil, err
	}
	if expected == logicaltime.HLAinteger64Time {
		return nil, exact, nil
	}
	return &legacy, exact, nil
}
