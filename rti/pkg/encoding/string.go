package encoding

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf16"
)

// HLAASCIIstring and HLAunicodeString are the length-prefixed string
// primitives introduced by TASK-013, per IEEE 1516.2-2010 §4:
//
//   HLAASCIIstring   4-byte BE length prefix (count of code units == bytes)
//                    + N ASCII bytes (0..127). OctetBoundary == 4.
//
//   HLAunicodeString 4-byte BE length prefix (count of UTF-16 code units)
//                    + N two-byte UTF-16BE code units. OctetBoundary == 4.
//                    Surrogate pairs (non-BMP code points) are out of scope.
//
// The 4-byte length prefix is itself 4-byte aligned, which is why the
// OctetBoundary of both types is 4 even though the per-character widths
// are 1 and 2 respectively.

// errStringShortBuffer is returned when the decode buffer is too short for
// the declared length prefix or its payload.
var errStringShortBuffer = errors.New("encoding: short buffer for string decode")

// ----- HLAASCIIstring ------------------------------------------------------

// HLAASCIIstring is the length-prefixed ASCII string codec.
type HLAASCIIstring struct{}

// OctetBoundary returns 4 (the length-prefix alignment).
func (HLAASCIIstring) OctetBoundary() int { return 4 }

// Encode accepts a Go string whose bytes are all in the ASCII range 0..127.
// Non-ASCII bytes (high bit set) are rejected.
func (HLAASCIIstring) Encode(v any) ([]byte, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("encoding: HLAASCIIstring cannot encode %T", v)
	}
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7F {
			return nil, fmt.Errorf("encoding: HLAASCIIstring byte 0x%02X at offset %d out of ASCII range", s[i], i)
		}
	}
	out := make([]byte, 4+len(s))
	binary.BigEndian.PutUint32(out[:4], uint32(len(s)))
	copy(out[4:], s)
	return out, nil
}

// Decode reads a 4-byte BE length and that many ASCII bytes. Returns the
// decoded string and total bytes consumed (4 + length).
func (HLAASCIIstring) Decode(b []byte) (any, int, error) {
	if len(b) < 4 {
		return nil, 0, errStringShortBuffer
	}
	n := int(binary.BigEndian.Uint32(b[:4]))
	if n < 0 || 4+n > len(b) {
		return nil, 0, errStringShortBuffer
	}
	payload := b[4 : 4+n]
	for i, c := range payload {
		if c > 0x7F {
			return nil, 0, fmt.Errorf("encoding: HLAASCIIstring byte 0x%02X at offset %d out of ASCII range", c, i)
		}
	}
	return string(payload), 4 + n, nil
}

// ----- HLAunicodeString ----------------------------------------------------

// HLAunicodeString is the length-prefixed UTF-16BE string codec.
type HLAunicodeString struct{}

// OctetBoundary returns 4 (the length-prefix alignment).
func (HLAunicodeString) OctetBoundary() int { return 4 }

// Encode accepts a Go string whose runes are all in the BMP (0..0xFFFF,
// excluding the surrogate range). Each rune is emitted as one UTF-16BE
// code unit; the prefix counts code units, not bytes.
func (HLAunicodeString) Encode(v any) ([]byte, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("encoding: HLAunicodeString cannot encode %T", v)
	}
	runes := []rune(s)
	for i, r := range runes {
		if r < 0 || r > 0xFFFF {
			return nil, fmt.Errorf("encoding: HLAunicodeString rune U+%04X at index %d outside BMP (surrogate pairs out of scope)", r, i)
		}
		if utf16.IsSurrogate(r) {
			return nil, fmt.Errorf("encoding: HLAunicodeString rune U+%04X at index %d is a UTF-16 surrogate", r, i)
		}
	}
	out := make([]byte, 4+2*len(runes))
	binary.BigEndian.PutUint32(out[:4], uint32(len(runes)))
	for i, r := range runes {
		u := uint16(r)
		out[4+2*i] = byte(u >> 8)
		out[4+2*i+1] = byte(u)
	}
	return out, nil
}

// Decode reads a 4-byte BE length (in code units) and 2*N bytes of payload.
// Returns the decoded Go string and total bytes consumed (4 + 2*N).
func (HLAunicodeString) Decode(b []byte) (any, int, error) {
	if len(b) < 4 {
		return nil, 0, errStringShortBuffer
	}
	n := int(binary.BigEndian.Uint32(b[:4]))
	if n < 0 || 4+2*n > len(b) {
		return nil, 0, errStringShortBuffer
	}
	runes := make([]rune, n)
	for i := 0; i < n; i++ {
		u := uint16(b[4+2*i])<<8 | uint16(b[4+2*i+1])
		if utf16.IsSurrogate(rune(u)) {
			return nil, 0, fmt.Errorf("encoding: HLAunicodeString code unit U+%04X at index %d is a UTF-16 surrogate (BMP only)", u, i)
		}
		runes[i] = rune(u)
	}
	return string(runes), 4 + 2*n, nil
}
