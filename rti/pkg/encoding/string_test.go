package encoding

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// TestPrimitive_StringCodecs_RoundTrip is the table-driven specification for
// the two HLA Evolved string primitives introduced by TASK-013:
//
//   - HLAASCIIstring   4-byte BE length prefix (count of code units)
//                      + N single-byte ASCII characters; OctetBoundary == 4.
//   - HLAunicodeString 4-byte BE length prefix (count of UTF-16 code units)
//                      + N two-byte UTF-16BE code units; OctetBoundary == 4.
//
// Surrogate pairs (non-BMP code points) are intentionally out of scope per
// the TASK-013 brief; the encoder is required to reject them.
func TestPrimitive_StringCodecs_RoundTrip(t *testing.T) {
	t.Parallel()

	type tc struct {
		name     string
		typeName string
		boundary int
		value    string
		hexBytes string
	}

	cases := []tc{
		// HLAASCIIstring: length prefix is the byte count (== code unit count).
		{"ascii-empty", "HLAASCIIstring", 4, "", "00000000"},
		{"ascii-hello", "HLAASCIIstring", 4, "hello", "0000000568656c6c6f"},
		{"ascii-space", "HLAASCIIstring", 4, " ", "0000000120"},
		{"ascii-A", "HLAASCIIstring", 4, "A", "0000000141"},

		// HLAunicodeString: length prefix is the UTF-16 code unit count;
		// payload is N*2 bytes (BMP only).
		{"unicode-empty", "HLAunicodeString", 4, "", "00000000"},
		{"unicode-hello", "HLAunicodeString", 4, "hello", "0000000500680065006c006c006f"},
		{"unicode-omega", "HLAunicodeString", 4, "Ω", "0000000103a9"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			codec, err := PrimitiveByName(c.typeName)
			if err != nil {
				t.Fatalf("PrimitiveByName(%q): %v", c.typeName, err)
			}
			if got := codec.OctetBoundary(); got != c.boundary {
				t.Errorf("OctetBoundary() = %d, want %d", got, c.boundary)
			}

			expected, err := hex.DecodeString(c.hexBytes)
			if err != nil {
				t.Fatalf("hex %q: %v", c.hexBytes, err)
			}

			encoded, err := codec.Encode(c.value)
			if err != nil {
				t.Fatalf("Encode(%q): %v", c.value, err)
			}
			if !bytes.Equal(encoded, expected) {
				t.Errorf("Encode(%q) = %x, want %x", c.value, encoded, expected)
			}

			decoded, n, err := codec.Decode(expected)
			if err != nil {
				t.Fatalf("Decode(%x): %v", expected, err)
			}
			if n != len(expected) {
				t.Errorf("Decode consumed %d bytes, want %d", n, len(expected))
			}
			if got, ok := decoded.(string); !ok || got != c.value {
				t.Errorf("Decode(%x) = %v (%T), want %q (string)", expected, decoded, decoded, c.value)
			}
		})
	}
}

// TestPrimitive_HLAASCIIstring_EncodeRejectsNonASCII pins that ASCII-only
// strings are accepted; the codec must reject high-bit-set bytes.
func TestPrimitive_HLAASCIIstring_EncodeRejectsNonASCII(t *testing.T) {
	t.Parallel()
	codec := HLAASCIIstring{}
	if _, err := codec.Encode("é"); err == nil { // é (U+00E9, multi-byte UTF-8)
		t.Errorf("Encode(\"é\") returned nil error, want non-ASCII error")
	}
	if _, err := codec.Encode(string([]byte{0x80})); err == nil {
		t.Errorf("Encode(byte 0x80) returned nil error")
	}
	// Wrong type: not a string.
	if _, err := codec.Encode(123); err == nil {
		t.Errorf("Encode(int) returned nil error, want type error")
	}
}

// TestPrimitive_HLAunicodeString_EncodeRejectsNonBMP pins that surrogate-pair
// (non-BMP) code points fail; per the brief they are out of scope.
func TestPrimitive_HLAunicodeString_EncodeRejectsNonBMP(t *testing.T) {
	t.Parallel()
	codec := HLAunicodeString{}
	if _, err := codec.Encode("\U0001F600"); err == nil {
		t.Errorf("Encode(emoji) returned nil error, want out-of-BMP error")
	}
	if _, err := codec.Encode(123); err == nil {
		t.Errorf("Encode(int) returned nil error, want type error")
	}
}

// TestPrimitive_StringCodecs_DecodeShortBuffer covers all the truncation
// paths: empty buffer, length-prefix-only-half, payload truncated.
func TestPrimitive_StringCodecs_DecodeShortBuffer(t *testing.T) {
	t.Parallel()
	asciiCodec := HLAASCIIstring{}
	uniCodec := HLAunicodeString{}

	for _, c := range []struct {
		name string
		buf  []byte
	}{
		{"empty", nil},
		{"three-bytes", []byte{0x00, 0x00, 0x00}},
	} {
		c := c
		t.Run("ascii-"+c.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := asciiCodec.Decode(c.buf); err == nil {
				t.Errorf("ASCII Decode(%x) returned nil error", c.buf)
			}
		})
		t.Run("unicode-"+c.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := uniCodec.Decode(c.buf); err == nil {
				t.Errorf("Unicode Decode(%x) returned nil error", c.buf)
			}
		})
	}

	// Length prefix says 5 chars but payload only has 1 byte.
	asciiTrunc := []byte{0x00, 0x00, 0x00, 0x05, 0x68}
	if _, _, err := asciiCodec.Decode(asciiTrunc); err == nil {
		t.Errorf("ASCII Decode(truncated payload) returned nil error")
	}
	// Length prefix says 2 code units (4 bytes) but payload only has 2.
	uniTrunc := []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x68}
	if _, _, err := uniCodec.Decode(uniTrunc); err == nil {
		t.Errorf("Unicode Decode(truncated payload) returned nil error")
	}
}

// TestPrimitive_HLAunicodeString_DecodeRejectsSurrogate ensures decoding
// rejects unpaired surrogate code units in the payload (BMP only).
func TestPrimitive_HLAunicodeString_DecodeRejectsSurrogate(t *testing.T) {
	t.Parallel()
	codec := HLAunicodeString{}
	// Length prefix 1, then 0xD800 (high surrogate).
	buf := []byte{0x00, 0x00, 0x00, 0x01, 0xD8, 0x00}
	if _, _, err := codec.Decode(buf); err == nil {
		t.Errorf("Decode(surrogate) returned nil error, want surrogate error")
	}
}
