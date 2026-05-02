package encoding

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

// TestSpec_FixedRecord_OctetThenFloat64_PaddingBytes asserts the canonical
// padding pattern from IEEE 1516.2-2010 §4 used by the
// fixed-record-octet-float64 conformance vector: a single octet (boundary 1)
// followed by an HLAfloat64BE (boundary 8) requires 7 padding bytes between
// the two fields so the float64 begins at offset 8 from the start of the
// record.
//
// Implements: FR-ENC-1, FR-ENC-2 (composite).
func TestSpec_FixedRecord_OctetThenFloat64_PaddingBytes(t *testing.T) {
	t.Parallel()

	codec := NewFixedRecord([]FixedRecordField{
		{Name: "a", Codec: HLAoctet{}},
		{Name: "b", Codec: hlaFloat64BE{}},
	})

	if got, want := codec.OctetBoundary(), 8; got != want {
		t.Fatalf("OctetBoundary() = %d, want %d (max of fields)", got, want)
	}

	value := map[string]any{"a": byte(0x05), "b": float64(1.0)}
	got, err := codec.Encode(value)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	want, err := hex.DecodeString("05000000000000003ff0000000000000")
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Encode bytes = %x, want %x", got, want)
	}

	dec, n, err := codec.Decode(want)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if n != len(want) {
		t.Fatalf("Decode consumed %d bytes, want %d", n, len(want))
	}
	m, ok := dec.(map[string]any)
	if !ok {
		t.Fatalf("Decode value type = %T, want map[string]any", dec)
	}
	if a, ok := m["a"].(byte); !ok || a != 0x05 {
		t.Errorf("decoded a = %v (%T), want byte(0x05)", m["a"], m["a"])
	}
	if b, ok := m["b"].(float64); !ok || b != 1.0 {
		t.Errorf("decoded b = %v (%T), want float64(1.0)", m["b"], m["b"])
	}
}

// TestSpec_FixedRecord_TwoInt32_NoPadding asserts that two consecutive
// HLAinteger32BE fields (boundary 4 each) pack tightly with no inter-field
// padding because the second field's start offset (4) is already aligned to
// its boundary (4).
func TestSpec_FixedRecord_TwoInt32_NoPadding(t *testing.T) {
	t.Parallel()

	codec := NewFixedRecord([]FixedRecordField{
		{Name: "x", Codec: HLAinteger32BE{}},
		{Name: "y", Codec: HLAinteger32BE{}},
	})

	if got, want := codec.OctetBoundary(), 4; got != want {
		t.Fatalf("OctetBoundary() = %d, want %d", got, want)
	}

	value := map[string]any{"x": int32(1), "y": int32(-1)}
	got, err := codec.Encode(value)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want, _ := hex.DecodeString("00000001ffffffff")
	if !bytes.Equal(got, want) {
		t.Fatalf("Encode bytes = %x, want %x (no padding expected)", got, want)
	}

	dec, n, err := codec.Decode(want)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if n != 8 {
		t.Errorf("Decode n = %d, want 8", n)
	}
	m := dec.(map[string]any)
	if m["x"].(int32) != 1 || m["y"].(int32) != -1 {
		t.Errorf("decoded fields = %v, want {x:1, y:-1}", m)
	}
}

// TestSpec_FixedRecord_Int32ThenFloat64_FourBytePadding asserts an
// HLAinteger32BE (boundary 4) followed by HLAfloat64BE (boundary 8) emits
// exactly 4 padding bytes after the int32 so the float64 starts at offset 8.
func TestSpec_FixedRecord_Int32ThenFloat64_FourBytePadding(t *testing.T) {
	t.Parallel()

	codec := NewFixedRecord([]FixedRecordField{
		{Name: "i", Codec: HLAinteger32BE{}},
		{Name: "d", Codec: hlaFloat64BE{}},
	})

	if got, want := codec.OctetBoundary(), 8; got != want {
		t.Fatalf("OctetBoundary() = %d, want %d", got, want)
	}

	value := map[string]any{"i": int32(1), "d": float64(0.5)}
	got, err := codec.Encode(value)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want, _ := hex.DecodeString("00000001000000003fe0000000000000")
	if !bytes.Equal(got, want) {
		t.Fatalf("Encode bytes = %x, want %x (expect 4 padding bytes)", got, want)
	}

	dec, n, err := codec.Decode(want)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if n != 16 {
		t.Errorf("Decode n = %d, want 16", n)
	}
	m := dec.(map[string]any)
	if m["i"].(int32) != 1 {
		t.Errorf("decoded i = %v, want 1", m["i"])
	}
	if m["d"].(float64) != 0.5 {
		t.Errorf("decoded d = %v, want 0.5", m["d"])
	}
}

// TestSpec_FixedRecord_NestedRecord_NestedPaddingResetsAtRecordStart asserts
// the padding rule applies relative to the record's start. A nested record's
// own padding is computed from the nested record's start, NOT from the outer
// record's start. This is the trickiest case for record encoding.
//
// Outer record: { a: HLAoctet (b=1), inner: FixedRecord{x: HLAoctet (b=1), y: HLAfloat64BE (b=8)} }
//
// Outer boundary = max(1, inner.boundary) = max(1, 8) = 8.
// Layout:
//   offset 0: a (1 byte, 0x07)
//   offset 1..7: padding to inner's boundary (8) — 7 bytes
//   offset 8: inner.x (1 byte, 0x09)
//   offset 9..15: padding inside inner from inner-start, to align inner.y on 8 — 7 bytes
//   offset 16..23: inner.y as float64 (1.0 = 0x3FF0000000000000)
func TestSpec_FixedRecord_NestedRecord_NestedPaddingResetsAtRecordStart(t *testing.T) {
	t.Parallel()

	inner := NewFixedRecord([]FixedRecordField{
		{Name: "x", Codec: HLAoctet{}},
		{Name: "y", Codec: hlaFloat64BE{}},
	})
	outer := NewFixedRecord([]FixedRecordField{
		{Name: "a", Codec: HLAoctet{}},
		{Name: "inner", Codec: inner},
	})

	if got, want := inner.OctetBoundary(), 8; got != want {
		t.Fatalf("inner.OctetBoundary() = %d, want %d", got, want)
	}
	if got, want := outer.OctetBoundary(), 8; got != want {
		t.Fatalf("outer.OctetBoundary() = %d, want %d", got, want)
	}

	value := map[string]any{
		"a": byte(0x07),
		"inner": map[string]any{
			"x": byte(0x09),
			"y": float64(1.0),
		},
	}

	got, err := outer.Encode(value)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// 0x07 | 7×00 | 0x09 | 7×00 | 0x3FF0000000000000
	want, _ := hex.DecodeString("0700000000000000" + "0900000000000000" + "3ff0000000000000")
	if !bytes.Equal(got, want) {
		t.Fatalf("Encode bytes = %x, want %x", got, want)
	}

	dec, n, err := outer.Decode(want)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if n != len(want) {
		t.Errorf("Decode n = %d, want %d", n, len(want))
	}
	m := dec.(map[string]any)
	if m["a"].(byte) != 0x07 {
		t.Errorf("decoded outer.a = %v, want 0x07", m["a"])
	}
	im := m["inner"].(map[string]any)
	if im["x"].(byte) != 0x09 {
		t.Errorf("decoded inner.x = %v, want 0x09", im["x"])
	}
	if im["y"].(float64) != 1.0 {
		t.Errorf("decoded inner.y = %v, want 1.0", im["y"])
	}
}

// TestFixedRecord_Encode_MissingField rejects a value map that omits a
// declared field with a meaningful error.
func TestFixedRecord_Encode_MissingField(t *testing.T) {
	t.Parallel()

	codec := NewFixedRecord([]FixedRecordField{
		{Name: "x", Codec: HLAinteger32BE{}},
		{Name: "y", Codec: HLAinteger32BE{}},
	})

	_, err := codec.Encode(map[string]any{"x": int32(1)})
	if err == nil {
		t.Fatalf("Encode missing field: want error, got nil")
	}
}

// TestFixedRecord_Encode_WrongType rejects a non-map value with a typed error.
func TestFixedRecord_Encode_WrongType(t *testing.T) {
	t.Parallel()

	codec := NewFixedRecord([]FixedRecordField{
		{Name: "x", Codec: HLAinteger32BE{}},
	})

	if _, err := codec.Encode("not-a-map"); err == nil {
		t.Errorf("Encode of non-map: want error, got nil")
	} else if !errors.Is(err, ErrEncTypeMismatch) {
		t.Errorf("Encode of non-map: want ErrEncTypeMismatch, got %v", err)
	}
}

// TestFixedRecord_Decode_ShortBuffer returns ErrEncShortBuffer when the
// input is shorter than the encoded record.
func TestFixedRecord_Decode_ShortBuffer(t *testing.T) {
	t.Parallel()

	codec := NewFixedRecord([]FixedRecordField{
		{Name: "x", Codec: HLAinteger32BE{}},
		{Name: "y", Codec: HLAinteger32BE{}},
	})

	if _, _, err := codec.Decode([]byte{0x00, 0x00, 0x00}); err == nil {
		t.Errorf("Decode short buffer: want error, got nil")
	}
}

// TestFixedRecord_Empty_OctetBoundaryOne asserts an empty (zero-field)
// fixed record is encodeable to zero bytes and reports OctetBoundary 1
// (the smallest legal alignment), per the most permissive reading of the
// spec — the caller can still combine it with other types.
func TestFixedRecord_Empty_OctetBoundaryOne(t *testing.T) {
	t.Parallel()

	codec := NewFixedRecord(nil)
	if got, want := codec.OctetBoundary(), 1; got != want {
		t.Errorf("OctetBoundary() = %d, want %d", got, want)
	}
	got, err := codec.Encode(map[string]any{})
	if err != nil {
		t.Errorf("Encode empty record: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Encode empty record bytes len = %d, want 0", len(got))
	}
}
