package encoding

import (
	"errors"
	"fmt"
)

// HLAfixedArray is the composite codec for arrays of a fixed cardinality
// per IEEE 1516.2-2010 §4. Construct one with NewFixedArray; the resulting
// Codec is immutable.
//
// Wire layout: N copies of the element encoding, no leading length prefix.
// Inter-element padding equals the element's OctetBoundary; for primitive
// elements whose encoded width already matches their boundary (e.g.
// HLAinteger32BE, 4 bytes wide / 4-byte boundary) no padding is emitted.
//
// The composite's OctetBoundary equals the element's OctetBoundary.
//
// Composite codecs are not registered in primitiveCodecs because their
// configuration is dynamic (element type + cardinality). TASK-019's
// CodecFor(model.DataType) will build them from FOM descriptors.

// fixedArrayCodec is the unexported value behind NewFixedArray.
type fixedArrayCodec struct {
	elem Codec
	n    int
}

// NewFixedArray returns a Codec for an HLAfixedArray of cardinality n with
// element codec elem. Panics if elem is nil or n is negative; both are
// invariant violations callers must avoid at construction time, not
// recover from at runtime.
func NewFixedArray(elem Codec, n int) Codec {
	if elem == nil {
		panic("encoding: NewFixedArray: elem is nil")
	}
	if n < 0 {
		panic(fmt.Sprintf("encoding: NewFixedArray: negative cardinality %d", n))
	}
	return fixedArrayCodec{elem: elem, n: n}
}

// OctetBoundary inherits from the element type.
func (c fixedArrayCodec) OctetBoundary() int { return c.elem.OctetBoundary() }

// Encode accepts a []any of length exactly c.n; it dispatches each element
// through the element codec and concatenates the results with element
// padding applied between members.
func (c fixedArrayCodec) Encode(v any) ([]byte, error) {
	xs, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("encoding: HLAfixedArray cannot encode %T (want []any)", v)
	}
	if len(xs) != c.n {
		return nil, fmt.Errorf("encoding: HLAfixedArray expected %d elements, got %d", c.n, len(xs))
	}
	out := make([]byte, 0, c.n*c.elem.OctetBoundary())
	for i, x := range xs {
		out = padTo(out, c.elem.OctetBoundary())
		b, err := c.elem.Encode(x)
		if err != nil {
			return nil, fmt.Errorf("encoding: HLAfixedArray element %d: %w", i, err)
		}
		out = append(out, b...)
	}
	return out, nil
}

// Decode reads c.n elements; returns the slice and total bytes consumed.
func (c fixedArrayCodec) Decode(b []byte) (any, int, error) {
	out := make([]any, 0, c.n)
	pos := 0
	for i := 0; i < c.n; i++ {
		pos = alignTo(pos, c.elem.OctetBoundary())
		if pos > len(b) {
			return nil, 0, errors.New("encoding: HLAfixedArray decode short buffer (alignment)")
		}
		v, n, err := c.elem.Decode(b[pos:])
		if err != nil {
			return nil, 0, fmt.Errorf("encoding: HLAfixedArray element %d: %w", i, err)
		}
		out = append(out, v)
		pos += n
	}
	return out, pos, nil
}

// padTo right-pads the byte slice to a multiple of boundary, with zero
// fill. Used between composite members where the next member must start
// on its own boundary.
func padTo(b []byte, boundary int) []byte {
	if boundary <= 1 {
		return b
	}
	rem := len(b) % boundary
	if rem == 0 {
		return b
	}
	pad := boundary - rem
	for i := 0; i < pad; i++ {
		b = append(b, 0)
	}
	return b
}

// alignTo returns pos rounded up to the nearest multiple of boundary.
// Inverse of padTo for the decoder side.
func alignTo(pos, boundary int) int {
	if boundary <= 1 {
		return pos
	}
	rem := pos % boundary
	if rem == 0 {
		return pos
	}
	return pos + (boundary - rem)
}
