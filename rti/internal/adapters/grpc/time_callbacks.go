package grpc

import (
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

func isTimeGrantEvent(event core.OutboundEvent) bool {
	switch event.(type) {
	case *timepkg.TimeAdvanceGrant, *timepkg.TimeAdvanceGrantOf[int64]:
		return true
	}
	return false
}
func encodeTimeGrant[T core.LogicalTimeScalar](event *timepkg.TimeAdvanceGrantOf[T]) (*rtiv1.FederateEvent, error) {
	legacy, exact, err := encodeScalarTime(event.Time)
	if err != nil {
		return nil, err
	}
	return &rtiv1.FederateEvent{Seq: event.Seq(), Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{LogicalTime: legacy, LogicalTimeValue: exact}}}, nil
}
func encodeRegulationEnabled[T core.LogicalTimeScalar](event *timepkg.TimeRegulationEnabledOf[T]) (*rtiv1.FederateEvent, error) {
	legacy, exact, err := encodeScalarTime(event.Time)
	if err != nil {
		return nil, err
	}
	return &rtiv1.FederateEvent{Seq: event.Seq(), Event: &rtiv1.FederateEvent_TimeRegulationEnabled{TimeRegulationEnabled: &rtiv1.TimeRegulationEnabled{LogicalTime: legacy, LogicalTimeValue: exact}}}, nil
}
func encodeConstrainedEnabled[T core.LogicalTimeScalar](event *timepkg.TimeConstrainedEnabledOf[T]) (*rtiv1.FederateEvent, error) {
	legacy, exact, err := encodeScalarTime(event.Time)
	if err != nil {
		return nil, err
	}
	return &rtiv1.FederateEvent{Seq: event.Seq(), Event: &rtiv1.FederateEvent_TimeConstrainedEnabled{TimeConstrainedEnabled: &rtiv1.TimeConstrainedEnabled{LogicalTime: legacy, LogicalTimeValue: exact}}}, nil
}
func encodeSaveInitiate[T core.LogicalTimeScalar](event *savepoint.InitiateSaveEventOf[T]) (*rtiv1.FederateEvent, error) {
	body := &rtiv1.InitiateFederateSave{Label: event.Label()}
	if value := event.SaveTime(); value != nil {
		legacy, exact, err := encodeScalarTime(*value)
		if err != nil {
			return nil, err
		}
		body.SaveTimeValue = exact
		if core.ScalarRepresentation[T]() == logicaltime.HLAfloat64Time {
			body.SaveTime = &legacy
		}
	}
	return &rtiv1.FederateEvent{Seq: event.Seq(), Event: &rtiv1.FederateEvent_SaveInitiate{SaveInitiate: body}}, nil
}
