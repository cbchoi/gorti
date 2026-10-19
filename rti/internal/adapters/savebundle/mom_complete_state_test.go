package savebundle

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/protobuf/proto"
)

type momCompleteOutbox struct{}

func (momCompleteOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return nil
}

func completeMOMCodec(t *testing.T) MOM {
	t.Helper()
	m, err := mom.New(mom.Options{Outbox: momCompleteOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.FederationCreated(ctx, "fed", []core.FOMModule{{Path: "base.xml"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateJoined(ctx, "fed", 7, "alpha", "model"); err != nil {
		t.Fatal(err)
	}
	m.SetAutoProvideSwitch("fed", true)
	m.SetConveyRegionDesignatorSetsSwitch("fed", 7, true)
	m.SetConveyProducingFederateSwitch("fed", 7, true)
	m.SetServiceReporting("fed", 7, true)
	m.SetExceptionReporting("fed", 7, true)
	m.IncrementUpdatesSent("fed", 7)
	if err := m.TimeStateChanged(ctx, "fed", 7, true, true, 0.5, core.LogicalTime(math.Inf(1))); err != nil {
		t.Fatal(err)
	}
	return MOM{Manager: m}
}

func TestMOMCompleteSnapshotRoundTripAndPreparedIsolation(t *testing.T) {
	codec := completeMOMCodec(t)
	state := codec.Manager.ExportState("fed")
	state.Objects = mom.ObjectInstanceState{
		Enabled: true, FederationClass: 10, FederateClass: 11,
		AttributeFederationName: 1, AttributeFederates: 2,
		AttributeFederateHandle: 3, AttributeFederateName: 4, AttributeFederateType: 5,
		FederationObject: (1 << 54) + 100, FederationObjectName: "HLAfederation.fed",
	}
	state.Federates[0].ObjectHandle = (1 << 54) + 101
	state.Federates[0].ObjectName = "HLAfederate.7"
	codec.Manager.ImportState("fed", state)
	data, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := codec.Marshal("fed")
	if err != nil || !bytes.Equal(data, duplicate) {
		t.Fatalf("snapshot not deterministic: %v", err)
	}
	want := codec.Manager.ExportState("fed")
	codec.Manager.SetAutoProvideSwitch("fed", false)
	codec.Manager.SetConveyRegionDesignatorSetsSwitch("fed", 7, false)
	codec.Manager.SetConveyProducingFederateSwitch("fed", 7, false)
	codec.Manager.SetServiceReporting("fed", 7, false)
	codec.Manager.SetExceptionReporting("fed", 7, false)
	changed := codec.Manager.ExportState("fed")
	changed.Objects = mom.ObjectInstanceState{}
	changed.Federates[0].ObjectHandle, changed.Federates[0].ObjectName = 0, ""
	codec.Manager.ImportState("fed", changed)
	before := codec.Manager.ExportState("fed")
	commit, err := codec.PrepareRestore("fed", data)
	if err != nil || commit == nil {
		t.Fatalf("prepare: %v", err)
	}
	if !reflect.DeepEqual(before, codec.Manager.ExportState("fed")) {
		t.Fatal("prepare mutated live state")
	}
	clear(data)
	commit()
	if !codec.Manager.AutoProvideSwitch("fed") || !codec.Manager.ConveyRegionDesignatorSetsSwitch("fed", 7) || !codec.Manager.ConveyProducingFederateSwitch("fed", 7) || !codec.Manager.ServiceReporting("fed", 7) || !codec.Manager.ExceptionReporting("fed", 7) {
		t.Fatal("MOM switches/reporting flags were lost")
	}
	if got := codec.Manager.ExportState("fed"); !reflect.DeepEqual(want, got) {
		t.Fatalf("round trip: got=%+v want=%+v", got, want)
	}
}

func TestMOMCompleteSnapshotRejectsInvalidExtensionsWithoutMutation(t *testing.T) {
	for name, mutate := range map[string]func(*momSnapshotEnvelope){
		"unsupported_version": func(e *momSnapshotEnvelope) { e.Version++ },
		"missing_runtime":     func(e *momSnapshotEnvelope) { e.Runtime = nil },
		"missing_base":        func(e *momSnapshotEnvelope) { e.LegacyProtoBytes = nil },
		"bad_base":            func(e *momSnapshotEnvelope) { e.LegacyProtoBytes = []byte{0xff} },
		"missing_federate":    func(e *momSnapshotEnvelope) { e.Runtime.Federates = nil },
		"extra_federate": func(e *momSnapshotEnvelope) {
			e.Runtime.Federates = append(e.Runtime.Federates, e.Runtime.Federates[0])
		},
		"wrong_runtime_handle":          func(e *momSnapshotEnvelope) { e.Runtime.Federates[0].Handle = 8 },
		"disabled_cache_has_handles":    func(e *momSnapshotEnvelope) { e.Runtime.Objects.FederationClass = 10 },
		"enabled_cache_missing_handles": func(e *momSnapshotEnvelope) { e.Runtime.Objects.Enabled = true },
		"instance_without_cache": func(e *momSnapshotEnvelope) {
			e.Runtime.Federates[0].ObjectHandle = 100
			e.Runtime.Federates[0].ObjectName = "instance"
		},
		"instance_name_without_handle": func(e *momSnapshotEnvelope) { e.Runtime.Federates[0].ObjectName = "instance" },
	} {
		t.Run(name, func(t *testing.T) {
			codec := completeMOMCodec(t)
			before := codec.Manager.ExportState("fed")
			data, err := codec.Marshal("fed")
			if err != nil {
				t.Fatal(err)
			}
			var envelope momSnapshotEnvelope
			if err := json.Unmarshal(data[len(momSnapshotMagic):], &envelope); err != nil {
				t.Fatal(err)
			}
			mutate(&envelope)
			payload, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			commit, err := codec.PrepareRestore("fed", append([]byte(momSnapshotMagic), payload...))
			if err == nil || commit != nil {
				t.Fatal("invalid extension accepted")
			}
			if !reflect.DeepEqual(before, codec.Manager.ExportState("fed")) {
				t.Fatal("invalid extension changed live state")
			}
		})
	}
}

func TestMOMCompleteSnapshotRejectsMalformedFraming(t *testing.T) {
	codec := completeMOMCodec(t)
	data, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	before := codec.Manager.ExportState("fed")
	for name, malformed := range map[string][]byte{
		"truncated_magic": []byte(momSnapshotMagic[:3]),
		"empty_extension": []byte(momSnapshotMagic),
		"truncated_json":  data[:len(data)-1],
		"trailing_json":   append(append([]byte(nil), data...), []byte(" {}")...),
		"unknown_field":   bytes.Replace(data, []byte(`"version":1`), []byte(`"version":1,"unexpected":true`), 1),
		"overflow_handle": bytes.Replace(data, []byte(`"handle":7`), []byte(`"handle":18446744073709551616`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			commit, err := codec.PrepareRestore("fed", malformed)
			if err == nil || commit != nil {
				t.Fatal("malformed framing accepted")
			}
			if !reflect.DeepEqual(before, codec.Manager.ExportState("fed")) {
				t.Fatal("malformed input changed live state")
			}
		})
	}
}

func TestMOMCompleteSnapshotPresentEmptyClears(t *testing.T) {
	for _, data := range [][]byte{nil, {}} {
		codec := completeMOMCodec(t)
		commit, err := codec.PrepareRestore("fed", data)
		if err != nil || commit == nil {
			t.Fatalf("prepare empty: %v", err)
		}
		if codec.Manager.ExportState("fed") == nil {
			t.Fatal("prepare cleared state before commit")
		}
		commit()
		if codec.Manager.ExportState("fed") != nil {
			t.Fatal("empty snapshot did not clear saved federation state")
		}
	}
}

func legacyMOMWire() *rtiv1.MOMManagerState {
	return &rtiv1.MOMManagerState{
		Federation: &rtiv1.MOMFederationRecord{Name: "fed", FederateHandles: []uint64{7}, FomModuleNames: []string{"old.xml"}},
		Federates:  []*rtiv1.MOMFederateRecord{{Handle: 7, Name: "old", FederateType: "model", LogicalTime: math.Inf(1), Lookahead: 0.5, UpdatesSent: 8}},
	}
}

func TestMOMCompleteSnapshotReadsLegacyProtobuf(t *testing.T) {
	codec := completeMOMCodec(t)
	data, err := proto.Marshal(legacyMOMWire())
	if err != nil {
		t.Fatal(err)
	}
	if err := codec.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	got, ok := codec.Manager.QueryFederateAttributes("fed", 7)
	if !ok || got.Name != "old" || got.UpdatesSent != 8 || !math.IsInf(float64(got.LogicalTime), 1) {
		t.Fatalf("legacy snapshot not restored: %+v", got)
	}
	if codec.Manager.ServiceReporting("fed", 7) || codec.Manager.AutoProvideSwitch("fed") {
		t.Fatal("missing legacy fields retained post-save state")
	}
}

func TestMOMCompleteSnapshotRejectsInvalidLegacyWithoutMutation(t *testing.T) {
	for name, mutate := range map[string]func(*rtiv1.MOMManagerState){
		"duplicate_federate": func(w *rtiv1.MOMManagerState) {
			w.Federates = append(w.Federates, proto.Clone(w.Federates[0]).(*rtiv1.MOMFederateRecord))
		},
		"zero_handle":      func(w *rtiv1.MOMManagerState) { w.Federates[0].Handle = 0 },
		"roster_mismatch":  func(w *rtiv1.MOMManagerState) { w.Federation.FederateHandles[0] = 8 },
		"duplicate_roster": func(w *rtiv1.MOMManagerState) { w.Federation.FederateHandles = append(w.Federation.FederateHandles, 7) },
		"wrong_federation": func(w *rtiv1.MOMManagerState) { w.Federation.Name = "other" },
		"nan_time":         func(w *rtiv1.MOMManagerState) { w.Federates[0].LogicalTime = math.NaN() },
		"nan_lookahead":    func(w *rtiv1.MOMManagerState) { w.Federates[0].Lookahead = math.NaN() },
	} {
		t.Run(name, func(t *testing.T) {
			codec := completeMOMCodec(t)
			before := codec.Manager.ExportState("fed")
			wire := legacyMOMWire()
			mutate(wire)
			data, err := proto.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			commit, err := codec.PrepareRestore("fed", data)
			if err == nil || commit != nil {
				t.Fatal("invalid state accepted")
			}
			if !reflect.DeepEqual(before, codec.Manager.ExportState("fed")) {
				t.Fatal("invalid state mutated live manager")
			}
		})
	}
}
