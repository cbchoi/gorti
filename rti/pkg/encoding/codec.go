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

// primitiveCodecs maps HLA Evolved primitive names to their Codec instances.
// Each task branch (TASK-010 integers, TASK-011 floats, TASK-012 byte/bool/char,
// TASK-013 strings, ...) appends its family in a clearly-delimited block so
// concurrent additions merge with minimal friction.
var primitiveCodecs = map[string]Codec{
	// --- HLAinteger family (TASK-010) ---
	"HLAinteger16BE": HLAinteger16BE{},
	"HLAinteger16LE": HLAinteger16LE{},
	"HLAinteger32BE": HLAinteger32BE{},
	"HLAinteger32LE": HLAinteger32LE{},
	"HLAinteger64BE": HLAinteger64BE{},
	"HLAinteger64LE": HLAinteger64LE{},

	// --- HLAfloat family (TASK-011) ---
	"HLAfloat32BE": hlaFloat32BE{},
	"HLAfloat32LE": hlaFloat32LE{},
	"HLAfloat64BE": hlaFloat64BE{},
	"HLAfloat64LE": hlaFloat64LE{},

	// --- HLAoctet / HLAboolean / HLAchar family (TASK-012) ---
	"HLAoctet":       HLAoctet{},
	"HLAoctetPairBE": HLAoctetPairBE{},
	"HLAoctetPairLE": HLAoctetPairLE{},
	"HLAboolean":     HLAboolean{},
	"HLAASCIIchar":   HLAASCIIchar{},
	"HLAunicodeChar": HLAunicodeChar{},
}

// PrimitiveByName returns the Codec for an HLA Evolved primitive type by its
// canonical name (e.g. "HLAinteger32BE", "HLAfloat64BE", "HLAboolean",
// "HLAoctet", "HLAASCIIchar", "HLAunicodeChar"). Returns an error for unknown
// or composite types. Convenience for tests and bridges that work in name form.
func PrimitiveByName(name string) (Codec, error) {
	if c, ok := primitiveCodecs[name]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("encoding: PrimitiveByName(%q): unknown or unimplemented primitive type", name)
}
