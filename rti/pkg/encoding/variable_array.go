package encoding

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// HLAvariableArray is the composite codec for arrays of dynamic cardinality
// per IEEE 1516.2-2010 §4. Construct one with NewVariableArray.
//
// Wire layout: 4-byte big-endian element count, then N concatenated element
// encodings with element-boundary padding between them. The composite's
// OctetBoundary is max(4, elem.OctetBoundary()) — the length prefix forces
// at least 4-byte alignment regardless of the element type.

// variableArrayCodec is the unexported value behind NewVariableArray.
type variableArrayCodec struct {
	elem Codec
}

// NewVariableArray returns a Codec for an HLAvariableArray with element
// codec elem. Panics if elem is nil.
func NewVariableArray(elem Codec) Codec {
	if elem == nil {
		panic("encoding: NewVariableArray: elem is nil")
	}
	return variableArrayCodec{elem: elem}
}

// OctetBoundary returns max(4, element boundary).
func (c variableArrayCodec) OctetBoundary() int {
	eb := c.elem.OctetBoundary()
	if eb < 4 {
		return 4
	}
	return eb
}

// Encode emits the 4-byte length prefix, then each element encoded with
// inter-element padding to the element boundary.
func (c variableArrayCodec) Encode(v any) ([]byte, error) {
	xs, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("encoding: HLAvariableArray cannot encode %T (want []any)", v)
	}
	out := make([]byte, 4, 4+len(xs)*c.elem.OctetBoundary())
	binary.BigEndian.PutUint32(out[:4], uint32(len(xs)))
	for i, x := range xs {
		out = padTo(out, c.elem.OctetBoundary())
		b, err := c.elem.Encode(x)
		if err != nil {
			return nil, fmt.Errorf("encoding: HLAvariableArray element %d: %w", i, err)
		}
		out = append(out, b...)
	}
	return out, nil
}

// Decode reads the length prefix, then consumes that many element decodes
// with appropriate alignment.
func (c variableArrayCodec) Decode(b []byte) (any, int, error) {
	if len(b) < 4 {
		return nil, 0, errors.New("encoding: HLAvariableArray decode short buffer (length prefix)")
	}
	n := int(binary.BigEndian.Uint32(b[:4]))
	if n < 0 {
		return nil, 0, fmt.Errorf("encoding: HLAvariableArray decode invalid length %d", n)
	}
	out := make([]any, 0, n)
	pos := 4
	for i := 0; i < n; i++ {
		pos = alignTo(pos, c.elem.OctetBoundary())
		if pos > len(b) {
			return nil, 0, errors.New("encoding: HLAvariableArray decode short buffer (alignment)")
		}
		v, m, err := c.elem.Decode(b[pos:])
		if err != nil {
			return nil, 0, fmt.Errorf("encoding: HLAvariableArray element %d: %w", i, err)
		}
		out = append(out, v)
		pos += m
	}
	return out, pos, nil
}
