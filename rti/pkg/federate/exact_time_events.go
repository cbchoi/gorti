package federate

import (
	codec "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"math"
)

// CallbackProtocolError rejects malformed or mixed time representations before
// mutating SDK time/object state. It is not an HLA callback from the RTI.
type CallbackProtocolError struct{ Err error }

func (CallbackProtocolError) isFederateEvent() {}
func (e CallbackProtocolError) Error() string  { return e.Err.Error() }
func (e CallbackProtocolError) Unwrap() error  { return e.Err }

func (f *Federate) wireEventTime(event *rtiv1.FederateEvent) (*logicaltime.Time, error) {
	switch v := event.Event.(type) {
	case *rtiv1.FederateEvent_Reflect:
		if v.Reflect == nil {
			return nil, nil
		}
		return f.decodeEventOptionalTime(v.Reflect.LogicalTime, v.Reflect.LogicalTimeValue)
	case *rtiv1.FederateEvent_Receive:
		if v.Receive == nil {
			return nil, nil
		}
		return f.decodeEventOptionalTime(v.Receive.LogicalTime, v.Receive.LogicalTimeValue)
	case *rtiv1.FederateEvent_Remove:
		if v.Remove == nil {
			return nil, nil
		}
		return f.decodeEventOptionalTime(v.Remove.LogicalTime, v.Remove.LogicalTimeValue)
	case *rtiv1.FederateEvent_Grant:
		if v.Grant == nil {
			return nil, nil
		}
		return f.decodeEventRequiredTime(v.Grant.LogicalTime, v.Grant.LogicalTimeValue)
	case *rtiv1.FederateEvent_TimeRegulationEnabled:
		if v.TimeRegulationEnabled == nil {
			return nil, nil
		}
		return f.decodeEventRequiredTime(v.TimeRegulationEnabled.LogicalTime, v.TimeRegulationEnabled.LogicalTimeValue)
	case *rtiv1.FederateEvent_TimeConstrainedEnabled:
		if v.TimeConstrainedEnabled == nil {
			return nil, nil
		}
		return f.decodeEventRequiredTime(v.TimeConstrainedEnabled.LogicalTime, v.TimeConstrainedEnabled.LogicalTimeValue)
	case *rtiv1.FederateEvent_SaveInitiate:
		if v.SaveInitiate == nil {
			return nil, nil
		}
		return f.decodeEventOptionalTime(v.SaveInitiate.SaveTime, v.SaveInitiate.SaveTimeValue)
	default:
		return nil, nil
	}
}

func (f *Federate) legacyFinalTime(legacy float64, exact *rtiv1.LogicalTimeValue) bool {
	return f.timeRepresentation() == logicaltime.HLAfloat64Time && exact == nil && math.IsInf(legacy, 1)
}

func (f *Federate) decodeEventOptionalTime(legacy *float64, exact *rtiv1.LogicalTimeValue) (*logicaltime.Time, error) {
	if legacy != nil && f.legacyFinalTime(*legacy, exact) {
		return nil, nil
	}
	return codec.DecodeOptionalLogicalTimePair(f.timeRepresentation(), legacy, exact)
}

func attachEventTime(event Event, value *logicaltime.Time) Event {
	switch e := event.(type) {
	case TimeAdvanceGrant:
		if value != nil {
			e.TimeValue = *value
		}
		return e
	case TimeRegulationEnabled:
		if value != nil {
			e.TimeValue = *value
		}
		return e
	case TimeConstrainedEnabled:
		if value != nil {
			e.TimeValue = *value
		}
		return e
	case ReflectAttributeValues:
		e.TimestampValue = value
		return e
	case ReflectAttributeValuesByHandle:
		e.TimestampValue = value
		return e
	case ReceiveInteraction:
		e.TimestampValue = value
		return e
	case ReceiveInteractionByHandle:
		e.TimestampValue = value
		return e
	case RemoveObjectInstance:
		e.TimestampValue = value
		return e
	case InitiateFederateSave:
		e.SaveTimeValue = value
		return e
	default:
		return event
	}
}

func (f *Federate) decodeEventRequiredTime(legacy float64, exact *rtiv1.LogicalTimeValue) (*logicaltime.Time, error) {
	if f.legacyFinalTime(legacy, exact) {
		return nil, nil
	}
	value, err := codec.DecodeLogicalTimePair(f.timeRepresentation(), legacy, exact)
	return &value, err
}
