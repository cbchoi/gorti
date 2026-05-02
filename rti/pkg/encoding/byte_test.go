package encoding

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// TestPrimitive_ByteCodecs_RoundTrip is a table-driven test exercising the
// six byte-level primitive codecs introduced by TASK-012:
//
//   - HLAoctet         (single byte; OctetBoundary == 1)
//   - HLAoctetPairBE   (two bytes BE; OctetBoundary == 2)
//   - HLAoctetPairLE   (two bytes LE; OctetBoundary == 2)
//   - HLAboolean       (encoded as HLAinteger32BE; OctetBoundary == 4)
//   - HLAASCIIchar     (single ASCII byte; OctetBoundary == 1)
//   - HLAunicodeChar   (UTF-16BE 2 bytes; OctetBoundary == 2)
//
// The test asserts: lookup via PrimitiveByName, declared OctetBoundary,
// expected encoded bytes, and decode produces a value that re-encodes
// to the same bytes.
func TestPrimitive_ByteCodecs_RoundTrip(t *testing.T) {
	t.Parallel()

	type tc struct {
		name     string
		typeName string
		boundary int
		value    any
		hexBytes string
	}

	cases := []tc{
		// HLAoctet
		{"octet-zero", "HLAoctet", 1, byte(0x00), "00"},
		{"octet-one", "HLAoctet", 1, byte(0x01), "01"},
		{"octet-max", "HLAoctet", 1, byte(0xFF), "ff"},
		{"octet-ab", "HLAoctet", 1, byte(0xAB), "ab"},

		// HLAboolean (encoded as HLAinteger32BE)
		{"boolean-true", "HLAboolean", 4, true, "00000001"},
		{"boolean-false", "HLAboolean", 4, false, "00000000"},

		// HLAASCIIchar
		{"ascii-char-A", "HLAASCIIchar", 1, "A", "41"},
		{"ascii-char-tilde", "HLAASCIIchar", 1, "~", "7e"},

		// HLAunicodeChar (UTF-16BE)
		{"unicode-char-A", "HLAunicodeChar", 2, "A", "0041"},
		{"unicode-char-omega", "HLAunicodeChar", 2, "Ω", "03a9"},

		// HLAoctetPairBE
		{"octet-pair-be-0001", "HLAoctetPairBE", 2, [2]byte{0x00, 0x01}, "0001"},
		{"octet-pair-be-abcd", "HLAoctetPairBE", 2, [2]byte{0xAB, 0xCD}, "abcd"},
		{"octet-pair-be-ffff", "HLAoctetPairBE", 2, [2]byte{0xFF, 0xFF}, "ffff"},

		// HLAoctetPairLE
		{"octet-pair-le-0001", "HLAoctetPairLE", 2, [2]byte{0x00, 0x01}, "0100"},
		{"octet-pair-le-abcd", "HLAoctetPairLE", 2, [2]byte{0xAB, 0xCD}, "cdab"},
		{"octet-pair-le-ffff", "HLAoctetPairLE", 2, [2]byte{0xFF, 0xFF}, "ffff"},
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
				t.Fatalf("Encode(%v): %v", c.value, err)
			}
			if !bytes.Equal(encoded, expected) {
				t.Errorf("Encode(%v) = %x, want %x", c.value, encoded, expected)
			}

			decoded, n, err := codec.Decode(expected)
			if err != nil {
				t.Fatalf("Decode(%x): %v", expected, err)
			}
			if n != len(expected) {
				t.Errorf("Decode consumed %d bytes, want %d", n, len(expected))
			}

			// Round-trip: re-encode the decoded value and assert byte-equality.
			reEncoded, err := codec.Encode(decoded)
			if err != nil {
				t.Fatalf("re-Encode(%v): %v", decoded, err)
			}
			if !bytes.Equal(reEncoded, expected) {
				t.Errorf("round-trip Encode(Decode(%x)) = %x, want %x",
					expected, reEncoded, expected)
			}
		})
	}
}

// TestPrimitive_HLAoctet_EncodeAcceptsAlternateGoTypes confirms HLAoctet
// accepts byte/uint8 and int (clamped to 0..255) per the task brief.
func TestPrimitive_HLAoctet_EncodeAcceptsAlternateGoTypes(t *testing.T) {
	t.Parallel()
	codec, err := PrimitiveByName("HLAoctet")
	if err != nil {
		t.Fatalf("PrimitiveByName: %v", err)
	}
	for _, in := range []any{byte(0xAB), uint8(0xAB), int(0xAB)} {
		got, err := codec.Encode(in)
		if err != nil {
			t.Fatalf("Encode(%T %v): %v", in, in, err)
		}
		if !bytes.Equal(got, []byte{0xAB}) {
			t.Errorf("Encode(%T %v) = %x, want ab", in, in, got)
		}
	}
	// Out-of-range int is rejected.
	if _, err := codec.Encode(256); err == nil {
		t.Errorf("Encode(256) returned nil error, want range error")
	}
	if _, err := codec.Encode(-1); err == nil {
		t.Errorf("Encode(-1) returned nil error, want range error")
	}
}

// TestPrimitive_HLAboolean_EncodeRejectsNonBool ensures we don't silently
// accept arbitrary types as booleans.
func TestPrimitive_HLAboolean_EncodeRejectsNonBool(t *testing.T) {
	t.Parallel()
	codec, err := PrimitiveByName("HLAboolean")
	if err != nil {
		t.Fatalf("PrimitiveByName: %v", err)
	}
	if _, err := codec.Encode("true"); err == nil {
		t.Errorf("Encode(\"true\") returned nil error, want type error")
	}
}

// TestPrimitive_HLAASCIIchar_EncodeAcceptsRuneAndByte covers the alternate
// input forms allowed by the task brief.
func TestPrimitive_HLAASCIIchar_EncodeAcceptsRuneAndByte(t *testing.T) {
	t.Parallel()
	codec, err := PrimitiveByName("HLAASCIIchar")
	if err != nil {
		t.Fatalf("PrimitiveByName: %v", err)
	}
	for _, in := range []any{"A", rune('A'), byte('A')} {
		got, err := codec.Encode(in)
		if err != nil {
			t.Fatalf("Encode(%T %v): %v", in, in, err)
		}
		if !bytes.Equal(got, []byte{0x41}) {
			t.Errorf("Encode(%T %v) = %x, want 41", in, in, got)
		}
	}
	// Non-ASCII rune rejected.
	if _, err := codec.Encode(rune(0x100)); err == nil {
		t.Errorf("Encode(0x100) returned nil error, want range error")
	}
	// Multi-character string rejected.
	if _, err := codec.Encode("AB"); err == nil {
		t.Errorf("Encode(\"AB\") returned nil error, want length error")
	}
}

// TestPrimitive_HLAunicodeChar_EncodeAcceptsRuneAndString covers alternate
// inputs and rejects out-of-BMP code points (surrogate-pair handling
// is out of scope for TASK-012).
func TestPrimitive_HLAunicodeChar_EncodeAcceptsRuneAndString(t *testing.T) {
	t.Parallel()
	codec, err := PrimitiveByName("HLAunicodeChar")
	if err != nil {
		t.Fatalf("PrimitiveByName: %v", err)
	}
	for _, in := range []any{"A", rune('A')} {
		got, err := codec.Encode(in)
		if err != nil {
			t.Fatalf("Encode(%T %v): %v", in, in, err)
		}
		if !bytes.Equal(got, []byte{0x00, 0x41}) {
			t.Errorf("Encode(%T %v) = %x, want 0041", in, in, got)
		}
	}
	// Outside BMP: U+1F600 (😀)
	if _, err := codec.Encode(rune(0x1F600)); err == nil {
		t.Errorf("Encode(0x1F600) returned nil error, want out-of-BMP error")
	}
	// Multi-char string rejected.
	if _, err := codec.Encode("AB"); err == nil {
		t.Errorf("Encode(\"AB\") returned nil error, want length error")
	}
}

// TestPrimitive_ByteCodecs_DecodeShortBuffer verifies decoders return errors
// (rather than panic) when given insufficient bytes.
func TestPrimitive_ByteCodecs_DecodeShortBuffer(t *testing.T) {
	t.Parallel()
	for _, typeName := range []string{
		"HLAoctet", "HLAoctetPairBE", "HLAoctetPairLE",
		"HLAboolean", "HLAASCIIchar", "HLAunicodeChar",
	} {
		typeName := typeName
		t.Run(typeName, func(t *testing.T) {
			t.Parallel()
			codec, err := PrimitiveByName(typeName)
			if err != nil {
				t.Fatalf("PrimitiveByName(%q): %v", typeName, err)
			}
			if _, _, err := codec.Decode(nil); err == nil {
				t.Errorf("Decode(nil) returned nil error, want short-buffer error")
			}
		})
	}
}

// TestPrimitive_ByteCodecs_UnknownName ensures PrimitiveByName errors on
// names outside this task's scope.
func TestPrimitive_ByteCodecs_UnknownName(t *testing.T) {
	t.Parallel()
	if _, err := PrimitiveByName("HLAnotAType"); err == nil {
		t.Errorf("PrimitiveByName(\"HLAnotAType\") returned nil error")
	}
}

// TestPrimitive_HLAoctet_NumericTypeWidth covers all integer-width inputs
// HLAoctet is documented to accept and pins their range checks.
func TestPrimitive_HLAoctet_NumericTypeWidth(t *testing.T) {
	t.Parallel()
	codec := HLAoctet{}

	// Each accepted type must encode 0xAB to a single byte.
	for _, v := range []any{int32(0xAB), int64(0xAB), uint(0xAB), float64(0xAB)} {
		got, err := codec.Encode(v)
		if err != nil {
			t.Errorf("Encode(%T %v): %v", v, v, err)
			continue
		}
		if !bytes.Equal(got, []byte{0xAB}) {
			t.Errorf("Encode(%T %v) = %x, want ab", v, v, got)
		}
	}
	// Range errors per width.
	for _, v := range []any{int32(-1), int32(256), int64(-1), int64(256), uint(256), float64(-1), float64(256), float64(0.5)} {
		if _, err := codec.Encode(v); err == nil {
			t.Errorf("Encode(%T %v) returned nil error, want range error", v, v)
		}
	}
	// Unsupported type.
	if _, err := codec.Encode(struct{}{}); err == nil {
		t.Errorf("Encode(struct{}) returned nil error, want type error")
	}
}

// TestPrimitive_HLAoctetPair_AcceptsByteSlice exercises the []byte form
// of octetPairBytes and its length-mismatch error path.
func TestPrimitive_HLAoctetPair_AcceptsByteSlice(t *testing.T) {
	t.Parallel()
	be := HLAoctetPairBE{}
	le := HLAoctetPairLE{}

	got, err := be.Encode([]byte{0xAB, 0xCD})
	if err != nil || !bytes.Equal(got, []byte{0xAB, 0xCD}) {
		t.Errorf("BE Encode([]byte{ab,cd}) = %x, %v", got, err)
	}
	got, err = le.Encode([]byte{0xAB, 0xCD})
	if err != nil || !bytes.Equal(got, []byte{0xCD, 0xAB}) {
		t.Errorf("LE Encode([]byte{ab,cd}) = %x, %v", got, err)
	}

	// Wrong length and unsupported type.
	if _, err := be.Encode([]byte{0xAB}); err == nil {
		t.Errorf("BE Encode([]byte len 1) returned nil error")
	}
	if _, err := le.Encode([]byte{0xAB, 0xCD, 0xEF}); err == nil {
		t.Errorf("LE Encode([]byte len 3) returned nil error")
	}
	if _, err := be.Encode("ab"); err == nil {
		t.Errorf("BE Encode(string) returned nil error")
	}
}

// TestPrimitive_HLAASCIIchar_DecodeRejectsHighBit ensures decoder enforces
// the 0..127 range on the input byte.
func TestPrimitive_HLAASCIIchar_DecodeRejectsHighBit(t *testing.T) {
	t.Parallel()
	codec := HLAASCIIchar{}
	if _, _, err := codec.Decode([]byte{0x80}); err == nil {
		t.Errorf("Decode(0x80) returned nil error, want range error")
	}
	// Empty string Encode rejected.
	if _, err := codec.Encode(""); err == nil {
		t.Errorf("Encode(\"\") returned nil error, want length error")
	}
	// Non-ASCII byte and high rune rejected.
	if _, err := codec.Encode(byte(0x80)); err == nil {
		t.Errorf("Encode(byte 0x80) returned nil error")
	}
	if _, err := codec.Encode(rune(-1)); err == nil {
		t.Errorf("Encode(rune -1) returned nil error")
	}
	// Non-ASCII string rejected.
	if _, err := codec.Encode(string([]byte{0x80})); err == nil {
		t.Errorf("Encode(\"\\x80\") returned nil error")
	}
	// Unsupported type.
	if _, err := codec.Encode(1.5); err == nil {
		t.Errorf("Encode(float64) returned nil error")
	}
}

// TestPrimitive_HLAunicodeChar_DecodeRejectsSurrogate ensures decoder
// rejects unpaired UTF-16 surrogate code units.
func TestPrimitive_HLAunicodeChar_DecodeRejectsSurrogate(t *testing.T) {
	t.Parallel()
	codec := HLAunicodeChar{}
	// 0xD800 is the start of the high-surrogate range.
	if _, _, err := codec.Decode([]byte{0xD8, 0x00}); err == nil {
		t.Errorf("Decode(0xD800) returned nil error, want surrogate error")
	}
	// Encode side: surrogate rune rejected.
	if _, err := codec.Encode(rune(0xD800)); err == nil {
		t.Errorf("Encode(rune 0xD800) returned nil error")
	}
	// Empty string rejected.
	if _, err := codec.Encode(""); err == nil {
		t.Errorf("Encode(\"\") returned nil error")
	}
	// Negative rune rejected.
	if _, err := codec.Encode(rune(-1)); err == nil {
		t.Errorf("Encode(rune -1) returned nil error")
	}
	// Unsupported type.
	if _, err := codec.Encode(1.5); err == nil {
		t.Errorf("Encode(float64) returned nil error")
	}
}
