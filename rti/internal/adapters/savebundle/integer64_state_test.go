package savebundle

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func integerTimeCodec(t *testing.T) IntegerTime {
	t.Helper()
	m, err := timepkg.NewOf[int64](timepkg.OptionsOf[int64]{Clock: core.NewRealClock(), Outbox: &timeStateOutbox{}, StallTimeout: stdtime.Second})
	if err != nil {
		t.Fatal(err)
	}
	return IntegerTime{Manager: m}
}

func TestInteger64TimeSnapshotCompleteQueuesAndFrontiers(t *testing.T) {
	s := integerTimeCodec(t)
	current, request, received, minimum := int64(1<<53+7), int64(1<<53+31), int64(1<<53+13), int64(1<<53+17)
	timestamp := int64(1<<53 + 23)
	metadata := &core.ObjectCallbackMetadata{SentOrder: core.OrderTimestamp, ReceivedOrder: core.OrderTimestamp, Producer: 7, RetractionHandle: 123, Regions: []core.DDMRegionHandleCore{11}}
	tso := &object.CallbackEventOf[int64]{Kind: object.CallbackReflect, Sequence: 91, Object: 44, ObjectClass: 3, ObjectName: "saved", Attributes: map[uint64][]byte{9: {0, 255}}, AttributeHandles: []core.AttributeHandle{9}, LogicalTime: &timestamp, Metadata: metadata, UserSuppliedTag: []byte{0, 254}}
	ro := &object.CallbackEventOf[int64]{Kind: object.CallbackReceive, Sequence: 92, InteractionClass: 5, Parameters: map[uint64][]byte{8: {255, 0}}, Metadata: &core.ObjectCallbackMetadata{SentOrder: core.OrderReceive, ReceivedOrder: core.OrderReceive, Producer: 7}, UserSuppliedTag: []byte{255, 0}}
	state := &timepkg.ManagerStateOf[int64]{StallTimeout: stdtime.Second, Federates: []timepkg.FederateStateOf[int64]{{Handle: 2, HasRegulationState: true, HasAdvanceState: true, Regulating: true, Constrained: true, Lookahead: 3, CurrentTime: current, PreservedReceiveTime: &current, ReceivedTSOTime: &received, PendingRequest: true, RequestedTime: request, Mode: timepkg.ModeNER, FlushMinimum: &minimum, HasPendingSince: true, PendingAge: stdtime.Millisecond, Queue: []core.MessageDeliveryOf[int64]{{Recipient: 2, Timestamp: timestamp, Sender: 7, RetractionHandle: 123, Event: tso}}, ROQueue: []core.MessageDeliveryOf[int64]{{Recipient: 2, Sender: 7, ReceiveOrder: true, Event: ro}}}}}
	commit, err := s.Manager.PrepareImportState("fed", state, cloneTimeEventOf[int64, integerSnapshotNumber])
	if err != nil {
		t.Fatal(err)
	}
	commit()
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("9007199254741015")) {
		t.Fatalf("integer timestamp rounded: %s", data)
	}
	dst := integerTimeCodec(t)
	restore, err := dst.PrepareRestore("fed", data)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := dst.Manager.ExportState("fed", cloneTimeEventOf[int64, integerSnapshotNumber])
	if before != nil && len(before.Federates) != 0 {
		t.Fatal("prepare published state")
	}
	restore()
	got, err := dst.Manager.ExportState("fed", cloneTimeEventOf[int64, integerSnapshotNumber])
	if err != nil {
		t.Fatal(err)
	}
	// Pending age accrues with the real clock, so compare separately.
	got.Federates[0].PendingAge = state.Federates[0].PendingAge
	if !reflect.DeepEqual(got, state) {
		t.Fatalf("state mismatch:\ngot %+v\nwant %+v", got.Federates[0], state.Federates[0])
	}
}

func TestInteger64SnapshotRejectsWrongFactoryAndInvalidNumbers(t *testing.T) {
	s := integerTimeCodec(t)
	for _, input := range []string{`{"version":1,"halted":false,"stall_timeout_ns":1000000000,"federates":[]}`, `{"version":6,"logical_time_implementation_name":"HLAfloat64Time","federates":[]}`, `{"version":6,"logical_time_implementation_name":"HLAinteger64Time","federates":[{"handle":2,"current_time":9007199254740993.5}]}`} {
		if commit, err := s.PrepareRestore("fed", []byte(input)); err == nil || commit != nil {
			t.Fatalf("invalid integer snapshot accepted: %s", input)
		}
	}
	for _, input := range []string{`null`, `-1`, `9223372036854775808`, `1.5`, `"+Inf"`, `1e3`} {
		var number integerSnapshotNumber
		if err := json.Unmarshal([]byte(input), &number); err == nil {
			t.Fatalf("invalid integer scalar accepted: %s", input)
		}
	}
	for _, value := range []int64{1<<53 + 1, 1<<53 + 3, math.MaxInt64} {
		data, err := json.Marshal(integerSnapshotNumber(value))
		if err != nil {
			t.Fatal(err)
		}
		var got integerSnapshotNumber
		if err := json.Unmarshal(data, &got); err != nil || int64(got) != value {
			t.Fatalf("roundtrip %d => %s => %d (%v)", value, data, got, err)
		}
	}
}

func TestInteger64ObjectSnapshotPendingDeletionAndRebinding(t *testing.T) {
	manager, err := object.NewOf[int64](object.OptionsOf[int64]{Declarations: declaration.New(), Outbox: objectStateOutbox{}, FOMs: objectStateFOMs{}, Clock: core.NewRealClock()})
	if err != nil {
		t.Fatal(err)
	}
	s := IntegerObject{Manager: manager}
	timestamp := int64(1<<53 + 7)
	state := &object.RegistryStateOf[int64]{LastObjectHandle: 1, Objects: []object.InstanceState{{Handle: 1, Class: 3, Name: "saved", Registrant: 7}}, Deletions: []object.DeletionStateOf[int64]{{Object: 1, Producer: 7, Timestamp: timestamp, RetractionHandle: 123}}}
	commit, err := manager.PrepareImportState("fed", state)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(manager.ExportState("fed"), state) {
		t.Fatal("object pending deletion changed")
	}
	rebound, err := s.RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) { return h + 100, nil })
	if err != nil {
		t.Fatal(err)
	}
	var wire objectSnapshotV1Of[int64]
	if err := json.Unmarshal(rebound, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.State.Deletions[0].Timestamp != timestamp || wire.State.Deletions[0].Producer != 107 {
		t.Fatal("rebind rounded time or missed producer")
	}
}

func TestInteger64RetractionSnapshotExactLedger(t *testing.T) {
	tm := integerTimeCodec(t)
	s := IntegerRetractions{Manager: tm.Manager}
	history := &timepkg.RetractionHistoryOf[int64]{Complete: true, Producers: []timepkg.RetractionProducerHistoryOf[int64]{{Producer: 7, Accepted: []timepkg.RetractionIDRange{{First: 123, Last: 123}}, Messages: []timepkg.RetractionMessageStateOf[int64]{{ID: 123, Timestamp: 1<<53 + 7, Delivered: []core.FederateHandle{2}}}}}}
	data, err := writeRetractionsOf(history)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readRetractionsOf[int64](data)
	if err != nil || !reflect.DeepEqual(got, history) {
		t.Fatalf("retraction ledger changed: %+v %v", got, err)
	}
	if err := s.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.Manager.ReserveRetractionID(context.Background(), "fed", 7, 123); err == nil {
		t.Fatal("restored ID reused")
	}
	rebound, err := s.RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) { return h + 100, nil })
	if err != nil {
		t.Fatal(err)
	}
	got, err = readRetractionsOf[int64](rebound)
	if err != nil || got.Producers[0].Messages[0].Timestamp != 1<<53+7 || got.Producers[0].Producer != 107 || got.Producers[0].Messages[0].Delivered[0] != 102 {
		t.Fatalf("rebound %+v %v", got, err)
	}
}

func TestInteger64TimeSnapshotsAllAdvanceModesAndEnableHistory(t *testing.T) {
	for _, mode := range []timepkg.AdvanceMode{timepkg.ModeNER, timepkg.ModeNMRA, timepkg.ModeTAR, timepkg.ModeTARA, timepkg.ModeFQR} {
		s := integerTimeCodec(t)
		current, requested := int64(1<<53+7), int64(1<<53+39)
		entry := timepkg.FederateStateOf[int64]{Handle: 2, HasRegulationState: true, HasAdvanceState: true, Regulating: true, Constrained: true, Lookahead: 5, CurrentTime: current, RequestedTime: requested, PendingRequest: true, Mode: mode, ReceivedTSOTime: &current, PreservedReceiveTime: &current}
		target := int64(3)
		entry.RequestedLookahead = &target
		entry.LookaheadOriginTime = current
		entry.LookaheadOriginValue = 5
		if mode == timepkg.ModeNER || mode == timepkg.ModeNMRA || mode == timepkg.ModeFQR {
			minimum := current + 3
			entry.FlushMinimum = &minimum
		}
		state := &timepkg.ManagerStateOf[int64]{StallTimeout: stdtime.Second, Federates: []timepkg.FederateStateOf[int64]{entry}}
		commit, err := s.Manager.PrepareImportState("fed", state, cloneTimeEventOf[int64, integerSnapshotNumber])
		if err != nil {
			t.Fatalf("mode %v: %v", mode, err)
		}
		commit()
		data, err := s.Marshal("fed")
		if err != nil {
			t.Fatal(err)
		}
		dst := integerTimeCodec(t)
		if err := dst.Unmarshal("fed", data); err != nil {
			t.Fatal(err)
		}
		got, err := dst.Manager.ExportState("fed", cloneTimeEventOf[int64, integerSnapshotNumber])
		if err != nil || !reflect.DeepEqual(got, state) {
			t.Fatalf("mode %v changed history: %+v %v", mode, got, err)
		}
	}
	for _, regulation := range []bool{false, true} {
		s := integerTimeCodec(t)
		entry := timepkg.FederateStateOf[int64]{Handle: 2, HasRegulationState: true, HasAdvanceState: true, CurrentTime: 1<<53 + 7}
		if regulation {
			entry.RegulationPending = true
			entry.RegulationLookahead = 3
		} else {
			entry.ConstrainedPending = true
		}
		state := &timepkg.ManagerStateOf[int64]{StallTimeout: stdtime.Second, Federates: []timepkg.FederateStateOf[int64]{entry}}
		commit, err := s.Manager.PrepareImportState("fed", state, cloneTimeEventOf[int64, integerSnapshotNumber])
		if err != nil {
			t.Fatal(err)
		}
		commit()
		data, err := s.Marshal("fed")
		if err != nil {
			t.Fatal(err)
		}
		dst := integerTimeCodec(t)
		if err := dst.Unmarshal("fed", data); err != nil {
			t.Fatal(err)
		}
		got, err := dst.Manager.ExportState("fed", cloneTimeEventOf[int64, integerSnapshotNumber])
		if err != nil || !reflect.DeepEqual(got, state) {
			t.Fatalf("pending enable changed: %+v %v", got, err)
		}
	}
}

func TestInteger64FinalTimeSnapshotRemainsFiniteMaxInt64(t *testing.T) {
	s := integerTimeCodec(t)
	state := &timepkg.ManagerStateOf[int64]{StallTimeout: stdtime.Second, Federates: []timepkg.FederateStateOf[int64]{{Handle: 2, HasAdvanceState: true, CurrentTime: math.MaxInt64}}}
	commit, err := s.Manager.PrepareImportState("fed", state, cloneTimeEventOf[int64, integerSnapshotNumber])
	if err != nil {
		t.Fatal(err)
	}
	commit()
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("9223372036854775807")) || bytes.Contains(data, []byte("+Inf")) {
		t.Fatalf("integer final time corrupted: %s", data)
	}
	dst := integerTimeCodec(t)
	if err := dst.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	got, err := dst.Manager.ExportState("fed", cloneTimeEventOf[int64, integerSnapshotNumber])
	if err != nil || !reflect.DeepEqual(got, state) {
		t.Fatalf("integer final changed: %+v %v", got, err)
	}
}

func TestInteger64SnapshotRejectsMissingRequiredTimeScalars(t *testing.T) {
	for _, field := range []string{"current_time", "lookahead", "requested_time"} {
		s := integerTimeCodec(t)
		wire := timeSnapshotOf[integerSnapshotNumber]{Version: 6, LogicalTimeImplementationName: "HLAinteger64Time", StallTimeout: stdtime.Second, Federates: []timeFederateSnapshotOf[integerSnapshotNumber]{{Handle: 2, HasAdvanceState: true, CurrentTime: 1<<53 + 7}}}
		data := graphJSON(t, wire)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(data, &fields)
		var federates []map[string]json.RawMessage
		_ = json.Unmarshal(fields["federates"], &federates)
		delete(federates[0], field)
		fields["federates"] = graphJSON(t, federates)
		if _, err := s.PrepareRestore("fed", graphJSON(t, fields)); err == nil {
			t.Fatalf("missing required integer scalar accepted: %s", field)
		}
	}
	for _, timestamp := range []string{"", "null"} {
		manager, err := object.NewOf[int64](object.OptionsOf[int64]{Declarations: declaration.New(), Outbox: objectStateOutbox{}, FOMs: objectStateFOMs{}, Clock: core.NewRealClock()})
		if err != nil {
			t.Fatal(err)
		}
		field := ""
		if timestamp != "" {
			field = `,"Timestamp":` + timestamp
		}
		data := []byte(`{"version":5,"logical_time_implementation_name":"HLAinteger64Time","state":{"LastObjectHandle":1,"Objects":[{"Handle":1,"Class":3,"Name":"saved","Registrant":7}],"Deletions":[{"Object":1,"Producer":7` + field + `}]}}`)
		if _, err := (IntegerObject{Manager: manager}).PrepareRestore("fed", data); err == nil {
			t.Fatal("missing/null integer deletion timestamp accepted")
		}
	}
}
