package object

import (
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestJournalWallBitsPreserveSignedNanoseconds(t *testing.T) {
	for _, tc := range []struct {
		wall int64
		bits uint64
	}{
		{math.MinInt64, 1 << 63},
		{math.MinInt64 + 1, 1<<63 | 1},
		{-1, math.MaxUint64},
		{0, 0},
		{1, 1},
		{math.MaxInt64, math.MaxInt64},
	} {
		records := []*core.ObjectJournalRecordOf[int64]{
			newObjectRegisteredEventOf[int64](1, 2, 3, "object", tc.wall),
			newAttributeUpdatedEventOf[int64](1, 3, nil, nil, tc.wall),
			newInteractionSentEventOf[int64](2, 3, nil, nil, tc.wall),
			newObjectDeletedEventOf[int64](1, nil, tc.wall),
		}
		for _, record := range records {
			if record.WallNS != tc.bits {
				t.Fatalf("kind %v, wall %d: bits=%x, want %x", record.Kind, tc.wall, record.WallNS, tc.bits)
			}
		}
	}
}

func TestOutboundSequenceRangePreservesEmptyAndWrappingRanges(t *testing.T) {
	for _, tc := range []struct {
		last, first, next uint64
		size              int
	}{
		{last: 7, size: 0, first: 8, next: 7},
		{last: 7, size: 1, first: 8, next: 8},
		{last: 7, size: 3, first: 8, next: 10},
		{last: math.MaxUint64, size: 0, first: 0, next: math.MaxUint64},
		{last: math.MaxUint64, size: 2, first: 0, next: 1},
	} {
		st := newFederationStateOf[int64]()
		st.nextOutboundSeq = tc.last
		if first := st.nextOutboundSeqRangeLocked(tc.size); first != tc.first || st.nextOutboundSeq != tc.next {
			t.Fatalf("last=%d size=%d: range=(%d,%d), want (%d,%d)", tc.last, tc.size, first, st.nextOutboundSeq, tc.first, tc.next)
		}
	}
}
