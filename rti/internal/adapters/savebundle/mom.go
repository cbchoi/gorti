package savebundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"

	codec "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/proto"
)

type MOM struct{ Manager *mom.Manager }

// A zero tag cannot start a valid protobuf message, so legacy snapshots and
// the versioned extension are distinguishable without changing the wire schema.
const momSnapshotMagic = "\x00gorti.mom\x00"

type momSnapshotEnvelope struct {
	LogicalTimeImplementationName string              `json:"logical_time_implementation_name,omitempty"`
	Version                       uint32              `json:"version"`
	LegacyProtoBytes              []byte              `json:"legacyProtoBytes"`
	Runtime                       *momSnapshotRuntime `json:"runtime"`
}

type momSnapshotRuntime struct {
	AutoProvideSwitch bool                    `json:"autoProvideSwitch"`
	Objects           mom.ObjectInstanceState `json:"objects"`
	Federates         []momFederateRuntime    `json:"federates"`
	Traffic           *mom.TrafficState       `json:"traffic,omitempty"`
}

type momFederateRuntime struct {
	Handle                           core.FederateHandle     `json:"handle"`
	ObjectHandle                     core.ObjectHandle       `json:"objectHandle"`
	ObjectName                       string                  `json:"objectName"`
	ConveyRegionDesignatorSetsSwitch bool                    `json:"conveyRegionDesignatorSetsSwitch"`
	ConveyProducingFederateSwitch    bool                    `json:"conveyProducingFederateSwitch"`
	ServiceReportingEnabled          bool                    `json:"serviceReportingEnabled"`
	ExceptionReportingEnabled        bool                    `json:"exceptionReportingEnabled"`
	ServiceReports                   *mom.ServiceReportState `json:"serviceReports,omitempty"`
	ReportPeriod                     int32                   `json:"reportPeriod,omitempty"`
	RuntimeMetrics                   *mom.RuntimeMetricState `json:"runtimeMetrics,omitempty"`
}

func (s MOM) Marshal(fed core.FederationName) ([]byte, error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("mom snapshot for %q: manager is nil", fed)
	}
	state := s.Manager.ExportState(fed)
	if state == nil {
		return nil, nil
	}
	if err := state.Validate(fed); err != nil {
		return nil, fmt.Errorf("mom snapshot for %q: %w", fed, err)
	}
	runtime := &momSnapshotRuntime{AutoProvideSwitch: state.Federation.AutoProvideSwitch, Objects: state.Objects, Traffic: state.Traffic}
	wire := &rtiv1.MOMManagerState{Federation: &rtiv1.MOMFederationRecord{Name: string(state.Federation.Name), FomModuleNames: append([]string(nil), state.Federation.FOMModuleNames...)}}
	for _, handle := range state.Federation.FederateHandles {
		wire.Federation.FederateHandles = append(wire.Federation.FederateHandles, uint64(handle))
	}
	for _, entry := range state.Federates {
		record := &rtiv1.MOMFederateRecord{Handle: uint64(entry.Handle), Name: entry.Name, FederateType: entry.FederateType, TimeRegulating: entry.TimeRegulating, TimeConstrained: entry.TimeConstrained, Lookahead: float64(entry.Lookahead), LogicalTime: float64(entry.LogicalTime), InteractionsSent: entry.InteractionsSent, InteractionsReceived: entry.InteractionsReceived, UpdatesSent: entry.UpdatesSent, ReflectionsReceived: entry.ReflectionsReceived}
		if err := encodeMOMRecordTime(entry, record); err != nil {
			return nil, err
		}
		wire.Federates = append(wire.Federates, record)
		runtime.Federates = append(runtime.Federates, momFederateRuntime{
			Handle: entry.Handle, ObjectHandle: entry.ObjectHandle, ObjectName: entry.ObjectName,
			ConveyRegionDesignatorSetsSwitch: entry.ConveyRegionDesignatorSetsSwitch,
			ConveyProducingFederateSwitch:    entry.ConveyProducingFederateSwitch,
			ServiceReportingEnabled:          entry.ServiceReportingEnabled,
			ExceptionReportingEnabled:        entry.ExceptionReportingEnabled,
			ServiceReports:                   entry.ServiceReports,
			ReportPeriod:                     entry.ReportPeriod,
			RuntimeMetrics:                   entry.RuntimeMetrics,
		})
	}
	// Logical time stays in protobuf: JSON cannot encode the +Inf final time.
	base, err := (proto.MarshalOptions{Deterministic: true}).Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("mom snapshot for %q: %w", fed, err)
	}
	envelope := momSnapshotEnvelope{Version: 1, LegacyProtoBytes: base, Runtime: runtime}
	for _, entry := range state.Federates {
		if entry.LogicalTimeValue != nil && entry.LogicalTimeValue.Representation() == logicaltime.HLAinteger64Time {
			envelope.Version = 2
			envelope.LogicalTimeImplementationName = checkpointHLAinteger64Time
		}
	}
	representation := logicaltime.HLAfloat64Time
	if envelope.Version == 2 {
		representation = logicaltime.HLAinteger64Time
	}
	for _, entry := range wire.Federates {
		if _, _, err := decodeMOMTimeRecord(entry, representation); err != nil {
			return nil, fmt.Errorf("MOM mixed or invalid time representation: %w", err)
		}
	}
	extension, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("mom snapshot for %q: %w", fed, err)
	}
	return append([]byte(momSnapshotMagic), extension...), nil
}

func (s MOM) Unmarshal(fed core.FederationName, data []byte) error {
	// Preserve the legacy direct-call contract; the manifest-aware path uses
	// PrepareRestore, where present empty bytes mean an empty saved state.
	if len(data) == 0 {
		return nil
	}
	commit, err := s.PrepareRestore(fed, data)
	if err != nil {
		return err
	}
	if commit != nil {
		commit()
	}
	return nil
}

// PrepareRestore validates the complete snapshot before any live mutation.
func (s MOM) PrepareRestore(fed core.FederationName, data []byte) (func(), error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("mom snapshot for %q: manager is nil", fed)
	}
	if len(data) == 0 {
		return s.Manager.PrepareImportState(fed, nil)
	}
	var runtime *momSnapshotRuntime
	representation := logicaltime.HLAfloat64Time
	if bytes.HasPrefix(data, []byte(momSnapshotMagic)) {
		var envelope momSnapshotEnvelope
		if err := decodeSnapshotJSON(data[len(momSnapshotMagic):], &envelope); err != nil {
			return nil, fmt.Errorf("mom snapshot for %q: extension: %w", fed, err)
		}
		if err := validateMOMEnvelope(envelope); err != nil {
			return nil, fmt.Errorf("mom snapshot for %q: unsupported or incomplete extension version %d", fed, envelope.Version)
		}
		if envelope.Version == 2 {
			representation = logicaltime.HLAinteger64Time
		}
		data, runtime = envelope.LegacyProtoBytes, envelope.Runtime
	}
	var wire rtiv1.MOMManagerState
	if err := validateMOMTimeWire(data, wire.ProtoReflect().Descriptor(), representation); err != nil {
		return nil, fmt.Errorf("mom snapshot for %q: %w", fed, err)
	}
	if err := proto.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("mom snapshot for %q: %w", fed, err)
	}
	state := &mom.ManagerState{}
	if federation := wire.GetFederation(); federation != nil {
		state.Federation.Name = core.FederationName(federation.GetName())
		state.Federation.FOMModuleNames = append([]string(nil), federation.GetFomModuleNames()...)
		for _, handle := range federation.GetFederateHandles() {
			state.Federation.FederateHandles = append(state.Federation.FederateHandles, core.FederateHandle(handle))
		}
	}
	if err := decodeMOMFederates(fed, &wire, representation, state); err != nil {
		return nil, err
	}
	if err := restoreMOMRuntime(fed, state, runtime); err != nil {
		return nil, err
	}
	if err := state.Validate(fed); err != nil {
		return nil, fmt.Errorf("mom snapshot for %q: %w", fed, err)
	}
	return s.Manager.PrepareImportState(fed, state)
}

func validateMOMEnvelope(envelope momSnapshotEnvelope) error {
	if envelope.Runtime == nil || len(envelope.LegacyProtoBytes) == 0 {
		return fmt.Errorf("missing MOM runtime")
	}
	if envelope.Version == 1 && (envelope.LogicalTimeImplementationName == "" || envelope.LogicalTimeImplementationName == checkpointHLAfloat64Time) {
		return nil
	}
	if envelope.Version == 2 && envelope.LogicalTimeImplementationName == checkpointHLAinteger64Time {
		return nil
	}
	return fmt.Errorf("unsupported MOM snapshot representation")
}

func decodeMOMTimeRecord(record *rtiv1.MOMFederateRecord, expected logicaltime.Representation) (*logicaltime.Time, *logicaltime.Interval, error) {
	if record == nil {
		return nil, nil, fmt.Errorf("nil MOM federate")
	}
	// Historical Float64 snapshots use +Inf for the product final-time sentinel.
	// Preserve that legacy-only value; exact standardized values remain finite.
	if expected == logicaltime.HLAfloat64Time && record.LogicalTimeValue == nil && math.IsInf(record.LogicalTime, 1) {
		if record.LookaheadValue != nil {
			return nil, nil, fmt.Errorf("legacy final time cannot contain a partial exact pair")
		}
		if _, err := logicaltime.Float64Interval(record.Lookahead); err != nil {
			return nil, nil, err
		}
		return nil, nil, nil
	}
	value, err := codec.DecodeLogicalTimePair(expected, record.LogicalTime, record.LogicalTimeValue)
	if err != nil {
		return nil, nil, err
	}
	lookahead, err := codec.DecodeLogicalTimeIntervalPair(expected, record.Lookahead, record.LookaheadValue)
	if err != nil {
		return nil, nil, err
	}
	return &value, &lookahead, nil
}

func encodeMOMRecordTime(entry mom.FederateState, record *rtiv1.MOMFederateRecord) error {
	if entry.LogicalTimeValue != nil || entry.LookaheadValue != nil {
		if entry.LogicalTimeValue == nil || entry.LookaheadValue == nil {
			return fmt.Errorf("mom snapshot: incomplete time values")
		}
		representation := entry.LogicalTimeValue.Representation()
		var err error
		record.LogicalTime, record.LogicalTimeValue, err = codec.EncodeLogicalTimePair(representation, *entry.LogicalTimeValue)
		if err != nil {
			return err
		}
		record.Lookahead, record.LookaheadValue, err = codec.EncodeLogicalTimeIntervalPair(representation, *entry.LookaheadValue)
		if err != nil {
			return err
		}
	}
	return nil
}

func decodeMOMFederates(fed core.FederationName, wire *rtiv1.MOMManagerState, representation logicaltime.Representation, state *mom.ManagerState) error {
	for _, entry := range wire.GetFederates() {
		if entry == nil {
			return fmt.Errorf("mom snapshot for %q: nil federate record", fed)
		}
		value, lookahead, err := decodeMOMTimeRecord(entry, representation)
		if err != nil {
			return fmt.Errorf("mom snapshot for %q: %w", fed, err)
		}
		record := mom.FederateState{Handle: core.FederateHandle(entry.GetHandle()), Name: entry.GetName(), FederateType: entry.GetFederateType(), TimeRegulating: entry.GetTimeRegulating(), TimeConstrained: entry.GetTimeConstrained(), InteractionsSent: entry.GetInteractionsSent(), InteractionsReceived: entry.GetInteractionsReceived(), UpdatesSent: entry.GetUpdatesSent(), ReflectionsReceived: entry.GetReflectionsReceived()}
		if representation == logicaltime.HLAinteger64Time || entry.LogicalTimeValue != nil || entry.LookaheadValue != nil {
			record.LogicalTimeValue = value
			record.LookaheadValue = lookahead
		}
		if representation == logicaltime.HLAfloat64Time {
			record.Lookahead = core.LogicalTime(entry.GetLookahead())
			record.LogicalTime = core.LogicalTime(entry.GetLogicalTime())
			if value != nil {
				floating, _ := value.Float64()
				record.LogicalTime = core.LogicalTime(floating)
			}
			if lookahead != nil {
				floating, _ := lookahead.Float64()
				record.Lookahead = core.LogicalTime(floating)
			}
		}
		state.Federates = append(state.Federates, record)
	}
	return nil
}

func restoreMOMRuntime(fed core.FederationName, state *mom.ManagerState, runtime *momSnapshotRuntime) error {
	if runtime != nil {
		if len(runtime.Federates) != len(state.Federates) {
			return fmt.Errorf("mom snapshot for %q: runtime federate count differs", fed)
		}
		state.Federation.AutoProvideSwitch = runtime.AutoProvideSwitch
		state.Objects = runtime.Objects
		state.Traffic = runtime.Traffic
		byHandle := make(map[core.FederateHandle]momFederateRuntime, len(runtime.Federates))
		for _, extra := range runtime.Federates {
			if _, exists := byHandle[extra.Handle]; exists {
				return fmt.Errorf("mom snapshot for %q: duplicate runtime federate %d", fed, extra.Handle)
			}
			byHandle[extra.Handle] = extra
		}
		for i := range state.Federates {
			entry := &state.Federates[i]
			extra, exists := byHandle[entry.Handle]
			if !exists {
				return fmt.Errorf("mom snapshot for %q: missing runtime federate %d", fed, entry.Handle)
			}
			entry.ObjectHandle, entry.ObjectName = extra.ObjectHandle, extra.ObjectName
			entry.ConveyRegionDesignatorSetsSwitch = extra.ConveyRegionDesignatorSetsSwitch
			entry.ConveyProducingFederateSwitch = extra.ConveyProducingFederateSwitch
			entry.ServiceReportingEnabled = extra.ServiceReportingEnabled
			entry.ExceptionReportingEnabled = extra.ExceptionReportingEnabled
			entry.ServiceReports = extra.ServiceReports
			entry.ReportPeriod = extra.ReportPeriod
			entry.RuntimeMetrics = extra.RuntimeMetrics
		}
	}
	return nil
}

const checkpointHLAfloat64Time = "HLAfloat64Time"

const checkpointHLAinteger64Time = "HLAinteger64Time"
