package encoding

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// TestSpec_FloatPrimitiveCodecs_RoundTrip is a table-driven round-trip and
// byte-equality test for the four IEEE 754 float primitive codecs:
// HLAfloat32BE, HLAfloat32LE, HLAfloat64BE, HLAfloat64LE.
//
// Values are restricted to exactly-representable IEEE 754 numbers per the
// anti-goal in docs/agent-b-fom-encoding.md §7 — NaN/Inf cross-language
// equality is a separate post-M1 concern.
//
// Implements: FR-ENC-1, FR-ENC-2.
func TestSpec_FloatPrimitiveCodecs_RoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		typeName  string
		value     any
		hexBytes  string
		boundary  int
		valueSize int
	}{
		// HLAfloat32BE
		{"f32be-zero", "HLAfloat32BE", float32(0.0), "00000000", 4, 4},
		{"f32be-one", "HLAfloat32BE", float32(1.0), "3f800000", 4, 4},
		{"f32be-neg-one", "HLAfloat32BE", float32(-1.0), "bf800000", 4, 4},
		{"f32be-half", "HLAfloat32BE", float32(0.5), "3f000000", 4, 4},
		{"f32be-quarter", "HLAfloat32BE", float32(0.25), "3e800000", 4, 4},
		{"f32be-two", "HLAfloat32BE", float32(2.0), "40000000", 4, 4},

		// HLAfloat32LE
		{"f32le-zero", "HLAfloat32LE", float32(0.0), "00000000", 4, 4},
		{"f32le-one", "HLAfloat32LE", float32(1.0), "0000803f", 4, 4},
		{"f32le-neg-one", "HLAfloat32LE", float32(-1.0), "000080bf", 4, 4},
		{"f32le-half", "HLAfloat32LE", float32(0.5), "0000003f", 4, 4},

		// HLAfloat64BE
		{"f64be-zero", "HLAfloat64BE", float64(0.0), "0000000000000000", 8, 8},
		{"f64be-one", "HLAfloat64BE", float64(1.0), "3ff0000000000000", 8, 8},
		{"f64be-neg-two", "HLAfloat64BE", float64(-2.0), "c000000000000000", 8, 8},
		{"f64be-half", "HLAfloat64BE", float64(0.5), "3fe0000000000000", 8, 8},
		{"f64be-neg-half", "HLAfloat64BE", float64(-0.5), "bfe0000000000000", 8, 8},
		{"f64be-quarter", "HLAfloat64BE", float64(0.25), "3fd0000000000000", 8, 8},

		// HLAfloat64LE
		{"f64le-zero", "HLAfloat64LE", float64(0.0), "0000000000000000", 8, 8},
		{"f64le-one", "HLAfloat64LE", float64(1.0), "000000000000f03f", 8, 8},
		{"f64le-half", "HLAfloat64LE", float64(0.5), "000000000000e03f", 8, 8},
		{"f64le-neg-two", "HLAfloat64LE", float64(-2.0), "00000000000000c0", 8, 8},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			codec, err := PrimitiveByName(tc.typeName)
			if err != nil {
				t.Fatalf("PrimitiveByName(%q): %v", tc.typeName, err)
			}
			if got := codec.OctetBoundary(); got != tc.boundary {
				t.Errorf("OctetBoundary() = %d, want %d", got, tc.boundary)
			}

			expected, err := hex.DecodeString(tc.hexBytes)
			if err != nil {
				t.Fatalf("hex %q: %v", tc.hexBytes, err)
			}

			got, err := codec.Encode(tc.value)
			if err != nil {
				t.Fatalf("Encode(%v): %v", tc.value, err)
			}
			if !bytes.Equal(got, expected) {
				t.Fatalf("encode mismatch: got %x, want %x", got, expected)
			}

			decoded, n, err := codec.Decode(expected)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if n != tc.valueSize {
				t.Errorf("Decode consumed %d bytes, want %d", n, tc.valueSize)
			}
			// Decoders always return float64 (widened from float32 for the
			// 32-bit codecs) for cross-language equality with the Python
			// encoder, which has only one float type.
			g, ok := decoded.(float64)
			if !ok {
				t.Fatalf("Decode returned %T, want float64", decoded)
			}
			switch want := tc.value.(type) {
			case float32:
				if g != float64(want) {
					t.Errorf("Decode round-trip: got %v, want %v", g, float64(want))
				}
			case float64:
				if g != want {
					t.Errorf("Decode round-trip: got %v, want %v", g, want)
				}
			}
		})
	}
}

// TestSpec_FloatPrimitiveCodecs_DecodeShortBuffer asserts that decoding a
// truncated buffer reports an error rather than panicking.
func TestSpec_FloatPrimitiveCodecs_DecodeShortBuffer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		typeName string
		short    []byte
	}{
		{"HLAfloat32BE", []byte{0x00, 0x01, 0x02}},
		{"HLAfloat32LE", []byte{0x00}},
		{"HLAfloat64BE", []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}},
		{"HLAfloat64LE", []byte{}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.typeName, func(t *testing.T) {
			t.Parallel()
			codec, err := PrimitiveByName(tc.typeName)
			if err != nil {
				t.Fatalf("PrimitiveByName(%q): %v", tc.typeName, err)
			}
			if _, _, err := codec.Decode(tc.short); err == nil {
				t.Fatalf("Decode(%x): want error, got nil", tc.short)
			}
		})
	}
}

// TestSpec_FloatPrimitiveCodecs_EncodeWrongType asserts that Encode rejects
// values of the wrong Go type with a non-nil error rather than panicking or
// silently misencoding.
func TestSpec_FloatPrimitiveCodecs_EncodeWrongType(t *testing.T) {
	t.Parallel()

	// Note: float32 and float64 are mutually accepted (with conversion) by
	// both float32 and float64 codecs to support JSON-loaded vector values
	// and the Python decoder. This test asserts that *non-float* types are
	// rejected.
	cases := []struct {
		typeName string
		bad      any
	}{
		{"HLAfloat32BE", "not-a-float"},
		{"HLAfloat32LE", []byte{0x00}},
		{"HLAfloat64BE", true},
		{"HLAfloat64LE", 42},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.typeName, func(t *testing.T) {
			t.Parallel()
			codec, err := PrimitiveByName(tc.typeName)
			if err != nil {
				t.Fatalf("PrimitiveByName(%q): %v", tc.typeName, err)
			}
			if _, err := codec.Encode(tc.bad); err == nil {
				t.Fatalf("Encode(%T): want error, got nil", tc.bad)
			}
		})
	}
}
