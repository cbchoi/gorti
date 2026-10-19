package core

import "github.com/cbchoi/gorti/rti/pkg/logicaltime"

// LogicalTimeScalar lets one service algorithm retain its federation's exact
// representation. It excludes wall-clock durations and DDM range coordinates.
type LogicalTimeScalar interface{ ~float64 | ~int64 }

func ScalarRepresentation[T LogicalTimeScalar]() logicaltime.Representation {
	if T(1)/T(2) == 0 {
		return logicaltime.HLAinteger64Time
	}
	return logicaltime.HLAfloat64Time
}

func ScalarTimeValue[T LogicalTimeScalar](value T) (logicaltime.Time, error) {
	if ScalarRepresentation[T]() == logicaltime.HLAinteger64Time {
		return logicaltime.Integer64(int64(value))
	}
	return logicaltime.Float64(float64(value))
}

func ScalarIntervalValue[T LogicalTimeScalar](value T) (logicaltime.Interval, error) {
	if ScalarRepresentation[T]() == logicaltime.HLAinteger64Time {
		return logicaltime.Integer64Interval(int64(value))
	}
	return logicaltime.Float64Interval(float64(value))
}

func ScalarFromTimeValue[T LogicalTimeScalar](value logicaltime.Time) (T, error) {
	if value.Representation() != ScalarRepresentation[T]() {
		return 0, logicaltime.ErrRepresentationMismatch
	}
	if value.Representation() == logicaltime.HLAinteger64Time {
		integer, err := value.Integer64()
		return T(integer), err
	}
	floating, err := value.Float64()
	return T(floating), err
}

func ScalarFromIntervalValue[T LogicalTimeScalar](value logicaltime.Interval) (T, error) {
	if value.Representation() != ScalarRepresentation[T]() {
		return 0, logicaltime.ErrRepresentationMismatch
	}
	if value.Representation() == logicaltime.HLAinteger64Time {
		integer, err := value.Integer64()
		return T(integer), err
	}
	floating, err := value.Float64()
	return T(floating), err
}
