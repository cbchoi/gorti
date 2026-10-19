package object

import "github.com/cbchoi/gorti/rti/internal/hla/core"

type JournalKind = core.ObjectJournalKind

const (
	JournalObjectRegistered = core.ObjectJournalRegistered
	JournalAttributeUpdated = core.ObjectJournalAttributeUpdated
	JournalInteractionSent  = core.ObjectJournalInteractionSent
	JournalObjectDeleted    = core.ObjectJournalDeleted
)

// JournalRecord is the transport-neutral write-ahead record emitted by OM.
// Maps and byte slices are immutable after Append is called.
type JournalRecord = core.ObjectJournalRecord

type eventRecord = JournalRecord

func copiedLogicalTimeOf[T core.LogicalTimeScalar](ts *T) *T {
	if ts == nil {
		return nil
	}
	value := *ts
	return &value
}

func validateFiniteMessageTimestampOf[T core.LogicalTimeScalar](ts *T) error {
	if ts != nil {
		if _, err := core.ScalarTimeValue(*ts); err != nil {
			return core.ErrTimeInvalidLogicalTime
		}
	}
	return nil
}

// journalWallBits retains the journal schema's two's-complement representation,
// including pre-epoch clock values.
func journalWallBits(wallNS int64) uint64 {
	//nolint:gosec // G115: signed Unix nanoseconds are deliberately encoded as exact uint64 bits.
	return uint64(wallNS)
}
