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
// The dt parameter is opaque at this contract layer; concrete dataType types
// live in rti/pkg/fom/model (Agent B).
func CodecFor(dt any) (Codec, error) {
	_ = dt
	return nil, ErrNotImplemented
}

// PrimitiveByName returns the Codec for an HLA Evolved primitive type by its
// canonical name (e.g. "HLAinteger32BE", "HLAfloat64BE", "HLAboolean",
// "HLAoctet", "HLAASCIIchar", "HLAunicodeChar"). Returns an error for unknown
// or composite types. Convenience for tests and bridges that work in name form.
func PrimitiveByName(name string) (Codec, error) {
	switch name {
	// --- BEGIN TASK-012 byte-level primitives (Agent B) -----------------
	case "HLAoctet":
		return HLAoctet{}, nil
	case "HLAoctetPairBE":
		return HLAoctetPairBE{}, nil
	case "HLAoctetPairLE":
		return HLAoctetPairLE{}, nil
	case "HLAboolean":
		return HLAboolean{}, nil
	case "HLAASCIIchar":
		return HLAASCIIchar{}, nil
	case "HLAunicodeChar":
		return HLAunicodeChar{}, nil
	// --- END TASK-012 byte-level primitives -----------------------------
	}
	return nil, fmt.Errorf("encoding: unknown primitive type %q", name)
}
