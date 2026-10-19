package eventlog

import (
	"fmt"
	"math"

	valuecodec "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/proto"
)

// wireMessageForRecord maps transport-neutral HLA journal records to the
// frozen event-log Protobuf representation. Legacy proto-backed records remain
// accepted so existing readers, replay fixtures, and captures stay compatible.
func wireMessageForRecord(record core.EventRecord) (proto.Message, error) {
	if err := validateJournalRecord(record); err != nil {
		return nil, err
	}
	if message, ok := record.(proto.Message); ok {
		return message, nil
	}
	switch event := record.(type) {
	case *core.FederationJoinedJournalRecord:
		return federationJoinedJournalEvent(event)
	case *core.FederationResignedJournalRecord:
		return federationResignedJournalEvent(event)
	case *core.ObjectJournalRecord:
		return objectJournalEvent(event)
	case *core.ObjectJournalRecordOf[int64]:
		return objectJournalEventOf(event)
	case *timepkg.TimeAdvanceGrantedRecordOf[core.LogicalTime]:
		return timeGrantedJournalEvent(event)
	case *timepkg.TimeAdvanceGrantedRecordOf[int64]:
		return timeGrantedJournalEvent(event)
	default:
		return &rtiv1.Event{Seq: record.Seq()}, nil
	}
}

// validateJournalRecord rejects unsupported state before the writer assigns a
// sequence. The frozen registration message cannot preserve region bindings.
func validateJournalRecord(record core.EventRecord) error {
	if event, ok := record.(*core.ObjectJournalRecord); ok && event != nil && event.Kind == core.ObjectJournalRegistered && event.RegionScoped {
		return core.ErrDDMJournalUnsupported
	}
	if event, ok := record.(*core.ObjectJournalRecordOf[int64]); ok && event != nil && event.Kind == core.ObjectJournalRegistered && event.RegionScoped {
		return core.ErrDDMJournalUnsupported
	}
	return nil
}

func federationJoinedJournalEvent(event *core.FederationJoinedJournalRecord) (*rtiv1.Event, error) {
	if event == nil {
		return nil, fmt.Errorf("eventlog: nil federation joined journal record")
	}
	modules := make([]*rtiv1.FOMModule, len(event.AdditionalFOMModules))
	for i, module := range event.AdditionalFOMModules {
		modules[i] = &rtiv1.FOMModule{Path: module.Path, Xml: append([]byte(nil), module.XML...)}
	}
	return &rtiv1.Event{
		Seq: event.Sequence, WallNs: event.WallNS,
		Body: &rtiv1.Event_FedJoined{FedJoined: &rtiv1.FederateJoined{
			FederateHandle: uint64(event.FederateHandle), FederateName: event.FederateName,
			FederateType: event.FederateType, AdditionalFomModules: modules,
			FomRevision: event.FOMRevision, FomDigest: event.FOMDigest,
		}},
	}, nil
}

func federationResignedJournalEvent(event *core.FederationResignedJournalRecord) (*rtiv1.Event, error) {
	if event == nil {
		return nil, fmt.Errorf("eventlog: nil federation resigned journal record")
	}
	action, err := journalResignActionToWire(event.ResignAction)
	if err != nil {
		return nil, err
	}
	return &rtiv1.Event{
		Seq: event.Sequence, WallNs: event.WallNS,
		Body: &rtiv1.Event_FedResigned{FedResigned: &rtiv1.FederateResigned{
			FederateHandle: uint64(event.FederateHandle), Action: action,
		}},
	}, nil
}

func journalResignActionToWire(action core.ResignAction) (rtiv1.ResignAction, error) {
	switch action {
	case core.ResignActionUnconditionallyDivestAttributes:
		return rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES, nil
	case core.ResignActionDeleteThenDivest:
		return rtiv1.ResignAction_RESIGN_ACTION_DELETE_THEN_DIVEST, nil
	case core.ResignActionCancelThenDelete:
		return rtiv1.ResignAction_RESIGN_ACTION_CANCEL_THEN_DELETE, nil
	case core.ResignActionCancelPendingOwnership:
		return rtiv1.ResignAction_RESIGN_ACTION_CANCEL_PENDING_OWNERSHIP, nil
	case core.ResignActionNoAction:
		return rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION, nil
	case core.ResignActionDeleteObjects:
		return rtiv1.ResignAction_RESIGN_ACTION_DELETE_OBJECTS, nil
	default:
		return 0, fmt.Errorf("eventlog: federation journal resign action %d: %w", action, core.ErrResignActionUnsupported)
	}
}

func objectJournalEvent(event *core.ObjectJournalRecord) (*rtiv1.Event, error) {
	return objectJournalEventOf(event)
}

func objectJournalEventOf[T core.LogicalTimeScalar](event *core.ObjectJournalRecordOf[T]) (*rtiv1.Event, error) {
	if event == nil {
		return nil, fmt.Errorf("eventlog: nil object journal record")
	}
	wire := &rtiv1.Event{Seq: event.Sequence, WallNs: event.WallNS}
	var logicalTime *float64
	var exactTime *rtiv1.LogicalTimeValue
	if event.LogicalTime != nil {
		if core.ScalarRepresentation[T]() == logicaltime.HLAfloat64Time && math.IsInf(float64(*event.LogicalTime), 1) {
			value := math.Inf(1)
			logicalTime = &value
		} else {
			value, err := core.ScalarTimeValue(*event.LogicalTime)
			if err != nil {
				return nil, err
			}
			logicalTime, exactTime, err = valuecodec.EncodeOptionalLogicalTimePair(core.ScalarRepresentation[T](), &value)
			if err != nil {
				return nil, err
			}
			// Retain historical Float64 journal bytes for strict replay comparison.
			if core.ScalarRepresentation[T]() == logicaltime.HLAfloat64Time {
				exactTime = nil
			}
		}
	}
	switch event.Kind {
	case core.ObjectJournalRegistered:
		wire.Body = &rtiv1.Event_ObjRegistered{ObjRegistered: &rtiv1.ObjectRegistered{
			ObjectHandle: uint64(event.Object), ObjectClassHandle: uint64(event.ObjectClass),
			OwnerFederateHandle: uint64(event.Federate), ObjectName: event.ObjectName,
		}}
	case core.ObjectJournalAttributeUpdated:
		wire.Body = &rtiv1.Event_AttrUpdated{AttrUpdated: &rtiv1.AttributeUpdated{
			ObjectHandle: uint64(event.Object), ProducerFederateHandle: uint64(event.Federate),
			Attributes: event.Attributes, LogicalTime: logicalTime, LogicalTimeValue: exactTime,
		}}
	case core.ObjectJournalInteractionSent:
		wire.Body = &rtiv1.Event_InterSent{InterSent: &rtiv1.InteractionSent{
			InteractionClassHandle: uint64(event.InteractionClass), ProducerFederateHandle: uint64(event.Federate),
			Parameters: event.Parameters, LogicalTime: logicalTime, LogicalTimeValue: exactTime,
		}}
	case core.ObjectJournalDeleted:
		wire.Body = &rtiv1.Event_ObjDeleted{ObjDeleted: &rtiv1.ObjectDeleted{ObjectHandle: uint64(event.Object), LogicalTime: logicalTime, LogicalTimeValue: exactTime}}
	default:
		return nil, fmt.Errorf("eventlog: unsupported object journal kind %d", event.Kind)
	}
	return wire, nil
}

func timeGrantedJournalEvent[T core.LogicalTimeScalar](event *timepkg.TimeAdvanceGrantedRecordOf[T]) (*rtiv1.Event, error) {
	if event == nil {
		return nil, fmt.Errorf("eventlog: nil grant record")
	}
	if core.ScalarRepresentation[T]() == logicaltime.HLAfloat64Time && math.IsInf(float64(event.Time), 1) {
		return &rtiv1.Event{Seq: event.Seq(), Body: &rtiv1.Event_TimeGranted{TimeGranted: &rtiv1.TimeAdvanceGranted{FederateHandle: uint64(event.Federate), LogicalTime: float64(event.Time)}}}, nil
	}
	value, err := core.ScalarTimeValue(event.Time)
	if err != nil {
		return nil, err
	}
	legacy, exact, err := valuecodec.EncodeLogicalTimePair(core.ScalarRepresentation[T](), value)
	if err != nil {
		return nil, err
	}
	return &rtiv1.Event{Seq: event.Seq(), Body: &rtiv1.Event_TimeGranted{TimeGranted: &rtiv1.TimeAdvanceGranted{FederateHandle: uint64(event.Federate), LogicalTime: legacy, LogicalTimeValue: exact}}}, nil
}
