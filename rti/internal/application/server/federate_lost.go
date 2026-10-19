package server

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func reportLostFederateOf[T core.LogicalTimeScalar](ctx context.Context, id core.MemberIdentity, times *timepkg.ManagerOf[T], saves *savepoint.ManagerOf[T], reports *mom.Dispatcher, model core.FOMHandle) error {
	state := times.Metrics(id.Federation, id.Handle)
	var lastGood mom.LogicalTimeMetric
	if state.Regulating {
		lastGood = typedMOMLogicalTime(&state.CurrentTime)
	}
	const description = "authenticated callback session disconnected beyond reconnect grace"
	if saves == nil {
		return reports.ReportFederateLost(ctx, id, lastGood, description, model)
	}
	deliveries, err := reports.FederateLostReportDeliveries(ctx, id, lastGood, description, model)
	if err != nil {
		return err
	}
	return saves.FederateLost(ctx, id.Federation, id.Handle, deliveries...)
}

func (s *runtimeSaveAdmission) AcquireLoss(fed core.FederationName) (func(), error) {
	return s.selected(fed).(interface {
		AcquireLoss(core.FederationName) (func(), error)
	}).AcquireLoss(fed)
}
