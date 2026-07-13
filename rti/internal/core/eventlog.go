package core

import (
	"context"
	"io"
)

// OpenMode selects writer or replayer.
type OpenMode uint8

const (
	OpenWrite OpenMode = iota
	OpenReplay
)

// EventLog persists every federation-crossing event in TSO. It is the
// determinism guarantee — replay reads from this log to reproduce a run.
//
// Concrete event payloads are defined in proto/rti/v1/eventlog.proto.
type EventLog interface {
	// Append writes one event. Implementations write-ahead: caller MUST append
	// before applying state mutation, so replay reproduces the mutation order.
	Append(ctx context.Context, fed FederationName, evt EventRecord) error

	// Sync flushes the federation's event log to durable storage.
	Sync(ctx context.Context, fed FederationName) error

	// OpenReader returns a reader for the federation's stored event log.
	// path is implementation-defined (typically resolved from federation name + dir).
	OpenReader(ctx context.Context, path string) (EventLogReader, error)
}

// EventRecord is the abstract event written to the log. The concrete type is
// the generated rtiv1.Event Protobuf message; this interface keeps `core`
// free of generated-package imports. Implementations type-assert internally.
type EventRecord interface {
	// Seq returns the monotonic sequence number assigned at append time.
	// Returns 0 before append; readers see the assigned value.
	Seq() uint64
}

// EventLogReader iterates events in stored (TSO) order. Never random-access.
type EventLogReader interface {
	io.Closer
	// Header returns metadata read from the file header.
	Header() EventLogHeader
	// Next returns the next event, or (nil, io.EOF) when exhausted.
	// Returns io.ErrUnexpectedEOF on truncated trailing record.
	Next(ctx context.Context) (EventRecord, error)
}

// EventLogHeader summarizes file-level metadata.
type EventLogHeader struct {
	Magic      [8]byte
	Version    uint32
	Federation FederationName
	// CreatedAtNs is populated only when decoding legacy version-1 logs.
	CreatedAtNs uint64
	// Generation identifies the federation execution in version-2 logs.
	Generation uint64
	Seed       uint64
	Mode       Mode
}
