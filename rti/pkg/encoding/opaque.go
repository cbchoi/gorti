package encoding

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// HLAopaqueData is the simplest composite codec per IEEE 1516.2-2010 §4:
// a 4-byte big-endian length prefix followed by the raw byte payload.
// There is no element typing and no nested padding beyond the 4-byte
// length-prefix alignment.
//
// OctetBoundary is 4 (the length prefix alignment).

// opaqueDataCodec is the unexported value behind NewOpaqueData.
type opaqueDataCodec struct{}

// NewOpaqueData returns the singleton-shaped Codec for HLAopaqueData.
// A constructor (rather than an exported zero-value type) keeps the
// composite-codec API uniform with the array codecs.
func NewOpaqueData() Codec { return opaqueDataCodec{} }

// OctetBoundary returns 4.
func (opaqueDataCodec) OctetBoundary() int { return 4 }

// Encode accepts []byte (nil is treated as empty) and emits the length
// prefix + raw bytes.
func (opaqueDataCodec) Encode(v any) ([]byte, error) {
	xs, ok := v.([]byte)
	if !ok {
		return nil, fmt.Errorf("encoding: HLAopaqueData cannot encode %T (want []byte)", v)
	}
	out := make([]byte, 4+len(xs))
	binary.BigEndian.PutUint32(out[:4], uint32(len(xs)))
	copy(out[4:], xs)
	return out, nil
}

// Decode reads the 4-byte length prefix and returns the next length
// payload bytes as a freshly-allocated []byte (decoupled from b's
// underlying array so callers can keep b alive without aliasing).
func (opaqueDataCodec) Decode(b []byte) (any, int, error) {
	if len(b) < 4 {
		return nil, 0, errors.New("encoding: HLAopaqueData decode short buffer (length prefix)")
	}
	n := int(binary.BigEndian.Uint32(b[:4]))
	if n < 0 || 4+n > len(b) {
		return nil, 0, errors.New("encoding: HLAopaqueData decode short buffer (payload)")
	}
	out := make([]byte, n)
	copy(out, b[4:4+n])
	return out, 4 + n, nil
}
