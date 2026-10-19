package federation

import (
	"math"
	"testing"
	"time"
)

func TestJournalWallNSPreservesSignedTimestampBits(t *testing.T) {
	for _, tc := range []struct {
		nanos int64
		want  uint64
	}{
		{-1, math.MaxUint64},
		{math.MinInt64, uint64(1) << 63},
		{0, 0},
		{math.MaxInt64, math.MaxInt64},
	} {
		if got := journalWallNS(time.Unix(0, tc.nanos)); got != tc.want {
			t.Fatalf("journal timestamp %d = %x; want %x", tc.nanos, got, tc.want)
		}
	}
}
