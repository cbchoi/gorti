//go:build dds

// Package dds — Reader interface + Phase 1a stub.

package dds

import (
	"errors"
)

// Sample is one delivered DDS sample. The payload bytes carry the
// proto-encoded ReceiveInteraction / ReflectAttributeValues body
// (per docs/m19-dds-adapter.md §6.6 PINNED). SourceTimestampNS is
// the writer-stamped timestamp the federation runtime uses to
// preserve HLA logical-time order — only meaningful when the topic's
// QoS is DestinationOrderBySourceTimestamp.
type Sample struct {
	Payload           []byte
	SourceTimestampNS int64
}

// Reader is the gorti-side handle for a Cyclone DDS DataReader. The
// federation runtime drives a take-loop in a dedicated goroutine.
//
// Phase 1a stub returns errors.ErrUnsupported on Take/Close.
type Reader interface {
	// Take returns up to maxSamples samples from the reader's
	// queue. Returns an empty slice + nil when no samples are
	// available; the caller blocks on a separate condition
	// variable / select. Phase 1a stub returns ErrUnsupported.
	Take(maxSamples int) ([]Sample, error)

	// Close releases the reader. Idempotent. Phase 1a stub returns
	// ErrUnsupported on first call.
	Close() error
}

// defaultReader is the Phase 1a stub.
type defaultReader struct{}

func (*defaultReader) Take(int) ([]Sample, error) { return nil, errors.ErrUnsupported }
func (*defaultReader) Close() error               { return errors.ErrUnsupported }
