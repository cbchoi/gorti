package grpc

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type momSubscriptionGuard struct {
	foms      core.FOMRepository
	reporting core.MOMServiceReportingSource
}

func newMOMSubscriptionGuard(foms core.FOMRepository, management core.ManagementObjectModel) momSubscriptionGuard {
	reporting, _ := management.(core.MOMServiceReportingSource)
	return momSubscriptionGuard{foms: foms, reporting: reporting}
}

func (g momSubscriptionGuard) validate(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.InteractionClassHandle) error {
	if g.foms == nil || g.reporting == nil {
		return nil
	}
	fom, err := g.foms.Get(ctx, fed)
	if err != nil {
		return err
	}
	return core.ValidateMOMReportSubscription(fom, g.reporting, fed, sub, cls)
}
