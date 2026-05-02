package encoding

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math"
	"testing"
)

// TestSpec_IntegerCodec_RoundTrip is a table-driven specification of every
// HLAinteger BE/LE primitive: encode the value to expected bytes (byte-diff)
// and decode the bytes back to the original value.
//
// Implements: FR-ENC-1, FR-ENC-2 (integer primitives only).
func TestSpec_IntegerCodec_RoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		typ     string
		boundary int
		// encodeIn is what we hand to Encode (native Go integer width).
		encodeIn any
		// decodeOut is what Decode is expected to return for these bytes.
		decodeOut any
		hex       string
	}{
		// HLAinteger16BE — 2-byte big-endian, two's-complement.
		{"int16be-zero", "HLAinteger16BE", 2, int16(0), int32(0), "0000"},
		{"int16be-one", "HLAinteger16BE", 2, int16(1), int32(1), "0001"},
		{"int16be-neg-one", "HLAinteger16BE", 2, int16(-1), int32(-1), "ffff"},
		{"int16be-max", "HLAinteger16BE", 2, int16(math.MaxInt16), int32(math.MaxInt16), "7fff"},
		{"int16be-min", "HLAinteger16BE", 2, int16(math.MinInt16), int32(math.MinInt16), "8000"},

		// HLAinteger16LE — 2-byte little-endian.
		{"int16le-zero", "HLAinteger16LE", 2, int16(0), int32(0), "0000"},
		{"int16le-one", "HLAinteger16LE", 2, int16(1), int32(1), "0100"},
		{"int16le-neg-one", "HLAinteger16LE", 2, int16(-1), int32(-1), "ffff"},
		{"int16le-max", "HLAinteger16LE", 2, int16(math.MaxInt16), int32(math.MaxInt16), "ff7f"},
		{"int16le-min", "HLAinteger16LE", 2, int16(math.MinInt16), int32(math.MinInt16), "0080"},

		// HLAinteger32BE — 4-byte big-endian.
		{"int32be-zero", "HLAinteger32BE", 4, int32(0), int32(0), "00000000"},
		{"int32be-one", "HLAinteger32BE", 4, int32(1), int32(1), "00000001"},
		{"int32be-neg-one", "HLAinteger32BE", 4, int32(-1), int32(-1), "ffffffff"},
		{"int32be-max", "HLAinteger32BE", 4, int32(math.MaxInt32), int32(math.MaxInt32), "7fffffff"},
		{"int32be-min", "HLAinteger32BE", 4, int32(math.MinInt32), int32(math.MinInt32), "80000000"},

		// HLAinteger32LE — 4-byte little-endian.
		{"int32le-zero", "HLAinteger32LE", 4, int32(0), int32(0), "00000000"},
		{"int32le-one", "HLAinteger32LE", 4, int32(1), int32(1), "01000000"},
		{"int32le-neg-one", "HLAinteger32LE", 4, int32(-1), int32(-1), "ffffffff"},
		{"int32le-max", "HLAinteger32LE", 4, int32(math.MaxInt32), int32(math.MaxInt32), "ffffff7f"},
		{"int32le-min", "HLAinteger32LE", 4, int32(math.MinInt32), int32(math.MinInt32), "00000080"},

		// HLAinteger64BE — 8-byte big-endian.
		{"int64be-zero", "HLAinteger64BE", 8, int64(0), int64(0), "0000000000000000"},
		{"int64be-one", "HLAinteger64BE", 8, int64(1), int64(1), "0000000000000001"},
		{"int64be-neg-one", "HLAinteger64BE", 8, int64(-1), int64(-1), "ffffffffffffffff"},
		{"int64be-max", "HLAinteger64BE", 8, int64(math.MaxInt64), int64(math.MaxInt64), "7fffffffffffffff"},
		{"int64be-min", "HLAinteger64BE", 8, int64(math.MinInt64), int64(math.MinInt64), "8000000000000000"},

		// HLAinteger64LE — 8-byte little-endian.
		{"int64le-zero", "HLAinteger64LE", 8, int64(0), int64(0), "0000000000000000"},
		{"int64le-one", "HLAinteger64LE", 8, int64(1), int64(1), "0100000000000000"},
		{"int64le-neg-one", "HLAinteger64LE", 8, int64(-1), int64(-1), "ffffffffffffffff"},
		{"int64le-max", "HLAinteger64LE", 8, int64(math.MaxInt64), int64(math.MaxInt64), "ffffffffffffff7f"},
		{"int64le-min", "HLAinteger64LE", 8, int64(math.MinInt64), int64(math.MinInt64), "0000000000000080"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			codec, err := PrimitiveByName(tc.typ)
			if err != nil {
				t.Fatalf("PrimitiveByName(%q): %v", tc.typ, err)
			}
			if got := codec.OctetBoundary(); got != tc.boundary {
				t.Errorf("OctetBoundary() = %d, want %d", got, tc.boundary)
			}

			want, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatalf("hex %q: %v", tc.hex, err)
			}

			got, err := codec.Encode(tc.encodeIn)
			if err != nil {
				t.Fatalf("Encode(%v): %v", tc.encodeIn, err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("Encode bytes mismatch: got %x, want %x", got, want)
			}

			decoded, n, err := codec.Decode(want)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if n != len(want) {
				t.Errorf("Decode consumed %d bytes, want %d", n, len(want))
			}
			if decoded != tc.decodeOut {
				t.Errorf("Decode value mismatch: got %v (%T), want %v (%T)",
					decoded, decoded, tc.decodeOut, tc.decodeOut)
			}
		})
	}
}

// TestSpec_IntegerCodec_AcceptsFloat64FromJSON ensures Encode tolerates the
// float64 inputs produced by encoding/json for numeric vector values, which
// is how tests/spec/M1/encoding_vectors_test.go feeds the codec.
func TestSpec_IntegerCodec_AcceptsFloat64FromJSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		typ string
		v   float64
		hex string
	}{
		{"HLAinteger16BE", 1, "0001"},
		{"HLAinteger16LE", 1, "0100"},
		{"HLAinteger32BE", 1, "00000001"},
		{"HLAinteger32LE", 1, "01000000"},
		{"HLAinteger64BE", 1, "0000000000000001"},
		{"HLAinteger64LE", 1, "0100000000000000"},
		{"HLAinteger16BE", -1, "ffff"},
		{"HLAinteger32BE", -2147483648, "80000000"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.typ, func(t *testing.T) {
			t.Parallel()

			codec, err := PrimitiveByName(tc.typ)
			if err != nil {
				t.Fatalf("PrimitiveByName(%q): %v", tc.typ, err)
			}
			want, _ := hex.DecodeString(tc.hex)

			got, err := codec.Encode(tc.v)
			if err != nil {
				t.Fatalf("Encode(%v as float64): %v", tc.v, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("Encode(%v) = %x, want %x", tc.v, got, want)
			}
		})
	}
}

// TestSpec_IntegerCodec_TypeMismatch verifies Encode rejects values that
// cannot be represented as the codec's native integer width with a clear
// sentinel error (ErrEncTypeMismatch).
func TestSpec_IntegerCodec_TypeMismatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		typ string
		v   any
	}{
		{"HLAinteger16BE", "not-a-number"},
		{"HLAinteger32BE", []byte{1, 2, 3}},
		{"HLAinteger64BE", struct{}{}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.typ, func(t *testing.T) {
			t.Parallel()
			codec, err := PrimitiveByName(tc.typ)
			if err != nil {
				t.Fatalf("PrimitiveByName(%q): %v", tc.typ, err)
			}
			_, err = codec.Encode(tc.v)
			if !errors.Is(err, ErrEncTypeMismatch) {
				t.Errorf("Encode(%v) error = %v, want ErrEncTypeMismatch", tc.v, err)
			}
		})
	}
}

// TestSpec_IntegerCodec_RangeOverflow verifies Encode rejects values that
// fit a wider Go integer type but overflow the codec's storage width.
func TestSpec_IntegerCodec_RangeOverflow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		typ string
		v   any
	}{
		{"HLAinteger16BE", int32(math.MaxInt16) + 1},
		{"HLAinteger16BE", int32(math.MinInt16) - 1},
		{"HLAinteger32BE", int64(math.MaxInt32) + 1},
		{"HLAinteger32BE", int64(math.MinInt32) - 1},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.typ, func(t *testing.T) {
			t.Parallel()
			codec, _ := PrimitiveByName(tc.typ)
			if _, err := codec.Encode(tc.v); err == nil {
				t.Errorf("Encode(%v) on %s: expected error, got nil", tc.v, tc.typ)
			}
		})
	}
}

// TestSpec_IntegerCodec_DecodeShortBuffer verifies Decode rejects buffers
// that are shorter than the codec's storage width.
func TestSpec_IntegerCodec_DecodeShortBuffer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		typ string
		buf []byte
	}{
		{"HLAinteger16BE", []byte{0x01}},
		{"HLAinteger32LE", []byte{0x01, 0x02}},
		{"HLAinteger64BE", []byte{0x01, 0x02, 0x03, 0x04}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.typ, func(t *testing.T) {
			t.Parallel()
			codec, _ := PrimitiveByName(tc.typ)
			if _, _, err := codec.Decode(tc.buf); err == nil {
				t.Errorf("Decode(%x) on %s: expected error, got nil", tc.buf, tc.typ)
			}
		})
	}
}

// TestSpec_PrimitiveByName_Unknown ensures the lookup returns an error for
// names that are not (yet) supported, and does NOT silently fall back to a
// generic codec.
func TestSpec_PrimitiveByName_Unknown(t *testing.T) {
	t.Parallel()

	if _, err := PrimitiveByName("HLAimaginary42BE"); err == nil {
		t.Errorf("PrimitiveByName(unknown): expected error, got nil")
	}
}
