package eventlog

import (
	"math"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	"google.golang.org/protobuf/proto"
)

func TestReviewGrantJournalLegacyFinalAndExactInteger(t *testing.T) {
	t.Run("legacy-final", func(t *testing.T) {
		record := &timepkg.TimeAdvanceGrantedRecord{Federate: 7, Time: core.LogicalTime(math.Inf(1))}
		record.SetSeq(19)
		message, err := wireMessageForRecord(record)
		if err != nil {
			t.Fatalf("legacy final grant could not be journaled: %v", err)
		}
		grant := message.(*rtiv1.Event).GetTimeGranted()
		if grant == nil || !math.IsInf(grant.LogicalTime, 1) || grant.LogicalTimeValue != nil {
			t.Fatalf("legacy final grant=%v", message)
		}
	})
	for _, value := range []int64{1<<53 + 1, 1<<53 + 2, math.MaxInt64} {
		record := &timepkg.IntegerTimeAdvanceGrantedRecord{Federate: 7, Time: value}
		record.SetSeq(23)
		message, err := wireMessageForRecord(record)
		if err != nil {
			t.Fatal(err)
		}
		data, err := proto.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		var decoded rtiv1.Event
		if err := proto.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		grant := decoded.GetTimeGranted()
		if decoded.Seq != 23 || grant.GetFederateHandle() != 7 || grant.LogicalTime != 0 || grant.GetLogicalTimeValue().GetInteger64Value() != value {
			t.Fatalf("journal roundtrip lost grant metadata/time: %v", &decoded)
		}
	}
}
