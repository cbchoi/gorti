package encoding

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// IEEE 754 float primitive codecs per IEEE 1516.2-2010 §4.
//
// The four types differ only in width (32 vs. 64 bits) and byte order
// (big-endian vs. little-endian). Each implements the Codec interface
// declared in codec.go; signatures must not drift.
//
// Octet boundary per §4: 4 for HLAfloat32*, 8 for HLAfloat64*.
//
// Type-acceptance policy (cross-language friendly):
//   - HLAfloat32* Encode accepts native float32 or float64 (the latter is
//     converted via float32(v); use exactly-representable values to avoid
//     surprise rounding — see docs/agent-b-fom-encoding.md §7).
//   - HLAfloat32* Decode returns float64 (widened) so vectors loaded from
//     JSON (which decodes numbers to float64) can be compared directly and
//     the value mirrors the Python decoder which has only one float type.
//   - HLAfloat64* Encode accepts native float64 or float32 (widened).
//   - HLAfloat64* Decode returns float64.

// errFloatShortBuffer is returned by Decode when the input is too short to
// hold the value.
var errFloatShortBuffer = errors.New("encoding: short buffer for float decode")

// asFloat32 narrows v to float32 if v is float32 or float64. Returns false if
// v is some other Go type. The caller must ensure the value is exactly
// representable in single precision when round-trip equality matters.
func asFloat32(v any) (float32, bool) {
	switch f := v.(type) {
	case float32:
		return f, true
	case float64:
		return float32(f), true
	default:
		return 0, false
	}
}

// asFloat64 widens v to float64 if v is float32 or float64. Returns false
// for other Go types.
func asFloat64(v any) (float64, bool) {
	switch f := v.(type) {
	case float64:
		return f, true
	case float32:
		return float64(f), true
	default:
		return 0, false
	}
}

// hlaFloat32BE is the codec for HLAfloat32BE (IEEE 754 single, big-endian).
type hlaFloat32BE struct{}

func (hlaFloat32BE) Encode(v any) ([]byte, error) {
	f, ok := asFloat32(v)
	if !ok {
		return nil, fmt.Errorf("encoding: HLAfloat32BE.Encode: want float32 or float64, got %T", v)
	}
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, math.Float32bits(f))
	return out, nil
}

func (hlaFloat32BE) Decode(b []byte) (any, int, error) {
	if len(b) < 4 {
		return nil, 0, errFloatShortBuffer
	}
	bits := binary.BigEndian.Uint32(b[:4])
	return float64(math.Float32frombits(bits)), 4, nil
}

func (hlaFloat32BE) OctetBoundary() int { return 4 }

// hlaFloat32LE is the codec for HLAfloat32LE (IEEE 754 single, little-endian).
type hlaFloat32LE struct{}

func (hlaFloat32LE) Encode(v any) ([]byte, error) {
	f, ok := asFloat32(v)
	if !ok {
		return nil, fmt.Errorf("encoding: HLAfloat32LE.Encode: want float32 or float64, got %T", v)
	}
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, math.Float32bits(f))
	return out, nil
}

func (hlaFloat32LE) Decode(b []byte) (any, int, error) {
	if len(b) < 4 {
		return nil, 0, errFloatShortBuffer
	}
	bits := binary.LittleEndian.Uint32(b[:4])
	return float64(math.Float32frombits(bits)), 4, nil
}

func (hlaFloat32LE) OctetBoundary() int { return 4 }

// hlaFloat64BE is the codec for HLAfloat64BE (IEEE 754 double, big-endian).
type hlaFloat64BE struct{}

func (hlaFloat64BE) Encode(v any) ([]byte, error) {
	f, ok := asFloat64(v)
	if !ok {
		return nil, fmt.Errorf("encoding: HLAfloat64BE.Encode: want float32 or float64, got %T", v)
	}
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, math.Float64bits(f))
	return out, nil
}

func (hlaFloat64BE) Decode(b []byte) (any, int, error) {
	if len(b) < 8 {
		return nil, 0, errFloatShortBuffer
	}
	bits := binary.BigEndian.Uint64(b[:8])
	return math.Float64frombits(bits), 8, nil
}

func (hlaFloat64BE) OctetBoundary() int { return 8 }

// hlaFloat64LE is the codec for HLAfloat64LE (IEEE 754 double, little-endian).
type hlaFloat64LE struct{}

func (hlaFloat64LE) Encode(v any) ([]byte, error) {
	f, ok := asFloat64(v)
	if !ok {
		return nil, fmt.Errorf("encoding: HLAfloat64LE.Encode: want float32 or float64, got %T", v)
	}
	out := make([]byte, 8)
	binary.LittleEndian.PutUint64(out, math.Float64bits(f))
	return out, nil
}

func (hlaFloat64LE) Decode(b []byte) (any, int, error) {
	if len(b) < 8 {
		return nil, 0, errFloatShortBuffer
	}
	bits := binary.LittleEndian.Uint64(b[:8])
	return math.Float64frombits(bits), 8, nil
}

func (hlaFloat64LE) OctetBoundary() int { return 8 }
