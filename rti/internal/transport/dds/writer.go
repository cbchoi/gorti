//go:build dds

// Package dds — Writer interface + Phase 1a stub.

package dds

import (
	"errors"
)

// Writer is the gorti-side handle for a Cyclone DDS DataWriter. The
// payload bytes are opaque (per docs/m19-dds-adapter.md §6.6 PINNED:
// handle-based topic names + proto-bytes payload), so Write takes a
// raw []byte and the federation runtime is responsible for the
// proto.Marshal at the producer side and proto.Unmarshal at the
// consumer side.
//
// Phase 1a stub returns errors.ErrUnsupported on Write/Close. Phase
// 1b drives Cyclone DDS dds_write + sample-info plumbing.
type Writer interface {
	// Write publishes a single sample. Returns ErrUnsupported in
	// Phase 1a; in Phase 1b returns a wrapped Cyclone DDS error
	// when the underlying dds_write fails (e.g. resource limits
	// exhausted on a RELIABLE writer).
	Write(payload []byte) error

	// Close releases the writer. Idempotent. Phase 1a stub returns
	// ErrUnsupported on first call.
	Close() error
}

// defaultWriter is the Phase 1a stub.
type defaultWriter struct{}

func (*defaultWriter) Write([]byte) error { return errors.ErrUnsupported }
func (*defaultWriter) Close() error       { return errors.ErrUnsupported }
