package savebundle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/protobuf/proto"
)

func trafficMOMCodec(t *testing.T) (MOM, *mom.TrafficAccumulator) {
	t.Helper()
	m, err := mom.New(mom.Options{Outbox: momCompleteOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	a := mom.NewTrafficAccumulator()
	if err := m.SetTrafficAccumulator(a); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateJoined(context.Background(), "fed", 0x100000007, "seven", "test"); err != nil {
		t.Fatal(err)
	}
	if err := a.Record("fed", 0x100000007, mom.UpdatesSent, 0x10000000b, core.TransportTypeReliable); err != nil {
		t.Fatal(err)
	}
	m.IncrementUpdatesSent("fed", 0x100000007)
	return MOM{Manager: m}, a
}

func TestMOMTrafficCodecRoundTripPrepared(t *testing.T) {
	codec, a := trafficMOMCodec(t)
	want := codec.Manager.ExportState("fed")
	data, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	again, err := codec.Marshal("fed")
	if err != nil || !bytes.Equal(data, again) {
		t.Fatal("traffic snapshot not deterministic")
	}
	if err := a.Record("fed", 0x100000007, mom.UpdatesSent, 0x10000000b, core.TransportTypeReliable); err != nil {
		t.Fatal(err)
	}
	before := codec.Manager.ExportState("fed")
	commit, err := codec.PrepareRestore("fed", data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(codec.Manager.ExportState("fed"), before) {
		t.Fatal("prepare changed live traffic")
	}
	clear(data)
	commit()
	if !reflect.DeepEqual(codec.Manager.ExportState("fed"), want) {
		t.Fatalf("traffic lost on durable round trip: %+v", codec.Manager.ExportState("fed").Traffic)
	}
}

func TestMOMTrafficCodecMalformedStateNoMutation(t *testing.T) {
	for name, mutate := range map[string]func(*mom.TrafficState){
		"wrongVersion":    func(s *mom.TrafficState) { s.Version = 3 },
		"wrongFederation": func(s *mom.TrafficState) { s.Federation = "other" },
		"wrongRoster":     func(s *mom.TrafficState) { s.Federates[0].Handle = 9 },
		"invalidCount":    func(s *mom.TrafficState) { s.Federates[0].Counts[0].Count = math.MaxUint32 },
		"duplicateCount": func(s *mom.TrafficState) {
			s.Federates[0].Counts = append(s.Federates[0].Counts, s.Federates[0].Counts[0])
		},
	} {
		t.Run(name, func(t *testing.T) {
			codec, _ := trafficMOMCodec(t)
			before := codec.Manager.ExportState("fed")
			data, err := codec.Marshal("fed")
			if err != nil {
				t.Fatal(err)
			}
			var envelope map[string]json.RawMessage
			if err := json.Unmarshal(data[len(momSnapshotMagic):], &envelope); err != nil {
				t.Fatal(err)
			}
			var runtime map[string]json.RawMessage
			if err := json.Unmarshal(envelope["runtime"], &runtime); err != nil {
				t.Fatal(err)
			}
			invalid := codec.Manager.ExportState("fed").Traffic
			mutate(invalid)
			runtime["traffic"], err = json.Marshal(invalid)
			if err != nil {
				t.Fatal(err)
			}
			envelope["runtime"], err = json.Marshal(runtime)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := codec.PrepareRestore("fed", append([]byte(momSnapshotMagic), payload...)); err == nil {
				t.Fatal("invalid history accepted")
			}
			if !reflect.DeepEqual(codec.Manager.ExportState("fed"), before) {
				t.Fatal("invalid history partially changed state")
			}
		})
	}
}

func TestMOMTrafficCodecReliableV1AndPartitionedV2(t *testing.T) {
	for _, bestEffort := range []bool{false, true} {
		name := "reliable-v1"
		if bestEffort {
			name = "partitioned-v2"
		}
		t.Run(name, func(t *testing.T) {
			codec, traffic := trafficMOMCodec(t)
			if bestEffort {
				for i := 0; i < 2; i++ {
					if err := traffic.Record("fed", 0x100000007, mom.UpdatesSent, 0x10000000b, core.TransportTypeBestEffort); err != nil {
						t.Fatal(err)
					}
				}
			}
			want := codec.Manager.ExportState("fed")
			version := uint32(1)
			if bestEffort {
				version = 2
			}
			if want.Traffic.Version != version {
				t.Fatalf("traffic version=%d want=%d", want.Traffic.Version, version)
			}
			data, err := codec.Marshal("fed")
			if err != nil {
				t.Fatal(err)
			}
			if err := codec.Unmarshal("fed", data); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(want, codec.Manager.ExportState("fed")) {
				t.Fatal("codec changed partitioned state")
			}
			rows, err := traffic.Snapshot(context.Background(), "fed", 0x100000007, mom.UpdatesSent)
			if err != nil || rows[0].Transportation != "HLAreliable" || rows[0].Counts[0x10000000b] != 1 {
				t.Fatalf("reliable counts=%+v error=%v", rows, err)
			}
			if bestEffort && (len(rows) != 2 || rows[1].Transportation != "HLAbestEffort" || rows[1].Counts[0x10000000b] != 2) {
				t.Fatal("best-effort partition lost", rows)
			}
		})
	}
}

func TestMOMTrafficCodecLegacyMissingBaseline(t *testing.T) {
	codec, a := trafficMOMCodec(t)
	legacy, err := proto.Marshal(&rtiv1.MOMManagerState{
		Federation: &rtiv1.MOMFederationRecord{Name: "fed", FederateHandles: []uint64{0x100000007}},
		Federates:  []*rtiv1.MOMFederateRecord{{Handle: 0x100000007, UpdatesSent: 19}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := codec.Unmarshal("fed", legacy); err != nil {
		t.Fatal(err)
	}
	attrs, _ := codec.Manager.QueryFederateAttributes("fed", 0x100000007)
	if attrs.UpdatesSent != 19 {
		t.Fatal("legacy aggregate lost")
	}
	if _, err := a.Snapshot(context.Background(), "fed", 0x100000007, mom.UpdatesSent); !errors.Is(err, mom.ErrCounterDataUnavailable) {
		t.Fatalf("legacy baseline invented: %v", err)
	}
	data, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if err := codec.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Snapshot(context.Background(), "fed", 0x100000007, mom.UpdatesSent); !errors.Is(err, mom.ErrCounterDataUnavailable) {
		t.Fatalf("round trip lost unavailable state: %v", err)
	}
}

func TestMOMTrafficCodecOverflowAndClear(t *testing.T) {
	codec, a := trafficMOMCodec(t)
	state := codec.Manager.ExportState("fed")
	state.Traffic.Federates[0].Counts[0].Count = math.MaxInt32
	codec.Manager.ImportState("fed", state)
	if err := a.Record("fed", 0x100000007, mom.UpdatesSent, 0x10000000b, core.TransportTypeReliable); !errors.Is(err, mom.ErrCounterOverflow) {
		t.Fatal(err)
	}
	data, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if err := codec.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Snapshot(context.Background(), "fed", 0x100000007, mom.UpdatesSent); !errors.Is(err, mom.ErrCounterOverflow) {
		t.Fatalf("overflow marker lost: %v", err)
	}
	commit, err := codec.PrepareRestore("fed", nil)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	if codec.Manager.ExportState("fed") != nil {
		t.Fatal("MOM not cleared")
	}
	if _, err := a.Snapshot(context.Background(), "fed", 0x100000007, mom.UpdatesSent); !errors.Is(err, core.ErrFederateNotJoined) {
		t.Fatalf("traffic not cleared: %v", err)
	}
}
