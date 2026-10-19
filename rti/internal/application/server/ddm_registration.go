package server

import (
	"context"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func regionRegistrar(manager core.DataDistributionManagement) core.ObjectRegionRegistrar {
	registrar, _ := manager.(core.ObjectRegionRegistrar)
	return registrar
}

type regionAssociationValidatorFunc func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (core.ObjectClassHandle, func(), error)

func (f regionAssociationValidatorFunc) PrepareRegionAssociation(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, func(), error) {
	return f(ctx, fed, caller, obj, attrs)
}
