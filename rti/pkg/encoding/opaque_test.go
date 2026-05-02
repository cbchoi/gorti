package encoding

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// TestComposite_HLAopaqueData_RoundTrip pins the wire format for opaque
// blobs: 4-byte BE length + N raw bytes, no element typing, no internal
// padding beyond the 4-byte length-prefix alignment.
func TestComposite_HLAopaqueData_RoundTrip(t *testing.T) {
	t.Parallel()

	type tc struct {
		name     string
		value    []byte
		hexBytes string
	}

	cases := []tc{
		{"empty", []byte{}, "00000000"},
		{"single", []byte{0xAB}, "00000001ab"},
		{"three", []byte{0x01, 0x02, 0x03}, "00000003010203"},
		{"deadbeef", []byte{0xDE, 0xAD, 0xBE, 0xEF}, "00000004deadbeef"},
	}

	codec := NewOpaqueData()
	if got := codec.OctetBoundary(); got != 4 {
		t.Errorf("OctetBoundary() = %d, want 4", got)
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

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
				t.Errorf("Decode consumed %d, want %d", n, len(expected))
			}
			got, ok := decoded.([]byte)
			if !ok {
				t.Fatalf("Decode returned %T, want []byte", decoded)
			}
			if !bytes.Equal(got, c.value) {
				t.Errorf("Decode = %x, want %x", got, c.value)
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

// TestComposite_HLAopaqueData_DecodeShortBuffer covers length-prefix
// underflow and payload-truncation paths.
func TestComposite_HLAopaqueData_DecodeShortBuffer(t *testing.T) {
	t.Parallel()
	codec := NewOpaqueData()

	if _, _, err := codec.Decode(nil); err == nil {
		t.Errorf("Decode(nil) returned nil error")
	}
	if _, _, err := codec.Decode([]byte{0, 0, 0}); err == nil {
		t.Errorf("Decode(short prefix) returned nil error")
	}
	// Length prefix says 5, payload only 2.
	if _, _, err := codec.Decode([]byte{0, 0, 0, 5, 0xDE, 0xAD}); err == nil {
		t.Errorf("Decode(short payload) returned nil error")
	}
}

// TestComposite_HLAopaqueData_EncodeRejectsNonByteSlice pins that opaque
// only accepts []byte (not []any of bytes, not strings).
func TestComposite_HLAopaqueData_EncodeRejectsNonByteSlice(t *testing.T) {
	t.Parallel()
	codec := NewOpaqueData()
	if _, err := codec.Encode("hello"); err == nil {
		t.Errorf("Encode(string) returned nil error, want type error")
	}
	if _, err := codec.Encode([]any{byte(1)}); err == nil {
		t.Errorf("Encode([]any) returned nil error, want type error")
	}
	if _, err := codec.Encode(123); err == nil {
		t.Errorf("Encode(int) returned nil error, want type error")
	}
}

// TestComposite_HLAopaqueData_EncodeAcceptsNilSlice pins that nil []byte is
// equivalent to an empty blob.
func TestComposite_HLAopaqueData_EncodeAcceptsNilSlice(t *testing.T) {
	t.Parallel()
	codec := NewOpaqueData()
	got, err := codec.Encode([]byte(nil))
	if err != nil {
		t.Fatalf("Encode(nil []byte): %v", err)
	}
	if !bytes.Equal(got, []byte{0, 0, 0, 0}) {
		t.Errorf("Encode(nil []byte) = %x, want 00000000", got)
	}
}
