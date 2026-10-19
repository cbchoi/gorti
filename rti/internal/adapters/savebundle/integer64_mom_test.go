package savebundle

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestInteger64MOMTimeRecordExactPair(t *testing.T) {
	value := int64(1<<53 + 7)
	entry := &rtiv1.MOMFederateRecord{LogicalTimeValue: &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: value}}, LookaheadValue: &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Integer64Value{Integer64Value: 3}}}
	time, lookahead, err := decodeMOMTimeRecord(entry, logicaltime.HLAinteger64Time)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := time.Integer64()
	ahead, _ := lookahead.Integer64()
	if got != value || ahead != 3 {
		t.Fatal("MOM values rounded")
	}
	for _, bad := range []*rtiv1.MOMFederateRecord{{LogicalTimeValue: entry.LogicalTimeValue}, {LookaheadValue: entry.LookaheadValue}, {LogicalTime: 1, LogicalTimeValue: entry.LogicalTimeValue, LookaheadValue: entry.LookaheadValue}, {LogicalTimeValue: &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: 1}}, LookaheadValue: entry.LookaheadValue}} {
		if _, _, err := decodeMOMTimeRecord(bad, logicaltime.HLAinteger64Time); err == nil {
			t.Fatalf("invalid MOM exact pair accepted: %v", bad)
		}
	}
	if _, _, err := decodeMOMTimeRecord(entry, logicaltime.HLAfloat64Time); err == nil {
		t.Fatal("integer MOM record restored into float execution")
	}
}

func TestInteger64MOMSnapshotRoundtripAndRebind(t *testing.T) {
	codec := completeMOMCodec(t)
	state := codec.Manager.ExportState("fed")
	value, _ := logicaltime.Integer64(1<<53 + 7)
	lookahead, _ := logicaltime.Integer64Interval(3)
	state.Federates[0].LogicalTime = 0
	state.Federates[0].Lookahead = 0
	state.Federates[0].LogicalTimeValue = &value
	state.Federates[0].LookaheadValue = &lookahead
	codec.Manager.ImportState("fed", state)
	data, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"logical_time_implementation_name":"HLAinteger64Time"`)) {
		t.Fatal("missing integer marker")
	}
	if err := codec.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(codec.Manager.ExportState("fed"), state) {
		t.Fatal("MOM exact state lost")
	}
	rebound, err := codec.RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) { return h + 100, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := codec.Unmarshal("fed", rebound); err != nil {
		t.Fatal(err)
	}
	got := codec.Manager.ExportState("fed")
	integer, _ := got.Federates[0].LogicalTimeValue.Integer64()
	if integer != 1<<53+7 || got.Federates[0].Handle != 107 || got.Federates[0].LogicalTime != 0 {
		t.Fatal("MOM rebind changed exact time")
	}
}

func TestInteger64MOMRejectsRepeatedOneofBeforeUnmarshal(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		var exact []byte
		if mixed {
			exact = protowire.AppendTag(exact, 1, protowire.Fixed64Type)
			exact = protowire.AppendFixed64(exact, math.Float64bits(1))
		} else {
			exact = protowire.AppendTag(exact, 2, protowire.VarintType)
			exact = protowire.AppendVarint(exact, 2)
		}
		exact = protowire.AppendTag(exact, 2, protowire.VarintType)
		exact = protowire.AppendVarint(exact, protowire.EncodeZigZag(1<<53+7))
		var record []byte
		record = protowire.AppendTag(record, 15, protowire.BytesType)
		record = protowire.AppendBytes(record, exact)
		interval := protowire.AppendTag(nil, 2, protowire.VarintType)
		interval = protowire.AppendVarint(interval, 6)
		record = protowire.AppendTag(record, 14, protowire.BytesType)
		record = protowire.AppendBytes(record, interval)
		base := protowire.AppendTag(nil, 2, protowire.BytesType)
		base = protowire.AppendBytes(base, record)
		envelope, err := json.Marshal(momSnapshotEnvelope{Version: 2, LogicalTimeImplementationName: "HLAinteger64Time", LegacyProtoBytes: base, Runtime: &momSnapshotRuntime{}})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := decodeMOMGraph(append([]byte(momSnapshotMagic), envelope...)); err == nil {
			t.Fatal("repeated exact selector accepted after protobuf last-one-wins")
		}
	}
}
