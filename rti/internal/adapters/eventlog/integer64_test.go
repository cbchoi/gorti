package eventlog

import (
	"bytes"
	"context"
	"math"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	"google.golang.org/protobuf/proto"
)

type exactReplayObjects struct {
	core.ObjectRegistryOf[int64]
	seen []int64
}

func (o *exactReplayObjects) UpdateAttributes(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ map[core.AttributeHandle][]byte, value *int64) error {
	o.seen = append(o.seen, *value)
	return nil
}
func (o *exactReplayObjects) SendInteraction(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, _ map[core.ParameterHandle][]byte, value *int64) error {
	o.seen = append(o.seen, *value)
	return nil
}

func TestInteger64JournalAndReplayTimestampRemainExact(t *testing.T) {
	for _, target := range []int64{1<<53 + 1, math.MaxInt64} {
		for _, kind := range []core.ObjectJournalKind{core.ObjectJournalAttributeUpdated, core.ObjectJournalInteractionSent, core.ObjectJournalDeleted} {
			message, err := wireMessageForRecord(&core.ObjectJournalRecordOf[int64]{Kind: kind, Object: 2, InteractionClass: 3, Federate: 1, LogicalTime: &target})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := proto.Marshal(message)
			if err != nil {
				t.Fatal(err)
			}
			var decoded rtiv1.Event
			if err = proto.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			var legacy *float64
			var exact *rtiv1.LogicalTimeValue
			switch kind {
			case core.ObjectJournalAttributeUpdated:
				legacy = decoded.GetAttrUpdated().LogicalTime
				exact = decoded.GetAttrUpdated().LogicalTimeValue
			case core.ObjectJournalInteractionSent:
				legacy = decoded.GetInterSent().LogicalTime
				exact = decoded.GetInterSent().LogicalTimeValue
			case core.ObjectJournalDeleted:
				legacy = decoded.GetObjDeleted().LogicalTime
				exact = decoded.GetObjDeleted().LogicalTimeValue
			}
			if legacy != nil || exact == nil || exact.GetInteger64Value() != target {
				t.Fatalf("journal=%v", &decoded)
			}
			objects := &exactReplayObjects{}
			var sink bytes.Buffer
			replay := &ReplayerOf[int64]{objects: objects, capturingSink: newWriterForTest(t, "integer", &sink)}
			if err = replay.dispatchProtoEvent(context.Background(), &decoded); err != nil {
				t.Fatal(err)
			}
			if kind != core.ObjectJournalDeleted && (len(objects.seen) != 1 || objects.seen[0] != target) {
				t.Fatal("replay narrowed timestamp")
			}
		}
		message, err := wireMessageForRecord(&timepkg.TimeAdvanceGrantedRecordOf[int64]{Federate: 1, Time: target})
		if err != nil {
			t.Fatal(err)
		}
		grant := message.(*rtiv1.Event).GetTimeGranted()
		if grant == nil || grant.LogicalTime != 0 || grant.GetLogicalTimeValue().GetInteger64Value() != target {
			t.Fatal("grant journal lost event body or precision")
		}
	}
}

func TestLegacyReplayRejectsIntegerTimestampBeforeObjectMutation(t *testing.T) {
	value := int64(1<<53 + 1)
	exact := &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: value}}
	if _, err := decodeReplayTimestamp[core.LogicalTime](nil, exact); err == nil {
		t.Fatal("Float64 replay accepted Integer64 record")
	}
	zero := 0.0
	if _, err := decodeReplayTimestamp[int64](&zero, exact); err == nil {
		t.Fatal("Integer64 replay accepted a conflicting optional legacy field")
	}
}
