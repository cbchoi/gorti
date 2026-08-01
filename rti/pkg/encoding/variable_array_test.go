package encoding

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// TestComposite_HLAvariableArray_RoundTrip exercises the variable-array
// codec. Wire layout: 4-byte BE element count, then N concatenated element
// encodings with element-boundary padding between them. The composite's
// OctetBoundary is max(4, element boundary) — the length prefix forces at
// least 4-byte alignment.
func TestComposite_HLAvariableArray_RoundTrip(t *testing.T) {
	t.Parallel()

	type tc struct {
		name     string
		elem     Codec
		boundary int
		value    []any
		hexBytes string
	}

	cases := []tc{
		{
			name:     "empty-int32",
			elem:     HLAinteger32BE{},
			boundary: 4,
			value:    []any{},
			hexBytes: "00000000",
		},
		{
			name:     "int32-2",
			elem:     HLAinteger32BE{},
			boundary: 4,
			value:    []any{int32(1), int32(2)},
			hexBytes: "000000020000000100000002",
		},
		{
			name:     "float64-3",
			elem:     hlaFloat64BE{},
			boundary: 8,
			// 4-byte length + 4 bytes pad to reach 8-byte boundary, then 3*8.
			value:    []any{1.0, 0.5, -2.0},
			hexBytes: "00000003000000003ff00000000000003fe0000000000000c000000000000000",
		},
		{
			name:     "octet-3",
			elem:     HLAoctet{},
			boundary: 4,
			value:    []any{byte(0xAA), byte(0xBB), byte(0xCC)},
			hexBytes: "00000003aabbcc",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			codec := NewVariableArray(c.elem)
			if got := codec.OctetBoundary(); got != c.boundary {
				t.Errorf("OctetBoundary() = %d, want %d", got, c.boundary)
			}

			expected, err := hex.DecodeString(c.hexBytes)
			if err != nil {
				t.Fatalf("hex %q: %v", c.hexBytes, err)
			}

			encoded, err := codec.Encode(c.value)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if !bytes.Equal(encoded, expected) {
				t.Errorf("Encode = %x, want %x", encoded, expected)
			}

			decoded, n, err := codec.Decode(expected)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if n != len(expected) {
				t.Errorf("Decode consumed %d bytes, want %d", n, len(expected))
			}

			reEncoded, err := codec.Encode(decoded)
			if err != nil {
				t.Fatalf("re-Encode: %v", err)
			}
			if !bytes.Equal(reEncoded, expected) {
				t.Errorf("round-trip = %x, want %x", reEncoded, expected)
			}
		})
	}
}

// TestComposite_HLAvariableArray_DecodeShortBuffer verifies length-prefix
// underflow and payload-truncation paths.
func TestComposite_HLAvariableArray_DecodeShortBuffer(t *testing.T) {
	t.Parallel()
	codec := NewVariableArray(HLAinteger32BE{})

	// length prefix truncated
	if _, _, err := codec.Decode([]byte{0, 0, 0}); err == nil {
		t.Errorf("Decode(short prefix) returned nil error")
	}
	// length prefix says 2, only 1 element on the wire
	if _, _, err := codec.Decode([]byte{0, 0, 0, 2, 0, 0, 0, 1}); err == nil {
		t.Errorf("Decode(short payload) returned nil error")
	}
}

// TestComposite_HLAvariableArray_EncodeRejectsNonSlice pins the type
// contract on Encode.
func TestComposite_HLAvariableArray_EncodeRejectsNonSlice(t *testing.T) {
	t.Parallel()
	codec := NewVariableArray(HLAinteger32BE{})
	if _, err := codec.Encode("not a slice"); err == nil {
		t.Errorf("Encode(string) returned nil error, want type error")
	}
	if _, err := codec.Encode([]any{"not an int"}); err == nil {
		t.Errorf("Encode([1]any{string}) returned nil error, want element error")
	}
}

// TestComposite_HLAvariableArray_NilElementCodec pins the constructor
// invariant.
func TestComposite_HLAvariableArray_NilElementCodec(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewVariableArray(nil) did not panic")
		}
	}()
	_ = NewVariableArray(nil)
}
