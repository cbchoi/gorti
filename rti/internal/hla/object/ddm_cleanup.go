package object

import "github.com/cbchoi/gorti/rti/internal/hla/core"

func (r *RegistryOf[T]) forgetObjectRegions(fed core.FederationName, obj core.ObjectHandle) {
	if cleanup, ok := r.opts.RegistrationRegions.(interface {
		ForgetObject(core.FederationName, core.ObjectHandle)
	}); ok {
		cleanup.ForgetObject(fed, obj)
	}
}

func (r *RegistryOf[T]) forgetObjectRegionAssociations(fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle) {
	if cleanup, ok := r.opts.RegistrationRegions.(interface {
		ForgetObjectFederateAssociations(core.FederationName, core.FederateHandle, core.ObjectHandle)
	}); ok {
		cleanup.ForgetObjectFederateAssociations(fed, caller, obj)
	}
}
