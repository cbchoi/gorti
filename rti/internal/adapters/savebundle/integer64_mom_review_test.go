package savebundle

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/proto"
)

func TestReviewInteger64MOMRequiresInitialExactValuesForIdleMembers(t *testing.T) {
	ctx := context.Background()
	codec := completeMOMCodec(t)
	if err := codec.Manager.FederateJoined(ctx, "fed", 8, "idle", "model"); err != nil {
		t.Fatal(err)
	}
	current, _ := logicaltime.Integer64(1<<53 + 1)
	ahead, _ := logicaltime.Integer64Interval(1)
	if err := codec.Manager.TimeStateChangedValue(ctx, "fed", 7, true, true, ahead, current); err != nil {
		t.Fatal(err)
	}
	// The legacy join hook does not know the time representation. An Integer64
	// assembly must supply the idle member's exact initial pair before saving.
	if _, err := codec.Marshal("fed"); err == nil {
		t.Fatal("missing idle-member exact values silently adopted Float64 zero")
	}
	zero, _ := logicaltime.Integer64(0)
	zeroInterval, _ := logicaltime.Integer64Interval(0)
	if err := codec.Manager.TimeStateChangedValue(ctx, "fed", 8, false, false, zeroInterval, zero); err != nil {
		t.Fatal(err)
	}
	want := codec.Manager.ExportState("fed")
	data, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if err := codec.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if got := codec.Manager.ExportState("fed"); !reflect.DeepEqual(got, want) {
		t.Fatal("active and idle Integer64 MOM members did not roundtrip exactly")
	}
}

func TestReviewMOMExactOnlyFloatWireNormalizesLegacyView(t *testing.T) {
	codec := completeMOMCodec(t)
	wire := &rtiv1.MOMManagerState{
		Federation: &rtiv1.MOMFederationRecord{Name: "fed", FederateHandles: []uint64{7}},
		Federates: []*rtiv1.MOMFederateRecord{{Handle: 7, Name: "alpha", FederateType: "model",
			LogicalTimeValue: &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: 3}},
			LookaheadValue:   &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: 2}},
		}},
	}
	// Pair decoding permits omitted legacy doubles, whose protobuf value is zero.
	// The imported in-memory Float64 mirror must use the decoded exact value.
	if _, _, err := decodeMOMTimeRecord(wire.Federates[0], logicaltime.HLAfloat64Time); err != nil {
		t.Fatal(err)
	}
	base, err := proto.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := json.Marshal(momSnapshotEnvelope{Version: 1, LegacyProtoBytes: base, Runtime: &momSnapshotRuntime{Federates: []momFederateRuntime{{Handle: 7}}}})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := codec.PrepareRestore("fed", append([]byte(momSnapshotMagic), envelope...))
	if err != nil {
		t.Fatalf("valid exact-only Float64 record did not normalize: %v", err)
	}
	commit()
	attrs, _ := codec.Manager.QueryFederate("fed", 7)
	if attrs.LogicalTime != 3 || attrs.Lookahead != 2 || attrs.LogicalTimeValue == nil || attrs.LookaheadValue == nil {
		t.Fatalf("Float64 restored views disagree: %+v", attrs)
	}
}
