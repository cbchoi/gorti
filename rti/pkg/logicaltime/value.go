// Package logicaltime implements the standardized IEEE 1516-2010 time values.
// Integer64 arithmetic and encoding never convert through float64. These value
// types do not by themselves enable Integer64 federations on a Float64 server.
package logicaltime

import (
	"encoding/binary"
	"errors"
	"math"
	"strconv"
)

var (
	ErrInvalidValue           = errors.New("logicaltime: value must be finite and nonnegative")
	ErrUnknownRepresentation  = errors.New("logicaltime: unknown representation")
	ErrRepresentationMismatch = errors.New("logicaltime: representations differ")
	ErrIllegalArithmetic      = errors.New("logicaltime: result is outside the representation range")
	ErrInvalidEncoding        = errors.New("logicaltime: encoded value must contain exactly eight bytes")
)

type Representation uint8

const (
	Unspecified Representation = iota
	HLAfloat64Time
	HLAinteger64Time
)

func (r Representation) String() string {
	switch r {
	case HLAfloat64Time:
		return "HLAfloat64Time"
	case HLAinteger64Time:
		return "HLAinteger64Time"
	default:
		return ""
	}
}

func ParseRepresentation(name string) (Representation, error) {
	switch name {
	case "HLAfloat64Time":
		return HLAfloat64Time, nil
	case "HLAinteger64Time":
		return HLAinteger64Time, nil
	default:
		return Unspecified, ErrUnknownRepresentation
	}
}

// Time and Interval are immutable. Their zero values are uninitialized, not
// initial time/zero interval; use a representation's constructors explicitly.
type Time struct{ value scalar }
type Interval struct{ value scalar }

type scalar struct {
	kind Representation
	bits uint64
}

func Float64(value float64) (Time, error) {
	v, err := floatValue(value)
	return Time{v}, err
}

func Integer64(value int64) (Time, error) {
	v, err := integerValue(value)
	return Time{v}, err
}

func Float64Interval(value float64) (Interval, error) {
	v, err := floatValue(value)
	return Interval{v}, err
}

func Integer64Interval(value int64) (Interval, error) {
	v, err := integerValue(value)
	return Interval{v}, err
}

func floatValue(value float64) (scalar, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return scalar{}, ErrInvalidValue
	}
	if value == 0 {
		value = 0
	}
	return scalar{HLAfloat64Time, math.Float64bits(value)}, nil
}

func integerValue(value int64) (scalar, error) {
	if value < 0 {
		return scalar{}, ErrInvalidValue
	}
	return scalar{HLAinteger64Time, uint64(value)}, nil
}

func (r Representation) Initial() (Time, error) {
	switch r {
	case HLAfloat64Time:
		return Float64(0)
	case HLAinteger64Time:
		return Integer64(0)
	default:
		return Time{}, ErrUnknownRepresentation
	}
}

func (r Representation) Final() (Time, error) {
	switch r {
	case HLAfloat64Time:
		return Float64(math.MaxFloat64)
	case HLAinteger64Time:
		return Integer64(math.MaxInt64)
	default:
		return Time{}, ErrUnknownRepresentation
	}
}

func (r Representation) Zero() (Interval, error) {
	value, err := r.Initial()
	return Interval{value.value}, err
}

func (r Representation) Epsilon() (Interval, error) {
	switch r {
	case HLAfloat64Time:
		return Float64Interval(math.SmallestNonzeroFloat64)
	case HLAinteger64Time:
		return Integer64Interval(1)
	default:
		return Interval{}, ErrUnknownRepresentation
	}
}

func (s scalar) valid() bool                      { return s.kind == HLAfloat64Time || s.kind == HLAinteger64Time }
func (t Time) Representation() Representation     { return t.value.kind }
func (i Interval) Representation() Representation { return i.value.kind }
func (t Time) IsInitial() bool                    { return t.value.valid() && t.value.bits == 0 }
func (i Interval) IsZero() bool                   { return i.value.valid() && i.value.bits == 0 }
func (t Time) IsFinal() bool {
	final, err := t.Representation().Final()
	return err == nil && t == final
}
func (i Interval) IsEpsilon() bool {
	epsilon, err := i.Representation().Epsilon()
	return err == nil && i == epsilon
}

func (s scalar) floating() (float64, error) {
	if s.kind != HLAfloat64Time {
		return 0, ErrRepresentationMismatch
	}
	return math.Float64frombits(s.bits), nil
}

func (s scalar) integer() (int64, error) {
	if s.kind != HLAinteger64Time {
		return 0, ErrRepresentationMismatch
	}
	if s.bits > math.MaxInt64 {
		return 0, ErrInvalidValue
	}
	//nolint:gosec // G115: the MaxInt64 check above bounds the unsigned representation.
	return int64(s.bits), nil
}

func (t Time) Float64() (float64, error)     { return t.value.floating() }
func (t Time) Integer64() (int64, error)     { return t.value.integer() }
func (i Interval) Float64() (float64, error) { return i.value.floating() }
func (i Interval) Integer64() (int64, error) { return i.value.integer() }

func compatible(a, b scalar) error {
	if !a.valid() || !b.valid() {
		return ErrUnknownRepresentation
	}
	if a.kind != b.kind {
		return ErrRepresentationMismatch
	}
	return nil
}

func compare(a, b scalar) (int, error) {
	if err := compatible(a, b); err != nil {
		return 0, err
	}
	// Nonnegative canonical Float64 encodings have the same order as their
	// unsigned bit patterns. Integer64 uses the complete positive int64 range.
	if a.bits < b.bits {
		return -1, nil
	}
	if a.bits > b.bits {
		return 1, nil
	}
	return 0, nil
}

func (t Time) Compare(other Time) (int, error)         { return compare(t.value, other.value) }
func (i Interval) Compare(other Interval) (int, error) { return compare(i.value, other.value) }

func arithmetic(a, b scalar, subtract bool) (scalar, error) {
	if err := compatible(a, b); err != nil {
		return scalar{}, err
	}
	if a.kind == HLAinteger64Time {
		if subtract {
			if b.bits > a.bits {
				return scalar{}, ErrIllegalArithmetic
			}
			return scalar{a.kind, a.bits - b.bits}, nil
		}
		if b.bits > math.MaxInt64-a.bits {
			return scalar{}, ErrIllegalArithmetic
		}
		return scalar{a.kind, a.bits + b.bits}, nil
	}
	left, right := math.Float64frombits(a.bits), math.Float64frombits(b.bits)
	result, direction := left+right, math.Inf(1)
	if subtract {
		result, direction = left-right, math.Inf(-1)
	}
	if right != 0 && result == left {
		result = math.Nextafter(left, direction)
	}
	if math.IsInf(result, 0) || result < 0 {
		return scalar{}, ErrIllegalArithmetic
	}
	return floatValue(result)
}

func (t Time) Add(interval Interval) (Time, error) {
	value, err := arithmetic(t.value, interval.value, false)
	return Time{value}, err
}

func (t Time) Subtract(interval Interval) (Time, error) {
	value, err := arithmetic(t.value, interval.value, true)
	return Time{value}, err
}

func (i Interval) Add(other Interval) (Interval, error) {
	value, err := arithmetic(i.value, other.value, false)
	return Interval{value}, err
}

func (i Interval) Subtract(other Interval) (Interval, error) {
	value, err := arithmetic(i.value, other.value, true)
	return Interval{value}, err
}

// Distance is nonnegative, regardless of operand order.
func (t Time) Distance(other Time) (Interval, error) {
	order, err := t.Compare(other)
	if err != nil {
		return Interval{}, err
	}
	left, right := t.value, other.value
	if order < 0 {
		left, right = right, left
	}
	if left.kind == HLAfloat64Time {
		return Float64Interval(math.Float64frombits(left.bits) - math.Float64frombits(right.bits))
	}
	value, err := arithmetic(left, right, true)
	return Interval{value}, err
}

func (s scalar) encode() ([]byte, error) {
	if !s.valid() {
		return nil, ErrUnknownRepresentation
	}
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, s.bits)
	return bytes, nil
}

func decode(kind Representation, bytes []byte) (scalar, error) {
	if len(bytes) != 8 {
		return scalar{}, ErrInvalidEncoding
	}
	bits := binary.BigEndian.Uint64(bytes)
	switch kind {
	case HLAinteger64Time:
		if bits > math.MaxInt64 {
			return scalar{}, ErrInvalidValue
		}
		return integerValue(int64(bits))
	case HLAfloat64Time:
		return floatValue(math.Float64frombits(bits))
	default:
		return scalar{}, ErrUnknownRepresentation
	}
}

func (t Time) MarshalBinary() ([]byte, error)     { return t.value.encode() }
func (i Interval) MarshalBinary() ([]byte, error) { return i.value.encode() }

func DecodeTime(kind Representation, bytes []byte) (Time, error) {
	value, err := decode(kind, bytes)
	return Time{value}, err
}

func DecodeInterval(kind Representation, bytes []byte) (Interval, error) {
	value, err := decode(kind, bytes)
	return Interval{value}, err
}

func (s scalar) String() string {
	if s.kind == HLAinteger64Time {
		return s.kind.String() + "(" + strconv.FormatUint(s.bits, 10) + ")"
	}
	if s.kind == HLAfloat64Time {
		return s.kind.String() + "(" + strconv.FormatFloat(math.Float64frombits(s.bits), 'g', -1, 64) + ")"
	}
	return "uninitialized"
}

func (t Time) String() string     { return t.value.String() }
func (i Interval) String() string { return i.value.String() }
