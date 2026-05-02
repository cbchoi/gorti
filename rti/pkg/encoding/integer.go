package encoding

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// ErrEncTypeMismatch is returned by Encode when the supplied value cannot be
// represented as the codec's native integer width. Callers should
// errors.Is(err, ErrEncTypeMismatch) when distinguishing user-input errors
// from buffer-shape errors during decoding.
var ErrEncTypeMismatch = errors.New("encoding: value type does not match codec width")

// ErrEncShortBuffer is returned by Decode when the input slice is shorter
// than the codec's storage width.
var ErrEncShortBuffer = errors.New("encoding: buffer too short for codec width")

// HLAinteger16BE encodes/decodes a 16-bit signed two's-complement integer in
// network (big-endian) byte order per IEEE 1516.2-2010 §4.6.
type HLAinteger16BE struct{}

// HLAinteger16LE encodes/decodes a 16-bit signed two's-complement integer in
// little-endian byte order per IEEE 1516.2-2010 §4.6.
type HLAinteger16LE struct{}

// HLAinteger32BE encodes/decodes a 32-bit signed two's-complement integer in
// network (big-endian) byte order per IEEE 1516.2-2010 §4.6.
type HLAinteger32BE struct{}

// HLAinteger32LE encodes/decodes a 32-bit signed two's-complement integer in
// little-endian byte order per IEEE 1516.2-2010 §4.6.
type HLAinteger32LE struct{}

// HLAinteger64BE encodes/decodes a 64-bit signed two's-complement integer in
// network (big-endian) byte order per IEEE 1516.2-2010 §4.6.
type HLAinteger64BE struct{}

// HLAinteger64LE encodes/decodes a 64-bit signed two's-complement integer in
// little-endian byte order per IEEE 1516.2-2010 §4.6.
type HLAinteger64LE struct{}

// Encode implements Codec.
func (HLAinteger16BE) Encode(v any) ([]byte, error) {
	x, err := toInt16(v)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(x))
	return b, nil
}

// Decode implements Codec. Returns int32 (widened from int16) so JSON-driven
// vector comparisons that round numbers through float64 succeed.
func (HLAinteger16BE) Decode(b []byte) (any, int, error) {
	if len(b) < 2 {
		return nil, 0, ErrEncShortBuffer
	}
	return int32(int16(binary.BigEndian.Uint16(b[:2]))), 2, nil
}

// OctetBoundary implements Codec.
func (HLAinteger16BE) OctetBoundary() int { return 2 }

// Encode implements Codec.
func (HLAinteger16LE) Encode(v any) ([]byte, error) {
	x, err := toInt16(v)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, uint16(x))
	return b, nil
}

// Decode implements Codec.
func (HLAinteger16LE) Decode(b []byte) (any, int, error) {
	if len(b) < 2 {
		return nil, 0, ErrEncShortBuffer
	}
	return int32(int16(binary.LittleEndian.Uint16(b[:2]))), 2, nil
}

// OctetBoundary implements Codec.
func (HLAinteger16LE) OctetBoundary() int { return 2 }

// Encode implements Codec.
func (HLAinteger32BE) Encode(v any) ([]byte, error) {
	x, err := toInt32(v)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(x))
	return b, nil
}

// Decode implements Codec.
func (HLAinteger32BE) Decode(b []byte) (any, int, error) {
	if len(b) < 4 {
		return nil, 0, ErrEncShortBuffer
	}
	return int32(binary.BigEndian.Uint32(b[:4])), 4, nil
}

// OctetBoundary implements Codec.
func (HLAinteger32BE) OctetBoundary() int { return 4 }

// Encode implements Codec.
func (HLAinteger32LE) Encode(v any) ([]byte, error) {
	x, err := toInt32(v)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(x))
	return b, nil
}

// Decode implements Codec.
func (HLAinteger32LE) Decode(b []byte) (any, int, error) {
	if len(b) < 4 {
		return nil, 0, ErrEncShortBuffer
	}
	return int32(binary.LittleEndian.Uint32(b[:4])), 4, nil
}

// OctetBoundary implements Codec.
func (HLAinteger32LE) OctetBoundary() int { return 4 }

// Encode implements Codec.
func (HLAinteger64BE) Encode(v any) ([]byte, error) {
	x, err := toInt64(v)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(x))
	return b, nil
}

// Decode implements Codec.
func (HLAinteger64BE) Decode(b []byte) (any, int, error) {
	if len(b) < 8 {
		return nil, 0, ErrEncShortBuffer
	}
	return int64(binary.BigEndian.Uint64(b[:8])), 8, nil
}

// OctetBoundary implements Codec.
func (HLAinteger64BE) OctetBoundary() int { return 8 }

// Encode implements Codec.
func (HLAinteger64LE) Encode(v any) ([]byte, error) {
	x, err := toInt64(v)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(x))
	return b, nil
}

// Decode implements Codec.
func (HLAinteger64LE) Decode(b []byte) (any, int, error) {
	if len(b) < 8 {
		return nil, 0, ErrEncShortBuffer
	}
	return int64(binary.LittleEndian.Uint64(b[:8])), 8, nil
}

// OctetBoundary implements Codec.
func (HLAinteger64LE) OctetBoundary() int { return 8 }

// toInt16 normalises an Encode input to int16, accepting the native Go
// integer widths (int8/16/32/64, int, uint8/16/32/64, uint), bool-style
// integers excluded, and float64 (which is what encoding/json produces for
// JSON-loaded vector values). Out-of-range values yield a wrapped error;
// non-numeric types yield ErrEncTypeMismatch.
func toInt16(v any) (int16, error) {
	x, err := toInt64Any(v)
	if err != nil {
		return 0, err
	}
	if x < math.MinInt16 || x > math.MaxInt16 {
		return 0, fmt.Errorf("encoding: value %d out of range for int16", x)
	}
	return int16(x), nil
}

// toInt32 normalises an Encode input to int32. See toInt16.
func toInt32(v any) (int32, error) {
	x, err := toInt64Any(v)
	if err != nil {
		return 0, err
	}
	if x < math.MinInt32 || x > math.MaxInt32 {
		return 0, fmt.Errorf("encoding: value %d out of range for int32", x)
	}
	return int32(x), nil
}

// toInt64 normalises an Encode input to int64. See toInt16.
func toInt64(v any) (int64, error) {
	return toInt64Any(v)
}

// toInt64Any is the shared coercion. Accepts the native Go signed integer
// widths (int16/int32/int64/int) plus float64 (which is what encoding/json
// produces for JSON-loaded vector values, when it is an exact integer in
// range). Anything else returns ErrEncTypeMismatch.
func toInt64Any(v any) (int64, error) {
	switch x := v.(type) {
	case int16:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, fmt.Errorf("%w: float64 value %v not representable", ErrEncTypeMismatch, x)
		}
		if x < math.MinInt64 || x > math.MaxInt64 {
			return 0, fmt.Errorf("encoding: float64 value %v out of int64 range", x)
		}
		if math.Trunc(x) != x {
			return 0, fmt.Errorf("%w: float64 value %v has fractional part", ErrEncTypeMismatch, x)
		}
		return int64(x), nil
	default:
		return 0, fmt.Errorf("%w: %T", ErrEncTypeMismatch, v)
	}
}
