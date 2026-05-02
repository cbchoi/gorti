package encoding

import (
	"fmt"
)

// HLAfixedRecord composite codec per IEEE 1516.2-2010 §4.
//
// A fixed record is an ordered sequence of named, heterogeneously-typed
// fields. Per §4 each field is preceded by zero-or-more padding octets
// such that the field starts at an offset that is a multiple of its own
// OctetBoundary, measured from the start of the record. The first field
// has no leading padding because offset 0 satisfies any boundary.
//
// The record's overall OctetBoundary is the maximum of its fields'
// boundaries; when a fixed record is itself a field of an outer record,
// that maximum determines the alignment applied at the outer level.

// FixedRecordField is one named field of a fixed record. Order is
// significant — the slice passed to NewFixedRecord is the wire order.
type FixedRecordField struct {
	// Name is the field's identifier; it is used for value lookup in the
	// map[string]any input/output and for diagnostics.
	Name string
	// Codec encodes/decodes the field's value.
	Codec Codec
}

// FixedRecord is the runtime codec for an HLAfixedRecord with the
// declared field order and per-field codecs.
type FixedRecord struct {
	fields   []FixedRecordField
	boundary int
}

// NewFixedRecord constructs a Codec for an ordered named-field record.
// Field order is the declared order in fields. Empty records are legal:
// they encode to zero bytes and have OctetBoundary 1.
func NewFixedRecord(fields []FixedRecordField) *FixedRecord {
	boundary := 1
	for _, f := range fields {
		if b := f.Codec.OctetBoundary(); b > boundary {
			boundary = b
		}
	}
	return &FixedRecord{
		fields:   append([]FixedRecordField(nil), fields...),
		boundary: boundary,
	}
}

// OctetBoundary returns the maximum boundary across the record's fields,
// or 1 for an empty record.
func (r *FixedRecord) OctetBoundary() int { return r.boundary }

// Encode implements Codec. v must be a map[string]any keyed by field name;
// every declared field must be present.
func (r *FixedRecord) Encode(v any) ([]byte, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: HLAfixedRecord want map[string]any, got %T", ErrEncTypeMismatch, v)
	}
	out := make([]byte, 0, 16*len(r.fields))
	for _, f := range r.fields {
		out = padToBoundary(out, f.Codec.OctetBoundary())
		fv, present := m[f.Name]
		if !present {
			return nil, fmt.Errorf("encoding: HLAfixedRecord field %q missing from input", f.Name)
		}
		fb, err := f.Codec.Encode(fv)
		if err != nil {
			return nil, fmt.Errorf("encoding: HLAfixedRecord field %q: %w", f.Name, err)
		}
		out = append(out, fb...)
	}
	return out, nil
}

// Decode implements Codec. Returns map[string]any populated with each
// field's decoded value, plus the total bytes consumed (including
// inter-field padding).
func (r *FixedRecord) Decode(b []byte) (any, int, error) {
	out := make(map[string]any, len(r.fields))
	pos := 0
	for _, f := range r.fields {
		pad := paddingFor(pos, f.Codec.OctetBoundary())
		if pos+pad > len(b) {
			return nil, 0, fmt.Errorf("%w: HLAfixedRecord padding before field %q", ErrEncShortBuffer, f.Name)
		}
		pos += pad
		val, n, err := f.Codec.Decode(b[pos:])
		if err != nil {
			return nil, 0, fmt.Errorf("encoding: HLAfixedRecord field %q decode: %w", f.Name, err)
		}
		out[f.Name] = val
		pos += n
	}
	return out, pos, nil
}

// padToBoundary appends zero bytes to buf such that len(buf) is a multiple
// of boundary. boundary must be >= 1.
func padToBoundary(buf []byte, boundary int) []byte {
	pad := paddingFor(len(buf), boundary)
	for i := 0; i < pad; i++ {
		buf = append(buf, 0)
	}
	return buf
}

// paddingFor returns the number of padding octets needed at offset to
// reach the next multiple of boundary. boundary must be >= 1.
func paddingFor(offset, boundary int) int {
	if boundary <= 1 {
		return 0
	}
	rem := offset % boundary
	if rem == 0 {
		return 0
	}
	return boundary - rem
}
