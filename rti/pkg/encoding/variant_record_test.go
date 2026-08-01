package encoding

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

// TestSpec_VariantRecord_DiscOctetAlt asserts encoding with discriminator
// value 1 selects the HLAoctet alternative and produces the discriminator
// bytes followed by the alternative bytes. Boundary 4 (disc) → boundary 1
// (octet) requires no inter-field padding because offset 4 is already
// aligned to boundary 1.
//
// Implements: FR-ENC-1, FR-ENC-2 (variant record).
func TestSpec_VariantRecord_DiscOctetAlt(t *testing.T) {
	t.Parallel()

	codec := NewVariantRecord(HLAinteger32BE{}, map[any]Codec{
		int32(1): HLAoctet{},
		int32(2): hlaFloat64BE{},
	})

	if got, want := codec.OctetBoundary(), 8; got != want {
		t.Fatalf("OctetBoundary() = %d, want %d (max disc=4, alts {1,8})", got, want)
	}

	got, err := codec.Encode(map[string]any{
		"discriminator": int32(1),
		"value":         byte(0xAB),
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want, _ := hex.DecodeString("00000001ab")
	if !bytes.Equal(got, want) {
		t.Fatalf("Encode = %x, want %x", got, want)
	}

	dec, n, err := codec.Decode(want)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if n != len(want) {
		t.Errorf("Decode n = %d, want %d", n, len(want))
	}
	m := dec.(map[string]any)
	if m["discriminator"].(int32) != 1 {
		t.Errorf("decoded discriminator = %v, want 1", m["discriminator"])
	}
	if m["value"].(byte) != 0xAB {
		t.Errorf("decoded value = %v, want 0xAB", m["value"])
	}
}

// TestSpec_VariantRecord_DiscFloat64Alt asserts discriminator value 2 selects
// the HLAfloat64BE alternative and emits 4 padding bytes after the int32
// discriminator so the float64 begins at offset 8.
func TestSpec_VariantRecord_DiscFloat64Alt(t *testing.T) {
	t.Parallel()

	codec := NewVariantRecord(HLAinteger32BE{}, map[any]Codec{
		int32(1): HLAoctet{},
		int32(2): hlaFloat64BE{},
	})

	got, err := codec.Encode(map[string]any{
		"discriminator": int32(2),
		"value":         float64(1.0),
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want, _ := hex.DecodeString("00000002000000003ff0000000000000")
	if !bytes.Equal(got, want) {
		t.Fatalf("Encode = %x, want %x (4 padding bytes expected)", got, want)
	}

	dec, n, err := codec.Decode(want)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if n != len(want) {
		t.Errorf("Decode n = %d, want %d", n, len(want))
	}
	m := dec.(map[string]any)
	if m["discriminator"].(int32) != 2 {
		t.Errorf("decoded discriminator = %v, want 2", m["discriminator"])
	}
	if m["value"].(float64) != 1.0 {
		t.Errorf("decoded value = %v, want 1.0", m["value"])
	}
}

// TestSpec_VariantRecord_Int32Alt covers the variant-record-int32-disc-2-int32-alt
// vector: discriminator HLAinteger32BE with int32 alt requires no padding
// because offset 4 is already aligned to boundary 4.
func TestSpec_VariantRecord_Int32Alt(t *testing.T) {
	t.Parallel()

	codec := NewVariantRecord(HLAinteger32BE{}, map[any]Codec{
		int32(1): HLAoctet{},
		int32(2): HLAinteger32BE{},
	})

	if got, want := codec.OctetBoundary(), 4; got != want {
		t.Fatalf("OctetBoundary() = %d, want %d (max disc=4, alts {1,4})", got, want)
	}

	got, err := codec.Encode(map[string]any{
		"discriminator": int32(2),
		"value":         int32(42),
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want, _ := hex.DecodeString("000000020000002a")
	if !bytes.Equal(got, want) {
		t.Fatalf("Encode = %x, want %x (no padding expected)", got, want)
	}

	dec, n, err := codec.Decode(want)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if n != len(want) {
		t.Errorf("Decode n = %d, want %d", n, len(want))
	}
	m := dec.(map[string]any)
	if m["discriminator"].(int32) != 2 {
		t.Errorf("decoded discriminator = %v, want 2", m["discriminator"])
	}
	if m["value"].(int32) != 42 {
		t.Errorf("decoded value = %v, want 42", m["value"])
	}
}

// TestVariantRecord_Decode_UnknownDiscriminator returns ErrEncTypeMismatch
// (unknown discriminator value cannot be matched to an alternative).
func TestVariantRecord_Decode_UnknownDiscriminator(t *testing.T) {
	t.Parallel()

	codec := NewVariantRecord(HLAinteger32BE{}, map[any]Codec{
		int32(1): HLAoctet{},
		int32(2): hlaFloat64BE{},
	})

	// Discriminator value 99 (0x00000063), no alternative defined for it.
	buf := []byte{0x00, 0x00, 0x00, 0x63, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	_, _, err := codec.Decode(buf)
	if err == nil {
		t.Fatalf("Decode unknown discriminator: want error, got nil")
	}
	if !errors.Is(err, ErrEncTypeMismatch) {
		t.Errorf("Decode unknown discriminator: want errors.Is ErrEncTypeMismatch, got %v", err)
	}
}

// TestVariantRecord_Encode_UnknownDiscriminator rejects an Encode whose
// discriminator value is not a registered alternative.
func TestVariantRecord_Encode_UnknownDiscriminator(t *testing.T) {
	t.Parallel()

	codec := NewVariantRecord(HLAinteger32BE{}, map[any]Codec{
		int32(1): HLAoctet{},
	})

	_, err := codec.Encode(map[string]any{
		"discriminator": int32(99),
		"value":         byte(0xFF),
	})
	if err == nil {
		t.Fatalf("Encode unknown discriminator: want error, got nil")
	}
	if !errors.Is(err, ErrEncTypeMismatch) {
		t.Errorf("want errors.Is ErrEncTypeMismatch, got %v", err)
	}
}

// TestVariantRecord_Encode_MissingKeys rejects missing 'discriminator' or
// 'value' keys.
func TestVariantRecord_Encode_MissingKeys(t *testing.T) {
	t.Parallel()

	codec := NewVariantRecord(HLAinteger32BE{}, map[any]Codec{
		int32(1): HLAoctet{},
	})

	if _, err := codec.Encode(map[string]any{"value": byte(1)}); err == nil {
		t.Errorf("missing discriminator: want error, got nil")
	}
	if _, err := codec.Encode(map[string]any{"discriminator": int32(1)}); err == nil {
		t.Errorf("missing value: want error, got nil")
	}
}

// TestVariantRecord_Encode_WrongType rejects a non-map value.
func TestVariantRecord_Encode_WrongType(t *testing.T) {
	t.Parallel()

	codec := NewVariantRecord(HLAinteger32BE{}, map[any]Codec{
		int32(1): HLAoctet{},
	})

	if _, err := codec.Encode("not-a-map"); err == nil {
		t.Errorf("non-map: want error, got nil")
	} else if !errors.Is(err, ErrEncTypeMismatch) {
		t.Errorf("non-map: want ErrEncTypeMismatch, got %v", err)
	}
}

// TestVariantRecord_Decode_ShortBuffer fails when the buffer cannot hold
// the discriminator or the alternative.
func TestVariantRecord_Decode_ShortBuffer(t *testing.T) {
	t.Parallel()

	codec := NewVariantRecord(HLAinteger32BE{}, map[any]Codec{
		int32(2): hlaFloat64BE{},
	})

	if _, _, err := codec.Decode([]byte{0x00, 0x00}); err == nil {
		t.Errorf("short for discriminator: want error, got nil")
	}
	// Discriminator 2 + only 4 bytes — not enough for float64 alternative
	// (needs 8 bytes after a 4-byte pad).
	if _, _, err := codec.Decode([]byte{0x00, 0x00, 0x00, 0x02, 0, 0, 0, 0}); err == nil {
		t.Errorf("short for alternative payload: want error, got nil")
	}
}

// TestSpec_VariantRecord_JSONFloat64Discriminator covers the JSON-loading
// path: vectors loaded from encoding_vectors.json arrive with float64
// discriminator values (because encoding/json deserializes JSON numbers
// as float64), but the alternatives map is keyed on the discriminator
// codec's canonical Go type (int32 for HLAinteger32BE). The codec must
// canonicalize the input discriminator before lookup, otherwise every
// JSON-driven variant-record vector regresses.
//
// Implements: FR-ENC-1, FR-ENC-2 (variant record); fixes the gap surfaced
// by TestSpec_M1_CompositeVectorsRoundTrip/variant-record-* on JSON-shaped
// composite vectors.
func TestSpec_VariantRecord_JSONFloat64Discriminator(t *testing.T) {
	t.Parallel()

	// Same codec shape as TestSpec_VariantRecord_DiscOctetAlt (int32
	// discriminator, HLAoctet/float64 alts), but Encode receives the
	// discriminator as float64(1.0) — the JSON form.
	codec := NewVariantRecord(HLAinteger32BE{}, map[any]Codec{
		int32(1): HLAoctet{},
		int32(2): hlaFloat64BE{},
	})

	got, err := codec.Encode(map[string]any{
		"discriminator": float64(1.0),
		"value":         float64(0xAB), // JSON also delivers value as float64
	})
	if err != nil {
		t.Fatalf("Encode with float64 disc: %v", err)
	}
	// Same expected bytes as the int32-disc case: 4 bytes of disc + 1 byte alt.
	want, _ := hex.DecodeString("00000001ab")
	if !bytes.Equal(got, want) {
		t.Fatalf("Encode = %x, want %x", got, want)
	}

	// And float64(2.0) -> selects the float64 alt (with 4-byte pad).
	got, err = codec.Encode(map[string]any{
		"discriminator": float64(2.0),
		"value":         float64(1.0),
	})
	if err != nil {
		t.Fatalf("Encode with float64 disc=2: %v", err)
	}
	want, _ = hex.DecodeString("00000002000000003ff0000000000000")
	if !bytes.Equal(got, want) {
		t.Fatalf("Encode = %x, want %x", got, want)
	}
}
