package encoding

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// TestComposite_HLAfixedArray_RoundTrip exercises the fixed-array codec
// against representative element types, asserting both byte-equality with
// the spec layout and decode→encode round-trip.
//
// HLAfixedArray of N elements is encoded as N copies of the element codec's
// encoding, with inter-element padding equal to the element's OctetBoundary
// (no leading prefix). The composite's OctetBoundary equals the element
// boundary.
func TestComposite_HLAfixedArray_RoundTrip(t *testing.T) {
	t.Parallel()

	type tc struct {
		name     string
		elem     Codec
		boundary int
		card     int
		value    []any
		hexBytes string
	}

	cases := []tc{
		{
			name:     "int32be-3",
			elem:     HLAinteger32BE{},
			boundary: 4,
			card:     3,
			value:    []any{int32(1), int32(2), int32(3)},
			hexBytes: "000000010000000200000003",
		},
		{
			name:     "float64be-2",
			elem:     hlaFloat64BE{},
			boundary: 8,
			card:     2,
			value:    []any{1.0, 0.5},
			hexBytes: "3ff00000000000003fe0000000000000",
		},
		{
			name:     "octet-5",
			elem:     HLAoctet{},
			boundary: 1,
			card:     5,
			value:    []any{byte(0x01), byte(0x02), byte(0x03), byte(0x04), byte(0x05)},
			hexBytes: "0102030405",
		},
		{
			name:     "octet-empty",
			elem:     HLAoctet{},
			boundary: 1,
			card:     0,
			value:    []any{},
			hexBytes: "",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			codec := NewFixedArray(c.elem, c.card)
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

			// Round-trip: encoded(decoded) == expected.
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

// TestComposite_HLAfixedArray_EncodeRejectsWrongCardinality enforces the
// cardinality contract: callers must supply exactly N elements.
func TestComposite_HLAfixedArray_EncodeRejectsWrongCardinality(t *testing.T) {
	t.Parallel()
	codec := NewFixedArray(HLAinteger32BE{}, 3)

	if _, err := codec.Encode([]any{int32(1), int32(2)}); err == nil {
		t.Errorf("Encode([2]) returned nil error for cardinality 3")
	}
	if _, err := codec.Encode([]any{int32(1), int32(2), int32(3), int32(4)}); err == nil {
		t.Errorf("Encode([4]) returned nil error for cardinality 3")
	}
	if _, err := codec.Encode("not a slice"); err == nil {
		t.Errorf("Encode(string) returned nil error, want type error")
	}
}

// TestComposite_HLAfixedArray_DecodeShortBuffer verifies underflow detection
// when the wire is truncated mid-element.
func TestComposite_HLAfixedArray_DecodeShortBuffer(t *testing.T) {
	t.Parallel()
	codec := NewFixedArray(HLAinteger32BE{}, 3)
	// 3 * 4 == 12 bytes required; supply 7.
	if _, _, err := codec.Decode([]byte{0, 0, 0, 1, 0, 0, 0}); err == nil {
		t.Errorf("Decode(short) returned nil error")
	}
}

// TestComposite_HLAfixedArray_NilElementCodec pins the constructor-time
// requirement that elem != nil.
func TestComposite_HLAfixedArray_NilElementCodec(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewFixedArray(nil, ...) did not panic on invariant violation")
		}
	}()
	_ = NewFixedArray(nil, 3)
}

// TestComposite_HLAfixedArray_NegativeCardinality pins that cardinality
// must be non-negative.
func TestComposite_HLAfixedArray_NegativeCardinality(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewFixedArray(_, -1) did not panic")
		}
	}()
	_ = NewFixedArray(HLAinteger32BE{}, -1)
}
