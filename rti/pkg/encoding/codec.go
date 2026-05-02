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
//
// NOTE for merge: the cases below are intentionally grouped by primitive
// family so that concurrent task branches (TASK-010 integers, TASK-011
// floats, TASK-012 strings, etc.) can extend this switch with minimal
// merge friction. Keep the float family contiguous.
func PrimitiveByName(name string) (Codec, error) {
	switch name {
	// --- HLAinteger family (TASK-010) ---
	case "HLAinteger16BE":
		return HLAinteger16BE{}, nil
	case "HLAinteger16LE":
		return HLAinteger16LE{}, nil
	case "HLAinteger32BE":
		return HLAinteger32BE{}, nil
	case "HLAinteger32LE":
		return HLAinteger32LE{}, nil
	case "HLAinteger64BE":
		return HLAinteger64BE{}, nil
	case "HLAinteger64LE":
		return HLAinteger64LE{}, nil
	// --- HLAfloat family (TASK-011) ---
	case "HLAfloat32BE":
		return hlaFloat32BE{}, nil
	case "HLAfloat32LE":
		return hlaFloat32LE{}, nil
	case "HLAfloat64BE":
		return hlaFloat64BE{}, nil
	case "HLAfloat64LE":
		return hlaFloat64LE{}, nil
	default:
		return nil, fmt.Errorf("encoding: PrimitiveByName(%q): unknown or unimplemented primitive type", name)
	}
}
