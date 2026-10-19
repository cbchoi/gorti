package core

import (
	"errors"
	"slices"
)

var ErrServiceInvocationsReportedViaMOM = errors.New("FederateServiceInvocationsAreBeingReportedViaMOM")

type MOMServiceReportingSource interface {
	ServiceReporting(FederationName, FederateHandle) bool
}

// Report subscriptions include ancestors which would receive the same report.
func ValidateMOMReportSubscription(fom FOMHandle, reporting MOMServiceReportingSource, fed FederationName, sub FederateHandle, cls InteractionClassHandle) error {
	if reporting == nil || !reporting.ServiceReporting(fed, sub) || fom == nil {
		return nil
	}
	report, ok := fom.LookupInteractionClass("HLAmanager.HLAfederate.HLAreport.HLAreportServiceInvocation")
	if !ok {
		return nil
	}
	ancestors := []InteractionClassHandle{report}
	if hierarchy, ok := fom.(FOMClassAncestry); ok {
		ancestors = hierarchy.InteractionClassAncestors(report)
	}
	if slices.Contains(ancestors, cls) {
		return ErrServiceInvocationsReportedViaMOM
	}
	return nil
}
