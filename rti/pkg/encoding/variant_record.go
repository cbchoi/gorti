package encoding

import (
	"fmt"
)

// HLAvariantRecord composite codec per IEEE 1516.2-2010 §4.
//
// A variant record is a discriminated union: a discriminator field is
// followed by exactly one of a fixed set of alternative payload types,
// selected at encode time by the discriminator's value. The on-wire
// encoding is the discriminator followed by the selected alternative,
// with padding before the alternative computed from the start of the
// record (discriminator at offset 0, alternative at offset =
// alignUp(disc-len, alt-boundary)).
//
// The record's overall OctetBoundary is max(disc.OctetBoundary,
// max alt.OctetBoundary), so when a variant record nests inside another
// composite the outer alignment uses the worst-case alternative.

// VariantRecord is the runtime codec for an HLAvariantRecord.
type VariantRecord struct {
	disc         Codec
	alternatives map[any]Codec
	boundary     int
}

// NewVariantRecord constructs a Codec for a discriminator codec and a
// set of alternatives keyed by discriminator value. The keys' Go types
// must be comparable in the same way the discriminator's Decode returns
// values (e.g. an HLAinteger32BE discriminator decodes to int32, so
// alternative keys should be int32).
func NewVariantRecord(discriminator Codec, alternatives map[any]Codec) *VariantRecord {
	boundary := discriminator.OctetBoundary()
	for _, alt := range alternatives {
		if b := alt.OctetBoundary(); b > boundary {
			boundary = b
		}
	}
	if boundary < 1 {
		boundary = 1
	}
	// Defensive copy so caller can't mutate the alternative map afterwards.
	alts := make(map[any]Codec, len(alternatives))
	for k, v := range alternatives {
		alts[k] = v
	}
	return &VariantRecord{
		disc:         discriminator,
		alternatives: alts,
		boundary:     boundary,
	}
}

// OctetBoundary returns max(disc.OctetBoundary, max alt.OctetBoundary).
func (r *VariantRecord) OctetBoundary() int { return r.boundary }

// Encode implements Codec. v must be a map[string]any with keys
// "discriminator" and "value". The discriminator value selects which
// alternative codec encodes "value".
func (r *VariantRecord) Encode(v any) ([]byte, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: HLAvariantRecord want map[string]any, got %T", ErrEncTypeMismatch, v)
	}
	d, present := m["discriminator"]
	if !present {
		return nil, fmt.Errorf("encoding: HLAvariantRecord missing %q key", "discriminator")
	}
	val, present := m["value"]
	if !present {
		return nil, fmt.Errorf("encoding: HLAvariantRecord missing %q key", "value")
	}
	alt, ok := r.alternatives[d]
	if !ok {
		return nil, fmt.Errorf("%w: HLAvariantRecord no alternative for discriminator %v (%T)", ErrEncTypeMismatch, d, d)
	}

	out, err := r.disc.Encode(d)
	if err != nil {
		return nil, fmt.Errorf("encoding: HLAvariantRecord discriminator: %w", err)
	}
	out = padToBoundary(out, alt.OctetBoundary())
	altBytes, err := alt.Encode(val)
	if err != nil {
		return nil, fmt.Errorf("encoding: HLAvariantRecord alternative: %w", err)
	}
	out = append(out, altBytes...)
	return out, nil
}

// Decode implements Codec. Reads the discriminator, looks up the
// alternative, applies inter-field padding, and decodes the alternative.
// Returns map[string]any with "discriminator" and "value", and the total
// bytes consumed.
func (r *VariantRecord) Decode(b []byte) (any, int, error) {
	dv, dn, err := r.disc.Decode(b)
	if err != nil {
		return nil, 0, fmt.Errorf("encoding: HLAvariantRecord discriminator decode: %w", err)
	}
	alt, ok := r.alternatives[dv]
	if !ok {
		return nil, 0, fmt.Errorf("%w: HLAvariantRecord no alternative for discriminator %v (%T)", ErrEncTypeMismatch, dv, dv)
	}
	pad := paddingFor(dn, alt.OctetBoundary())
	pos := dn + pad
	if pos > len(b) {
		return nil, 0, fmt.Errorf("%w: HLAvariantRecord padding to alternative boundary", ErrEncShortBuffer)
	}
	val, vn, err := alt.Decode(b[pos:])
	if err != nil {
		return nil, 0, fmt.Errorf("encoding: HLAvariantRecord alternative decode: %w", err)
	}
	return map[string]any{
		"discriminator": dv,
		"value":         val,
	}, pos + vn, nil
}
