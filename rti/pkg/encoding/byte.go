package encoding

import (
	"errors"
	"fmt"
	"unicode/utf16"
)

// This file implements the byte-level primitive codecs introduced by
// TASK-012, per IEEE 1516.2-2010 §4:
//
//   HLAoctet         single byte                          (boundary 1)
//   HLAoctetPairBE   two bytes, big-endian order          (boundary 2)
//   HLAoctetPairLE   two bytes, little-endian order       (boundary 2)
//   HLAboolean       encoded as HLAinteger32BE (1 / 0)    (boundary 4)
//   HLAASCIIchar     single byte, 0..127                  (boundary 1)
//   HLAunicodeChar   UTF-16BE, 2 bytes (BMP only)         (boundary 2)
//
// Surrogate-pair handling for non-BMP code points is out of scope for
// HLAunicodeChar — the spec encodes a single 16-bit unit; supplementary
// code points belong in HLAunicodeString (TASK-013).

// ----- HLAoctet -------------------------------------------------------------

// HLAoctet is the 1516.2-2010 single-octet codec.
type HLAoctet struct{}

// OctetBoundary returns the alignment boundary for HLAoctet (1 byte).
func (HLAoctet) OctetBoundary() int { return 1 }

// Encode accepts byte/uint8 directly and integer / float64 values
// representable as a byte (0..255).
func (HLAoctet) Encode(v any) ([]byte, error) {
	b, err := octetFromAny(v)
	if err != nil {
		return nil, err
	}
	return []byte{b}, nil
}

// octetFromAny coerces a Go value to a single byte in [0,255]. It accepts
// byte, the signed and unsigned integer widths typically produced by
// hand-written Go and JSON decoding, and float64 (provided the value is
// integral and in range).
func octetFromAny(v any) (byte, error) {
	switch x := v.(type) {
	case byte:
		return x, nil
	case int:
		return rangeCheckByte(int64(x))
	case int32:
		return rangeCheckByte(int64(x))
	case int64:
		return rangeCheckByte(x)
	case uint:
		if x > 0xFF {
			return 0, fmt.Errorf("encoding: HLAoctet value %d out of range [0,255]", x)
		}
		return byte(x), nil
	case float64:
		// JSON numeric vector values arrive here as float64.
		if x != float64(int64(x)) {
			return 0, fmt.Errorf("encoding: HLAoctet value %v not an integer", x)
		}
		return rangeCheckByte(int64(x))
	default:
		return 0, fmt.Errorf("encoding: HLAoctet cannot encode %T", v)
	}
}

func rangeCheckByte(x int64) (byte, error) {
	if x < 0 || x > 0xFF {
		return 0, fmt.Errorf("encoding: HLAoctet value %d out of range [0,255]", x)
	}
	return byte(x), nil
}

// Decode reads exactly one byte. Returns the byte as uint8 and n=1.
func (HLAoctet) Decode(b []byte) (any, int, error) {
	if len(b) < 1 {
		return nil, 0, errors.New("encoding: HLAoctet decode short buffer")
	}
	return b[0], 1, nil
}

// ----- HLAoctetPairBE / HLAoctetPairLE --------------------------------------

// HLAoctetPairBE is the 2-octet big-endian pair codec.
type HLAoctetPairBE struct{}

// OctetBoundary returns 2.
func (HLAoctetPairBE) OctetBoundary() int { return 2 }

// Encode accepts [2]byte or []byte of length 2.
func (HLAoctetPairBE) Encode(v any) ([]byte, error) {
	a, b, err := octetPairBytes(v)
	if err != nil {
		return nil, err
	}
	return []byte{a, b}, nil
}

// Decode returns the pair as [2]byte preserving its in-buffer order.
func (HLAoctetPairBE) Decode(b []byte) (any, int, error) {
	if len(b) < 2 {
		return nil, 0, errors.New("encoding: HLAoctetPairBE decode short buffer")
	}
	return [2]byte{b[0], b[1]}, 2, nil
}

// HLAoctetPairLE is the 2-octet little-endian pair codec.
type HLAoctetPairLE struct{}

// OctetBoundary returns 2.
func (HLAoctetPairLE) OctetBoundary() int { return 2 }

// Encode accepts [2]byte or []byte of length 2 and emits the pair in
// little-endian byte order (the second logical byte first).
func (HLAoctetPairLE) Encode(v any) ([]byte, error) {
	a, b, err := octetPairBytes(v)
	if err != nil {
		return nil, err
	}
	return []byte{b, a}, nil
}

// Decode reads two bytes and returns them swapped back to logical order.
func (HLAoctetPairLE) Decode(b []byte) (any, int, error) {
	if len(b) < 2 {
		return nil, 0, errors.New("encoding: HLAoctetPairLE decode short buffer")
	}
	return [2]byte{b[1], b[0]}, 2, nil
}

func octetPairBytes(v any) (byte, byte, error) {
	switch x := v.(type) {
	case [2]byte:
		return x[0], x[1], nil
	case []byte:
		if len(x) != 2 {
			return 0, 0, fmt.Errorf("encoding: HLAoctetPair requires exactly 2 bytes, got %d", len(x))
		}
		return x[0], x[1], nil
	default:
		return 0, 0, fmt.Errorf("encoding: HLAoctetPair cannot encode %T", v)
	}
}

// ----- HLAboolean -----------------------------------------------------------

// HLAboolean is encoded as HLAinteger32BE per IEEE 1516.2-2010 §4: value 1
// for true, 0 for false. Decode treats any non-zero 32-bit value as true.
type HLAboolean struct{}

// OctetBoundary returns 4 (inherited from HLAinteger32BE).
func (HLAboolean) OctetBoundary() int { return 4 }

// Encode accepts only bool.
func (HLAboolean) Encode(v any) ([]byte, error) {
	x, ok := v.(bool)
	if !ok {
		return nil, fmt.Errorf("encoding: HLAboolean cannot encode %T", v)
	}
	if x {
		return []byte{0x00, 0x00, 0x00, 0x01}, nil
	}
	return []byte{0x00, 0x00, 0x00, 0x00}, nil
}

// Decode reads 4 bytes; non-zero is true.
func (HLAboolean) Decode(b []byte) (any, int, error) {
	if len(b) < 4 {
		return nil, 0, errors.New("encoding: HLAboolean decode short buffer")
	}
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	return v != 0, 4, nil
}

// ----- HLAASCIIchar ---------------------------------------------------------

// HLAASCIIchar is a single ASCII octet (0..127).
type HLAASCIIchar struct{}

// OctetBoundary returns 1.
func (HLAASCIIchar) OctetBoundary() int { return 1 }

// Encode accepts string of length 1 (ASCII), rune in [0,127], or byte in
// [0,127].
func (HLAASCIIchar) Encode(v any) ([]byte, error) {
	switch x := v.(type) {
	case string:
		if len(x) != 1 {
			return nil, fmt.Errorf("encoding: HLAASCIIchar requires single-character string, got len %d", len(x))
		}
		c := x[0]
		if c > 0x7F {
			return nil, fmt.Errorf("encoding: HLAASCIIchar value 0x%02X out of ASCII range", c)
		}
		return []byte{c}, nil
	case rune:
		if x < 0 || x > 0x7F {
			return nil, fmt.Errorf("encoding: HLAASCIIchar rune 0x%X out of ASCII range", x)
		}
		return []byte{byte(x)}, nil
	case byte:
		if x > 0x7F {
			return nil, fmt.Errorf("encoding: HLAASCIIchar byte 0x%02X out of ASCII range", x)
		}
		return []byte{x}, nil
	default:
		return nil, fmt.Errorf("encoding: HLAASCIIchar cannot encode %T", v)
	}
}

// Decode reads one byte and returns a single-character string.
func (HLAASCIIchar) Decode(b []byte) (any, int, error) {
	if len(b) < 1 {
		return nil, 0, errors.New("encoding: HLAASCIIchar decode short buffer")
	}
	if b[0] > 0x7F {
		return nil, 0, fmt.Errorf("encoding: HLAASCIIchar byte 0x%02X out of ASCII range", b[0])
	}
	return string([]byte{b[0]}), 1, nil
}

// ----- HLAunicodeChar -------------------------------------------------------

// HLAunicodeChar is a 2-byte UTF-16BE Basic Multilingual Plane code unit.
// Surrogate-pair handling is out of scope for TASK-012.
type HLAunicodeChar struct{}

// OctetBoundary returns 2.
func (HLAunicodeChar) OctetBoundary() int { return 2 }

// Encode accepts a single-rune string or a rune; rejects non-BMP code points.
func (HLAunicodeChar) Encode(v any) ([]byte, error) {
	var r rune
	switch x := v.(type) {
	case string:
		runes := []rune(x)
		if len(runes) != 1 {
			return nil, fmt.Errorf("encoding: HLAunicodeChar requires single-rune string, got %d runes", len(runes))
		}
		r = runes[0]
	case rune:
		r = x
	default:
		return nil, fmt.Errorf("encoding: HLAunicodeChar cannot encode %T", v)
	}
	if r < 0 || r > 0xFFFF {
		return nil, fmt.Errorf("encoding: HLAunicodeChar code point U+%04X outside BMP (surrogate pairs out of scope)", r)
	}
	if utf16.IsSurrogate(r) {
		return nil, fmt.Errorf("encoding: HLAunicodeChar code point U+%04X is a UTF-16 surrogate", r)
	}
	u := uint16(r)
	return []byte{byte(u >> 8), byte(u)}, nil
}

// Decode reads 2 bytes UTF-16BE and returns a single-rune string.
func (HLAunicodeChar) Decode(b []byte) (any, int, error) {
	if len(b) < 2 {
		return nil, 0, errors.New("encoding: HLAunicodeChar decode short buffer")
	}
	u := uint16(b[0])<<8 | uint16(b[1])
	if utf16.IsSurrogate(rune(u)) {
		return nil, 0, fmt.Errorf("encoding: HLAunicodeChar code unit U+%04X is a UTF-16 surrogate (BMP only)", u)
	}
	return string(rune(u)), 2, nil
}
