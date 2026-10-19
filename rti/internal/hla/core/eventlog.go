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

// EventLog is an optional observer for federation-crossing events. A nil
// EventLog selects the HLA core profile without audit or replay processing.
// Persistence adapters translate domain records to their storage format.
type EventLog interface {
	// Append observes one event before its state mutation is applied.
	Append(ctx context.Context, fed FederationName, evt EventRecord) error

	// Sync flushes the federation's event log to durable storage.
	Sync(ctx context.Context, fed FederationName) error

	// OpenReader returns a reader for the federation's stored event log.
	// path is implementation-defined (typically resolved from federation name + dir).
	OpenReader(ctx context.Context, path string) (EventLogReader, error)
}

// EventRecord is the persistence-neutral event written to the log.
type EventRecord interface {
	// Seq returns the monotonic sequence number assigned at append time.
	// Returns 0 before append; readers see the assigned value.
	Seq() uint64
}

// ObjectJournalKind identifies an object-management mutation in the optional
// audit log. It is independent of the persistence representation.
type ObjectJournalKind uint8

const (
	ObjectJournalRegistered ObjectJournalKind = iota + 1
	ObjectJournalAttributeUpdated
	ObjectJournalInteractionSent
	ObjectJournalDeleted
)

// ObjectJournalRecord is the narrow persistence DTO shared by the OM service
// and audit adapters. Maps and byte slices are immutable after Append.
type ObjectJournalRecord = ObjectJournalRecordOf[LogicalTime]

type ObjectJournalRecordOf[T LogicalTimeScalar] struct {
	Sequence    uint64
	Kind        ObjectJournalKind
	WallNS      uint64
	Object      ObjectHandle
	ObjectClass ObjectClassHandle
	Federate    FederateHandle
	ObjectName  string
	// RegionScoped marks a registration with region bindings. An adapter that
	// cannot preserve those bindings must reject it instead of recording an
	// ordinary registration. It is ignored for other journal kinds.
	RegionScoped     bool
	Attributes       map[uint64][]byte
	InteractionClass InteractionClassHandle
	Parameters       map[uint64][]byte
	LogicalTime      *T
}

func (e *ObjectJournalRecordOf[T]) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.Sequence
}

func (e *ObjectJournalRecordOf[T]) SetSeq(seq uint64) { e.Sequence = seq }

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
