// Package encoding implements HLA Evolved encoding rules per IEEE 1516.2-2010 §4.
//
// IMPORTANT (Agent B): the public API surface declared here — Codec and
// CodecFor — is part of the M0 contract. Bodies are stubs; fill them in
// test-first per docs/TDD.md. Signatures should not change without a
// contract-change-request.
package encoding

import (
	"errors"
	"fmt"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// ErrNotImplemented is returned by stub functions until Agent B implements them.
var ErrNotImplemented = errors.New("encoding: not implemented (Agent B M1 deliverable)")

// Codec is one HLA Evolved type encoder/decoder.
type Codec interface {
	Encode(v any) ([]byte, error)
	Decode(b []byte) (v any, n int, err error)
	OctetBoundary() int
}

// CodecFor returns the Codec for the given FOM data type descriptor.
// The dt parameter is opaque at this contract layer; the model package's
// DataType sum (rti/pkg/fom/model) is the concrete shape callers pass.
func CodecFor(dt any) (Codec, error) {
	switch t := dt.(type) {
	case nil:
		return nil, fmt.Errorf("encoding: CodecFor(nil)")
	case string:
		return primitiveByName(t)
	case model.DataType:
		return codecForModelType(t)
	}
	return nil, fmt.Errorf("encoding: CodecFor cannot dispatch %T", dt)
}

// PrimitiveByName returns the Codec for an HLA Evolved primitive type by its
// canonical name (e.g. "HLAinteger32BE", "HLAfloat64BE", "HLAboolean",
// "HLAoctet", "HLAASCIIchar", "HLAunicodeChar"). Returns an error for unknown
// or composite types. Convenience for tests and bridges that work in name form.
func PrimitiveByName(name string) (Codec, error) {
	return primitiveByName(name)
}
