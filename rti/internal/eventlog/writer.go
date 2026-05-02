package eventlog

import (
	"context"
	"errors"
	"io"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrNotImplemented is returned by stub methods until Agent A implements them.
var ErrNotImplemented = errors.New("eventlog: not implemented (Agent A M2 deliverable)")

// WriterOptions bundles Writer dependencies. Tests typically pass a
// bytes.Buffer for Sink; production passes an *os.File opened for write.
type WriterOptions struct {
	// Sink receives the binary log stream. MUST NOT be nil.
	Sink io.Writer

	// Federation is recorded in the header and used in EventLog.Append's
	// federation argument as a sanity check.
	Federation core.FederationName

	// Mode is recorded in the header so replay can validate the
	// federation's mode matches.
	Mode core.Mode

	// Seed is recorded in the header for deterministic replay seeding.
	Seed uint64

	// Clock provides CreatedAtNs at file open. MUST NOT be nil.
	Clock core.Clock
}

// Writer implements core.EventLog's write side over a single io.Writer.
// One Writer per federation; do not share across federations.
type Writer struct {
	opts WriterOptions
	// internal state declared by Agent A in implementation
}

// NewWriter constructs a Writer, writes the file header to opts.Sink, and
// returns the Writer ready to Append. Returns an error if any required
// field of opts is missing or the header write fails.
func NewWriter(opts WriterOptions) (*Writer, error) {
	return &Writer{opts: opts}, ErrNotImplemented
}

// Append implements core.EventLog. Writes one length-prefixed Event record
// to the sink and assigns evt.Seq() the next monotonic sequence number.
// fed argument MUST equal the Writer's federation; mismatch returns an
// error (defensive — multi-federation Writers are not supported).
//
// Write-ahead contract: Append MUST return successfully BEFORE the caller
// applies any state mutation. This is the determinism guarantee — replay
// reproduces exactly what was logged.
func (w *Writer) Append(ctx context.Context, fed core.FederationName, evt core.EventRecord) error {
	_ = ctx
	_ = fed
	_ = evt
	return ErrNotImplemented
}

// Sync implements core.EventLog. Flushes the federation's log to durable
// storage (no-op for buffer sinks; calls Sync on *os.File otherwise).
func (w *Writer) Sync(ctx context.Context, fed core.FederationName) error {
	_ = ctx
	_ = fed
	return ErrNotImplemented
}

// OpenReader implements core.EventLog. For a Writer, this resolves to the
// reader interface over the same federation's persisted file. Tests
// typically construct a Reader directly off the same buffer instead.
func (w *Writer) OpenReader(ctx context.Context, path string) (core.EventLogReader, error) {
	_ = ctx
	_ = path
	return nil, ErrNotImplemented
}

// Close finalizes the file (no-op for buffer sinks; closes *os.File
// otherwise). After Close, further Append calls return an error.
func (w *Writer) Close() error {
	return ErrNotImplemented
}

// Compile-time assertion that Writer implements core.EventLog.
var _ core.EventLog = (*Writer)(nil)
