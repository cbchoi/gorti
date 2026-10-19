package savebundle

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func TestMOMCatalogCodecHistoryAndSerial(t *testing.T) {
	codec, traffic := trafficMOMCodec(t)
	for _, obj := range []uint64{1, 1, 2} {
		if err := traffic.RecordObjectTraffic("fed", 0x100000007, core.ObjectHandle(obj), 0x10000000b, false); err != nil {
			t.Fatal(err)
		}
	}
	state := codec.Manager.ExportState("fed")
	state.Federates[0].ServiceReports = &mom.ServiceReportState{NextSerial: 19}
	commit, err := codec.Manager.PrepareImportState("fed", state)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	want := codec.Manager.ExportState("fed")
	data, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if err := traffic.RecordObjectTraffic("fed", 0x100000007, 3, 0x10000000b, false); err != nil {
		t.Fatal(err)
	}
	commit, err = codec.PrepareRestore("fed", data)
	if err != nil {
		t.Fatal(err)
	}
	clear(data)
	commit()
	commit()
	if !reflect.DeepEqual(want, codec.Manager.ExportState("fed")) {
		t.Fatal("catalog state lost during durable prepared restore")
	}
	counts, err := traffic.ObjectInstanceCounts(context.Background(), "fed", 0x100000007, mom.ObjectsUpdated)
	if err != nil || counts[0x10000000b] != 2 {
		t.Fatal("distinct history", counts, err)
	}
}

func TestMOMCatalogCodecRejectsMalformedBeforeMutation(t *testing.T) {
	for _, which := range []string{"duplicateObject", "conflictingClass", "negativePeriod", "serialOverflow"} {
		t.Run(which, func(t *testing.T) {
			codec, traffic := trafficMOMCodec(t)
			if err := traffic.RecordObjectTraffic("fed", 0x100000007, 1, 90, false); err != nil {
				t.Fatal(err)
			}
			data, err := codec.Marshal("fed")
			if err != nil {
				t.Fatal(err)
			}
			before := codec.Manager.ExportState("fed")
			var envelope momSnapshotEnvelope
			if err := json.Unmarshal(data[len(momSnapshotMagic):], &envelope); err != nil {
				t.Fatal(err)
			}
			history := envelope.Runtime.Traffic.Federates[0].ObjectHistory
			switch which {
			case "duplicateObject":
				history.Updated = append(history.Updated, history.Updated[0])
			case "conflictingClass":
				history.Reflected = []mom.ObjectHistoryEntry{{Object: 1, Class: 91}}
			case "negativePeriod":
				envelope.Runtime.Federates[0].ReportPeriod = -1
			case "serialOverflow":
				envelope.Runtime.Federates[0].ServiceReports = &mom.ServiceReportState{NextSerial: 1 << 32}
			}
			body, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := codec.PrepareRestore("fed", append([]byte(momSnapshotMagic), body...)); err == nil {
				t.Fatal("invalid catalog state accepted")
			}
			if !reflect.DeepEqual(before, codec.Manager.ExportState("fed")) {
				t.Fatal("failed preparation mutated state")
			}
		})
	}
}
