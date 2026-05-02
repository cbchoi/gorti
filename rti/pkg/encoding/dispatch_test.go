package encoding

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// TestSpec_CodecFor_Primitive_Model_BasicData asserts CodecFor accepts a
// *model.BasicData and returns the same primitive codec PrimitiveByName
// would. The body of CodecFor must look the data type up by its declared
// FOM name in the primitive registry.
//
// Implements: FR-ENC-1 (composite dispatcher over the FOM model).
func TestSpec_CodecFor_Primitive_Model_BasicData(t *testing.T) {
	t.Parallel()

	dt := &model.BasicData{NameField: "HLAinteger32BE", Size: 32, Endian: "Big"}
	got, err := CodecFor(dt)
	if err != nil {
		t.Fatalf("CodecFor(*BasicData{HLAinteger32BE}): %v", err)
	}
	want, err := PrimitiveByName("HLAinteger32BE")
	if err != nil {
		t.Fatalf("PrimitiveByName: %v", err)
	}
	if got != want {
		t.Errorf("CodecFor returned %T, want same as PrimitiveByName (%T)", got, want)
	}
}

// TestSpec_CodecFor_Primitive_Model_BasicData_Unknown asserts that an
// unknown primitive name on a *model.BasicData returns a non-nil error,
// not a nil-codec, nil-error pair (which would crash callers).
func TestSpec_CodecFor_Primitive_Model_BasicData_Unknown(t *testing.T) {
	t.Parallel()

	dt := &model.BasicData{NameField: "HLAimaginary"}
	c, err := CodecFor(dt)
	if err == nil {
		t.Fatalf("CodecFor unknown basic: want error, got codec %v", c)
	}
}

// TestSpec_CodecFor_Primitive_Model_SimpleData asserts that a SimpleData
// forwards to the codec of its representation. SimpleData is a named alias
// (units, accuracy, resolution metadata) over a basic representation.
func TestSpec_CodecFor_Primitive_Model_SimpleData(t *testing.T) {
	t.Parallel()

	dt := &model.SimpleData{NameField: "Velocity", Representation: "HLAfloat64BE"}
	got, err := CodecFor(dt)
	if err != nil {
		t.Fatalf("CodecFor(*SimpleData{HLAfloat64BE}): %v", err)
	}
	want, _ := PrimitiveByName("HLAfloat64BE")
	if got != want {
		t.Errorf("CodecFor SimpleData returned %T, want representation primitive %T", got, want)
	}
}

// TestSpec_CodecFor_Primitive_Model_EnumeratedData asserts that an
// EnumeratedData forwards to the codec of its representation.
func TestSpec_CodecFor_Primitive_Model_EnumeratedData(t *testing.T) {
	t.Parallel()

	dt := &model.EnumeratedData{NameField: "Color", Representation: "HLAinteger32BE"}
	got, err := CodecFor(dt)
	if err != nil {
		t.Fatalf("CodecFor(*EnumeratedData{HLAinteger32BE}): %v", err)
	}
	want, _ := PrimitiveByName("HLAinteger32BE")
	if got != want {
		t.Errorf("CodecFor EnumeratedData returned %T, want representation primitive %T", got, want)
	}
}

// TestSpec_CodecFor_Composite_Model_FixedArray asserts that an ArrayData
// with a numeric Cardinality builds a fixed-array codec over the named
// element type, with the right cardinality and OctetBoundary.
func TestSpec_CodecFor_Composite_Model_FixedArray(t *testing.T) {
	t.Parallel()

	dt := &model.ArrayData{
		NameField:   "Triple",
		DataType:    "HLAinteger32BE",
		Cardinality: "3",
	}
	c, err := CodecFor(dt)
	if err != nil {
		t.Fatalf("CodecFor(*ArrayData fixed): %v", err)
	}
	if got := c.OctetBoundary(); got != 4 {
		t.Errorf("OctetBoundary = %d, want 4 (element boundary)", got)
	}
	got, err := c.Encode([]any{int32(1), int32(2), int32(3)})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want, _ := hex.DecodeString("000000010000000200000003")
	if !bytes.Equal(got, want) {
		t.Errorf("Encode bytes = %x, want %x", got, want)
	}
}

// TestSpec_CodecFor_Composite_Model_FixedArray_BadCardinality rejects
// non-numeric, non-Dynamic cardinality values with a meaningful error.
func TestSpec_CodecFor_Composite_Model_FixedArray_BadCardinality(t *testing.T) {
	t.Parallel()

	dt := &model.ArrayData{NameField: "Bad", DataType: "HLAoctet", Cardinality: "tres"}
	if _, err := CodecFor(dt); err == nil {
		t.Errorf("CodecFor with bad cardinality: want error, got nil")
	}
}

// TestSpec_CodecFor_Composite_Model_VariableArray asserts that an
// ArrayData with Cardinality "Dynamic" builds a variable-array codec.
func TestSpec_CodecFor_Composite_Model_VariableArray(t *testing.T) {
	t.Parallel()

	dt := &model.ArrayData{
		NameField:   "Series",
		DataType:    "HLAinteger32BE",
		Cardinality: "Dynamic",
	}
	c, err := CodecFor(dt)
	if err != nil {
		t.Fatalf("CodecFor(*ArrayData dynamic): %v", err)
	}
	got, err := c.Encode([]any{int32(1), int32(2)})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want, _ := hex.DecodeString("000000020000000100000002")
	if !bytes.Equal(got, want) {
		t.Errorf("Encode bytes = %x, want %x", got, want)
	}
}

// TestSpec_CodecFor_Composite_Model_FixedRecord asserts a FixedRecordData
// builds a record codec preserving field order.
func TestSpec_CodecFor_Composite_Model_FixedRecord(t *testing.T) {
	t.Parallel()

	dt := &model.FixedRecordData{
		NameField: "Pair",
		Fields: []model.RecordField{
			{Name: "x", DataType: "HLAinteger32BE"},
			{Name: "y", DataType: "HLAinteger32BE"},
		},
	}
	c, err := CodecFor(dt)
	if err != nil {
		t.Fatalf("CodecFor(*FixedRecordData): %v", err)
	}
	got, err := c.Encode(map[string]any{"x": int32(1), "y": int32(-1)})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want, _ := hex.DecodeString("00000001ffffffff")
	if !bytes.Equal(got, want) {
		t.Errorf("Encode bytes = %x, want %x", got, want)
	}
}

// TestSpec_CodecFor_Composite_Model_FixedRecord_UnknownField rejects a
// record whose field references an unknown primitive type.
func TestSpec_CodecFor_Composite_Model_FixedRecord_UnknownField(t *testing.T) {
	t.Parallel()

	dt := &model.FixedRecordData{
		NameField: "Bad",
		Fields: []model.RecordField{
			{Name: "x", DataType: "HLAimaginary"},
		},
	}
	if _, err := CodecFor(dt); err == nil {
		t.Errorf("CodecFor record with unknown field type: want error, got nil")
	}
}

// TestSpec_CodecFor_Composite_Model_VariantRecord asserts a
// VariantRecordData builds a variant-record codec whose alternative keys
// are typed to match the discriminator's decoded value.
func TestSpec_CodecFor_Composite_Model_VariantRecord(t *testing.T) {
	t.Parallel()

	dt := &model.VariantRecordData{
		NameField:        "Tag",
		DiscriminantName: "kind",
		DiscriminantType: "HLAinteger32BE",
		Alternatives: []model.VariantAlternative{
			{Enumerator: "1", Name: "a", DataType: "HLAoctet"},
			{Enumerator: "2", Name: "b", DataType: "HLAfloat64BE"},
		},
	}
	c, err := CodecFor(dt)
	if err != nil {
		t.Fatalf("CodecFor(*VariantRecordData): %v", err)
	}
	got, err := c.Encode(map[string]any{
		"discriminator": int32(1),
		"value":         byte(0xAB),
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want, _ := hex.DecodeString("00000001ab")
	if !bytes.Equal(got, want) {
		t.Errorf("Encode = %x, want %x", got, want)
	}
}

// TestSpec_CodecFor_Composite_Model_VariantRecord_BadEnumerator rejects a
// VariantRecordData whose alternative enumerator string fails to parse
// against the discriminator type.
func TestSpec_CodecFor_Composite_Model_VariantRecord_BadEnumerator(t *testing.T) {
	t.Parallel()

	dt := &model.VariantRecordData{
		NameField:        "Bad",
		DiscriminantName: "kind",
		DiscriminantType: "HLAinteger32BE",
		Alternatives: []model.VariantAlternative{
			{Enumerator: "not-an-int", Name: "x", DataType: "HLAoctet"},
		},
	}
	if _, err := CodecFor(dt); err == nil {
		t.Errorf("CodecFor with non-numeric int enumerator: want error, got nil")
	}
}

// TestSpec_CodecFor_NilOrUnknownInput rejects a nil dt argument and an
// unrecognized concrete type with errors. nil must not panic.
func TestSpec_CodecFor_NilOrUnknownInput(t *testing.T) {
	t.Parallel()

	if _, err := CodecFor(nil); err == nil {
		t.Errorf("CodecFor(nil): want error, got nil")
	}
	if _, err := CodecFor(42); err == nil {
		t.Errorf("CodecFor(int): want error, got nil")
	}
}

// TestSpec_CodecFor_JSON_StringPrimitive asserts that passing a plain
// string descriptor (the shape used by primitive vectors) returns the
// expected primitive codec. This makes CodecFor a single uniform entry
// point for both the JSON-fixture path and the model path.
func TestSpec_CodecFor_JSON_StringPrimitive(t *testing.T) {
	t.Parallel()

	c, err := CodecFor("HLAinteger32BE")
	if err != nil {
		t.Fatalf("CodecFor(\"HLAinteger32BE\"): %v", err)
	}
	want, _ := PrimitiveByName("HLAinteger32BE")
	if c != want {
		t.Errorf("CodecFor string returned %T, want %T", c, want)
	}
}

// TestSpec_CodecFor_JSON_FixedArray asserts that a map[string]any
// descriptor with kind=HLAfixedArray builds a fixed-array codec, mirroring
// the encoding_vectors.json shape:
//
//	{"kind": "HLAfixedArray", "element": "HLAinteger32BE", "cardinality": 3}
//
// JSON decodes the cardinality as float64; the dispatcher must accept that.
func TestSpec_CodecFor_JSON_FixedArray(t *testing.T) {
	t.Parallel()

	desc := map[string]any{
		"kind":        "HLAfixedArray",
		"element":     "HLAinteger32BE",
		"cardinality": float64(3),
	}
	c, err := CodecFor(desc)
	if err != nil {
		t.Fatalf("CodecFor JSON fixed array: %v", err)
	}
	got, err := c.Encode([]any{int32(1), int32(2), int32(3)})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want, _ := hex.DecodeString("000000010000000200000003")
	if !bytes.Equal(got, want) {
		t.Errorf("Encode = %x, want %x", got, want)
	}
}

// TestSpec_CodecFor_JSON_VariableArray asserts the dispatcher builds a
// variable-array codec from the JSON descriptor shape.
func TestSpec_CodecFor_JSON_VariableArray(t *testing.T) {
	t.Parallel()

	desc := map[string]any{
		"kind":    "HLAvariableArray",
		"element": "HLAinteger32BE",
	}
	c, err := CodecFor(desc)
	if err != nil {
		t.Fatalf("CodecFor JSON variable array: %v", err)
	}
	got, err := c.Encode([]any{int32(1), int32(2)})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want, _ := hex.DecodeString("000000020000000100000002")
	if !bytes.Equal(got, want) {
		t.Errorf("Encode = %x, want %x", got, want)
	}
}

// TestSpec_CodecFor_JSON_OpaqueData asserts the singleton opaque codec
// is returned for kind=HLAopaqueData.
func TestSpec_CodecFor_JSON_OpaqueData(t *testing.T) {
	t.Parallel()

	desc := map[string]any{"kind": "HLAopaqueData"}
	c, err := CodecFor(desc)
	if err != nil {
		t.Fatalf("CodecFor JSON opaque: %v", err)
	}
	got, err := c.Encode([]byte{0xDE, 0xAD, 0xBE, 0xEF})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want, _ := hex.DecodeString("00000004deadbeef")
	if !bytes.Equal(got, want) {
		t.Errorf("Encode = %x, want %x", got, want)
	}
}

// TestSpec_CodecFor_JSON_FixedRecord asserts the dispatcher walks the
// fields list in order and builds a record codec, including nested
// records (a field whose "type" is itself an object descriptor).
func TestSpec_CodecFor_JSON_FixedRecord(t *testing.T) {
	t.Parallel()

	desc := map[string]any{
		"kind": "HLAfixedRecord",
		"fields": []any{
			map[string]any{"name": "a", "type": "HLAoctet"},
			map[string]any{"name": "b", "type": "HLAfloat64BE"},
		},
	}
	c, err := CodecFor(desc)
	if err != nil {
		t.Fatalf("CodecFor JSON fixed record: %v", err)
	}
	got, err := c.Encode(map[string]any{"a": byte(0x05), "b": float64(1.0)})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want, _ := hex.DecodeString("05000000000000003ff0000000000000")
	if !bytes.Equal(got, want) {
		t.Errorf("Encode = %x, want %x", got, want)
	}
}

// TestSpec_CodecFor_JSON_FixedRecord_Nested asserts the dispatcher
// recurses through nested HLAfixedRecord descriptors, mirroring the
// fixed-record-nested-octet-record-octet-float64 vector.
func TestSpec_CodecFor_JSON_FixedRecord_Nested(t *testing.T) {
	t.Parallel()

	desc := map[string]any{
		"kind": "HLAfixedRecord",
		"fields": []any{
			map[string]any{"name": "a", "type": "HLAoctet"},
			map[string]any{
				"name": "inner",
				"type": map[string]any{
					"kind": "HLAfixedRecord",
					"fields": []any{
						map[string]any{"name": "x", "type": "HLAoctet"},
						map[string]any{"name": "y", "type": "HLAfloat64BE"},
					},
				},
			},
		},
	}
	c, err := CodecFor(desc)
	if err != nil {
		t.Fatalf("CodecFor JSON nested record: %v", err)
	}
	got, err := c.Encode(map[string]any{
		"a": byte(0x07),
		"inner": map[string]any{
			"x": byte(0x09),
			"y": float64(1.0),
		},
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want, _ := hex.DecodeString("07000000000000000900000000000000" + "3ff0000000000000")
	if !bytes.Equal(got, want) {
		t.Errorf("Encode = %x, want %x", got, want)
	}
}

// TestSpec_CodecFor_JSON_VariantRecord asserts the dispatcher builds a
// variant-record codec from the JSON descriptor: a discriminator string +
// a list of {discriminant, type} alternatives. The numeric discriminants
// in JSON are float64 and must be coerced to the decoded type of the
// discriminator (here int32).
func TestSpec_CodecFor_JSON_VariantRecord(t *testing.T) {
	t.Parallel()

	desc := map[string]any{
		"kind":          "HLAvariantRecord",
		"discriminator": "HLAinteger32BE",
		"alternatives": []any{
			map[string]any{"discriminant": float64(1), "type": "HLAoctet"},
			map[string]any{"discriminant": float64(2), "type": "HLAfloat64BE"},
		},
	}
	c, err := CodecFor(desc)
	if err != nil {
		t.Fatalf("CodecFor JSON variant record: %v", err)
	}

	// Disc=1 selects HLAoctet alternative.
	got1, err := c.Encode(map[string]any{
		"discriminator": int32(1),
		"value":         byte(0xAB),
	})
	if err != nil {
		t.Fatalf("Encode disc=1: %v", err)
	}
	want1, _ := hex.DecodeString("00000001ab")
	if !bytes.Equal(got1, want1) {
		t.Errorf("Encode disc=1 = %x, want %x", got1, want1)
	}

	// Disc=2 selects HLAfloat64BE — 4 padding bytes after disc.
	got2, err := c.Encode(map[string]any{
		"discriminator": int32(2),
		"value":         float64(1.0),
	})
	if err != nil {
		t.Fatalf("Encode disc=2: %v", err)
	}
	want2, _ := hex.DecodeString("00000002000000003ff0000000000000")
	if !bytes.Equal(got2, want2) {
		t.Errorf("Encode disc=2 = %x, want %x", got2, want2)
	}
}

// TestSpec_CodecFor_JSON_FixedArray_MissingFields rejects malformed
// fixed-array descriptors (missing element, missing or non-numeric
// cardinality) without panicking.
func TestSpec_CodecFor_JSON_FixedArray_MissingFields(t *testing.T) {
	t.Parallel()

	cases := []map[string]any{
		{"kind": "HLAfixedArray", "cardinality": float64(3)}, // missing element
		{"kind": "HLAfixedArray", "element": "HLAinteger32BE"}, // missing cardinality
		{"kind": "HLAfixedArray", "element": "HLAinteger32BE", "cardinality": "three"}, // non-numeric
		{"kind": "HLAfixedArray", "element": "HLAimaginary", "cardinality": float64(3)}, // unknown elem
	}
	for i, desc := range cases {
		if _, err := CodecFor(desc); err == nil {
			t.Errorf("case %d: CodecFor(%v): want error, got nil", i, desc)
		}
	}
}

// TestSpec_CodecFor_JSON_UnknownKind rejects unrecognized "kind" values.
func TestSpec_CodecFor_JSON_UnknownKind(t *testing.T) {
	t.Parallel()

	desc := map[string]any{"kind": "HLAimaginary"}
	if _, err := CodecFor(desc); err == nil {
		t.Errorf("CodecFor unknown kind: want error, got nil")
	}
	desc2 := map[string]any{} // missing kind
	if _, err := CodecFor(desc2); err == nil {
		t.Errorf("CodecFor missing kind: want error, got nil")
	}
}

// TestSpec_CodecFor_RoundTripCompositeVectors round-trips every composite
// vector in tests/conformance/encoding_vectors.json through the dispatcher,
// asserting that CodecFor builds a codec whose Encode produces the golden
// bytes and whose Decode reverses it. This is the M1 acceptance behavior
// for the composite codec build step.
func TestSpec_CodecFor_RoundTripCompositeVectors(t *testing.T) {
	t.Parallel()

	vectors := loadCompositeVectors(t)
	if len(vectors) == 0 {
		t.Fatalf("expected composite vectors in conformance fixture, found 0")
	}
	for _, v := range vectors {
		v := v
		t.Run(v.id, func(t *testing.T) {
			t.Parallel()

			c, err := CodecFor(v.descriptor)
			if err != nil {
				t.Fatalf("CodecFor(%v): %v", v.descriptor, err)
			}

			value := normalizeValue(v.descriptor, v.value)

			expected, err := hex.DecodeString(v.bytesHex)
			if err != nil {
				t.Fatalf("hex %q: %v", v.bytesHex, err)
			}

			got, err := c.Encode(value)
			if err != nil {
				t.Fatalf("Encode(%v): %v", value, err)
			}
			if !bytes.Equal(got, expected) {
				t.Fatalf("encode mismatch: got %x, want %x", got, expected)
			}

			dec, n, err := c.Decode(expected)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if n != len(expected) {
				t.Errorf("Decode consumed %d bytes, want %d", n, len(expected))
			}
			if !compositeValuesEqual(dec, value) {
				t.Errorf("decode mismatch: got %v, want %v", dec, value)
			}
		})
	}
}

// TestSpec_CodecFor_Model_NestedFixedRecord_ResolvesFromRegistry asserts
// that when a FixedRecordData field references a non-primitive type by
// name (not a primitive in primitiveCodecs), CodecFor returns an error
// indicating the missing reference. This documents that the model-side
// path of CodecFor does NOT carry a registry for user-defined types yet —
// nested user types need a separate task.
func TestSpec_CodecFor_Model_NestedFixedRecord_ResolvesFromRegistry(t *testing.T) {
	t.Parallel()

	dt := &model.FixedRecordData{
		NameField: "Outer",
		Fields: []model.RecordField{
			{Name: "inner", DataType: "MyUserDefinedType"},
		},
	}
	_, err := CodecFor(dt)
	if err == nil {
		t.Fatalf("CodecFor with user-defined nested type: want error, got nil")
	}
}
