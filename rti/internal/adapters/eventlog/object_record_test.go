package eventlog

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestObjectJournalCodecPreservesFrozenFields(t *testing.T) {
	timestamp := core.LogicalTime(12.5)
	record := &core.ObjectJournalRecord{Sequence: 7, Kind: core.ObjectJournalAttributeUpdated, WallNS: 99, Object: 11, Federate: 3, Attributes: map[uint64][]byte{2: {0xab}}, LogicalTime: &timestamp}
	message, err := wireMessageForRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	event, ok := message.(*rtiv1.Event)
	if !ok {
		t.Fatalf("wire message type = %T", message)
	}
	updated := event.GetAttrUpdated()
	if event.GetSeq() != 7 || event.GetWallNs() != 99 || updated == nil {
		t.Fatalf("wire event = %+v", event)
	}
	if updated.GetObjectHandle() != 11 || updated.GetProducerFederateHandle() != 3 || updated.GetLogicalTime() != 12.5 || updated.GetAttributes()[2][0] != 0xab {
		t.Fatalf("attribute update = %+v", updated)
	}
}
