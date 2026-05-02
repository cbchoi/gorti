package encoding

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// ErrCodecForUnsupported is returned by CodecFor when the descriptor's
// concrete type is recognized but cannot yet be turned into a Codec —
// for example a FixedRecord field referencing a user-defined type that
// is not in the primitive registry. Callers may errors.Is against this
// to distinguish "not yet implemented at this layer" from genuine
// invalid-input errors.
var ErrCodecForUnsupported = errors.New("encoding: CodecFor: type not supported by primitive registry")

// dispatch is the body of CodecFor. It accepts either a FOM model
// DataType (the production path Agent A's RTI uses) or a JSON-shaped
// descriptor (a map[string]any with a "kind" key, used by the spec test
// fixtures and any other JSON-driven path). Anything else is rejected.
func dispatch(dt any) (Codec, error) {
	if dt == nil {
		return nil, errors.New("encoding: CodecFor: nil descriptor")
	}
	switch d := dt.(type) {
	case string:
		return PrimitiveByName(d)
	case map[string]any:
		return codecForJSON(d)
	case model.DataType:
		return codecForModel(d)
	}
	return nil, fmt.Errorf("encoding: CodecFor: unsupported descriptor type %T", dt)
}

// codecForModel branches on the concrete model.DataType variant.
func codecForModel(dt model.DataType) (Codec, error) {
	switch d := dt.(type) {
	case *model.BasicData:
		return PrimitiveByName(d.NameField)
	case *model.SimpleData:
		return PrimitiveByName(d.Representation)
	case *model.EnumeratedData:
		return PrimitiveByName(d.Representation)
	case *model.ArrayData:
		return modelArray(d)
	case *model.FixedRecordData:
		return modelFixedRecord(d)
	case *model.VariantRecordData:
		return modelVariantRecord(d)
	}
	return nil, fmt.Errorf("encoding: CodecFor: unsupported model.DataType %T", dt)
}

// modelArray builds a fixed- or variable-array codec from an ArrayData
// descriptor. Element resolution goes through PrimitiveByName, so a
// non-primitive element type yields ErrCodecForUnsupported.
func modelArray(d *model.ArrayData) (Codec, error) {
	elem, err := PrimitiveByName(d.DataType)
	if err != nil {
		return nil, fmt.Errorf("%w: array %q element %q: %v",
			ErrCodecForUnsupported, d.NameField, d.DataType, err)
	}
	if d.Cardinality == "Dynamic" {
		return NewVariableArray(elem), nil
	}
	n, err := strconv.Atoi(d.Cardinality)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("encoding: CodecFor: array %q invalid cardinality %q",
			d.NameField, d.Cardinality)
	}
	return NewFixedArray(elem, n), nil
}

// modelFixedRecord builds a fixed-record codec by looking up each field's
// primitive type in the registry. Nested user-defined types are not
// supported at this layer; they require an upstream resolver and will be
// added in a follow-up task.
func modelFixedRecord(d *model.FixedRecordData) (Codec, error) {
	fields := make([]FixedRecordField, 0, len(d.Fields))
	for _, f := range d.Fields {
		fc, err := PrimitiveByName(f.DataType)
		if err != nil {
			return nil, fmt.Errorf("%w: record %q field %q (%q): %v",
				ErrCodecForUnsupported, d.NameField, f.Name, f.DataType, err)
		}
		fields = append(fields, FixedRecordField{Name: f.Name, Codec: fc})
	}
	return NewFixedRecord(fields), nil
}

// modelVariantRecord builds a variant-record codec. The discriminant
// type's codec determines the Go type of the alternative-map keys via
// parseDiscriminantValue.
func modelVariantRecord(d *model.VariantRecordData) (Codec, error) {
	disc, err := PrimitiveByName(d.DiscriminantType)
	if err != nil {
		return nil, fmt.Errorf("%w: variant %q discriminator %q: %v",
			ErrCodecForUnsupported, d.NameField, d.DiscriminantType, err)
	}
	alts := make(map[any]Codec, len(d.Alternatives))
	for _, a := range d.Alternatives {
		key, err := parseDiscriminantValue(d.DiscriminantType, a.Enumerator)
		if err != nil {
			return nil, fmt.Errorf("encoding: CodecFor: variant %q alt %q enumerator %q: %w",
				d.NameField, a.Name, a.Enumerator, err)
		}
		altCodec, err := PrimitiveByName(a.DataType)
		if err != nil {
			return nil, fmt.Errorf("%w: variant %q alt %q (%q): %v",
				ErrCodecForUnsupported, d.NameField, a.Name, a.DataType, err)
		}
		alts[key] = altCodec
	}
	return NewVariantRecord(disc, alts), nil
}

// codecForJSON dispatches on the "kind" of a JSON-shaped descriptor.
// Recurses into nested record/array fields when their type is itself a
// map[string]any.
func codecForJSON(m map[string]any) (Codec, error) {
	kindRaw, ok := m["kind"]
	if !ok {
		return nil, errors.New("encoding: CodecFor: JSON descriptor missing \"kind\"")
	}
	kind, ok := kindRaw.(string)
	if !ok {
		return nil, fmt.Errorf("encoding: CodecFor: JSON \"kind\" is %T, want string", kindRaw)
	}
	switch kind {
	case "HLAfixedArray":
		return jsonFixedArray(m)
	case "HLAvariableArray":
		return jsonVariableArray(m)
	case "HLAfixedRecord":
		return jsonFixedRecord(m)
	case "HLAvariantRecord":
		return jsonVariantRecord(m)
	case "HLAopaqueData":
		return NewOpaqueData(), nil
	}
	return nil, fmt.Errorf("encoding: CodecFor: unknown JSON kind %q", kind)
}

func jsonFixedArray(m map[string]any) (Codec, error) {
	elem, err := jsonElement(m["element"])
	if err != nil {
		return nil, fmt.Errorf("encoding: CodecFor: HLAfixedArray element: %w", err)
	}
	cardRaw, ok := m["cardinality"]
	if !ok {
		return nil, errors.New("encoding: CodecFor: HLAfixedArray missing cardinality")
	}
	n, err := jsonCardinality(cardRaw)
	if err != nil {
		return nil, err
	}
	return NewFixedArray(elem, n), nil
}

func jsonVariableArray(m map[string]any) (Codec, error) {
	elem, err := jsonElement(m["element"])
	if err != nil {
		return nil, fmt.Errorf("encoding: CodecFor: HLAvariableArray element: %w", err)
	}
	return NewVariableArray(elem), nil
}

func jsonFixedRecord(m map[string]any) (Codec, error) {
	rawFields, _ := m["fields"].([]any)
	fields := make([]FixedRecordField, 0, len(rawFields))
	for i, rf := range rawFields {
		fm, ok := rf.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("encoding: CodecFor: HLAfixedRecord field %d: not an object", i)
		}
		name, ok := fm["name"].(string)
		if !ok {
			return nil, fmt.Errorf("encoding: CodecFor: HLAfixedRecord field %d: missing name", i)
		}
		fc, err := jsonElement(fm["type"])
		if err != nil {
			return nil, fmt.Errorf("encoding: CodecFor: HLAfixedRecord field %q: %w", name, err)
		}
		fields = append(fields, FixedRecordField{Name: name, Codec: fc})
	}
	return NewFixedRecord(fields), nil
}

func jsonVariantRecord(m map[string]any) (Codec, error) {
	discName, ok := m["discriminator"].(string)
	if !ok {
		return nil, errors.New("encoding: CodecFor: HLAvariantRecord missing or non-string discriminator")
	}
	disc, err := PrimitiveByName(discName)
	if err != nil {
		return nil, fmt.Errorf("encoding: CodecFor: HLAvariantRecord discriminator %q: %w", discName, err)
	}
	rawAlts, _ := m["alternatives"].([]any)
	alts := make(map[any]Codec, len(rawAlts))
	for i, ra := range rawAlts {
		am, ok := ra.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("encoding: CodecFor: HLAvariantRecord alternative %d: not an object", i)
		}
		key, err := parseDiscriminantValue(discName, am["discriminant"])
		if err != nil {
			return nil, fmt.Errorf("encoding: CodecFor: HLAvariantRecord alt %d discriminant: %w", i, err)
		}
		altCodec, err := jsonElement(am["type"])
		if err != nil {
			return nil, fmt.Errorf("encoding: CodecFor: HLAvariantRecord alt %d type: %w", i, err)
		}
		alts[key] = altCodec
	}
	return NewVariantRecord(disc, alts), nil
}

// jsonElement resolves the "type" field of a JSON descriptor: a string
// names a primitive; a map recurses through codecForJSON.
func jsonElement(v any) (Codec, error) {
	switch t := v.(type) {
	case string:
		return PrimitiveByName(t)
	case map[string]any:
		return codecForJSON(t)
	case nil:
		return nil, errors.New("missing element/type")
	}
	return nil, fmt.Errorf("element/type has unsupported shape %T", v)
}

// jsonCardinality coerces the cardinality value (typically float64 from
// JSON unmarshaling, possibly int from programmatic callers) to a
// non-negative int.
func jsonCardinality(v any) (int, error) {
	switch x := v.(type) {
	case float64:
		n := int(x)
		if float64(n) != x || n < 0 {
			return 0, fmt.Errorf("encoding: CodecFor: cardinality %v is not a non-negative integer", x)
		}
		return n, nil
	case int:
		if x < 0 {
			return 0, fmt.Errorf("encoding: CodecFor: cardinality %d negative", x)
		}
		return x, nil
	case int64:
		if x < 0 {
			return 0, fmt.Errorf("encoding: CodecFor: cardinality %d negative", x)
		}
		return int(x), nil
	}
	return 0, fmt.Errorf("encoding: CodecFor: cardinality has unsupported type %T", v)
}

// parseDiscriminantValue converts a string enumerator (model path) or a
// JSON-decoded value (JSON path) to the Go type the discriminator's
// Decode would emit. The variant-record codec keys the alternative map
// by that exact Go type, so this conversion is required for the lookup
// to succeed at encode time.
func parseDiscriminantValue(discName string, raw any) (any, error) {
	switch discName {
	case "HLAinteger16BE", "HLAinteger16LE":
		return parseDiscInt16(raw)
	case "HLAinteger32BE", "HLAinteger32LE":
		return parseDiscInt32(raw)
	case "HLAinteger64BE", "HLAinteger64LE":
		return parseDiscInt64(raw)
	case "HLAoctet":
		return parseDiscOctet(raw)
	case "HLAboolean":
		return parseDiscBool(raw)
	}
	return nil, fmt.Errorf("discriminator type %q not supported", discName)
}

func parseDiscInt16(raw any) (any, error) {
	f, err := toInt64Loose(raw)
	if err != nil {
		return nil, err
	}
	return int32(int16(f)), nil
}

func parseDiscInt32(raw any) (any, error) {
	f, err := toInt64Loose(raw)
	if err != nil {
		return nil, err
	}
	return int32(f), nil
}

func parseDiscInt64(raw any) (any, error) {
	return toInt64Loose(raw)
}

func parseDiscOctet(raw any) (any, error) {
	f, err := toInt64Loose(raw)
	if err != nil {
		return nil, err
	}
	if f < 0 || f > 255 {
		return nil, fmt.Errorf("octet discriminator %d out of range [0,255]", f)
	}
	return byte(f), nil
}

func parseDiscBool(raw any) (any, error) {
	switch x := raw.(type) {
	case bool:
		return x, nil
	case float64:
		return x != 0, nil
	case string:
		b, err := strconv.ParseBool(x)
		if err != nil {
			return nil, fmt.Errorf("boolean discriminator %q: %w", x, err)
		}
		return b, nil
	}
	return nil, fmt.Errorf("boolean discriminator has unsupported type %T", raw)
}

// toInt64Loose accepts string (model.VariantAlternative.Enumerator) or
// any JSON-decoded number and returns it as int64.
func toInt64Loose(v any) (int64, error) {
	switch x := v.(type) {
	case string:
		i, err := strconv.ParseInt(x, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot parse %q as integer: %w", x, err)
		}
		return i, nil
	case float64:
		i := int64(x)
		if float64(i) != x {
			return 0, fmt.Errorf("number %v is not an integer", x)
		}
		return i, nil
	case int:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case int64:
		return x, nil
	}
	return 0, fmt.Errorf("cannot interpret %T as integer", v)
}
