package server

import (
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type elapsedMetricSource interface {
	MeasureElapsed(core.FederationName, core.FederateHandle) (timepkg.ElapsedMeasurement, error)
	AcknowledgeElapsed(core.FederationName, core.FederateHandle, timepkg.ElapsedMeasurement, timepkg.ElapsedAttributes) error
}

func typedMOMLogicalTime[T core.LogicalTimeScalar](value *T) mom.LogicalTimeMetric {
	if value == nil {
		return mom.LogicalTimeMetric{}
	}
	if core.ScalarRepresentation[T]() == core.ScalarRepresentation[core.LogicalTime]() {
		return mom.LogicalTimeMetric{Defined: true, Value: core.LogicalTime(*value)}
	}
	exact, err := core.ScalarTimeValue(*value)
	if err != nil {
		return mom.LogicalTimeMetric{}
	}
	encoded, err := exact.MarshalBinary()
	if err != nil {
		return mom.LogicalTimeMetric{}
	}
	return mom.LogicalTimeMetric{Defined: true, ExactValue: encoded}
}

func typedMOMTimeMetrics[T core.LogicalTimeScalar](manager *timepkg.ManagerOf[T], fed core.FederationName, h core.FederateHandle) (*mom.TimeMetrics, *uint64) {
	actual := manager.Metrics(fed, h)
	result := &mom.TimeMetrics{Regulating: actual.Regulating, Constrained: actual.Constrained, AsynchronousDelivery: actual.AsynchronousDelivery, Advancing: actual.Advancing}
	if core.ScalarRepresentation[T]() == core.ScalarRepresentation[core.LogicalTime]() {
		result.LogicalTime = core.LogicalTime(actual.CurrentTime)
	} else {
		exact, err := core.ScalarTimeValue(actual.CurrentTime)
		if err == nil {
			result.LogicalTimeValue = &exact
		}
	}
	result.Lookahead = typedMOMLogicalTime(actual.Lookahead)
	if !actual.GALTUnbounded {
		result.GALT = typedMOMLogicalTime(actual.GALT)
	} else if core.ScalarRepresentation[T]() == core.ScalarRepresentation[core.LogicalTime]() {
		result.GALT = typedMOMLogicalTime(actual.GALT)
	}
	if !actual.LITSUnbounded {
		result.LITS = typedMOMLogicalTime(actual.LITS)
	} else if core.ScalarRepresentation[T]() == core.ScalarRepresentation[core.LogicalTime]() {
		result.LITS = typedMOMLogicalTime(actual.LITS)
	}
	result.TSOQueued = &actual.TSOQueued
	return result, &actual.ROQueued
}
