package eventlog

import (
	"context"
	"io"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// Reader implements core.EventLogReader over an io.Reader, iterating events
// in stored (TSO) order. Never random-access.
type Reader struct {
	src io.Reader
	hdr core.EventLogHeader
	// internal state declared by Agent A in implementation
}

// NewReader constructs a Reader, reads + validates the file header, and
// positions the reader at the first event record.
//
// Returns:
//   - core.ErrWireMalformedMessage if the magic doesn't match Magic.
//   - core.ErrWireVersionMismatch if the version field exceeds Version.
//   - the underlying error if the read itself fails.
func NewReader(src io.Reader) (*Reader, error) {
	_ = src
	return nil, ErrNotImplemented
}

// Header implements core.EventLogReader.
func (r *Reader) Header() core.EventLogHeader {
	return r.hdr
}

// Next implements core.EventLogReader. Returns the next event record or
// (nil, io.EOF) at the clean end of the stream. On a truncated trailing
// record, returns (nil, io.ErrUnexpectedEOF).
func (r *Reader) Next(ctx context.Context) (core.EventRecord, error) {
	_ = ctx
	return nil, ErrNotImplemented
}

// Close releases the underlying reader (best-effort; if src isn't a
// Closer, this is a no-op returning nil).
func (r *Reader) Close() error {
	if c, ok := r.src.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// Compile-time assertion that Reader implements core.EventLogReader.
var _ core.EventLogReader = (*Reader)(nil)
