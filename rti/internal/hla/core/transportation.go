package core

import "context"

// TransportationManagement is the IEEE 1516-2010 transportation service
// extension. Changes complete before return; the language binding queues the
// confirmation/report callback using the successful result.
type TransportationManagement interface {
	RequestAttributeTransportationTypeChange(context.Context, FederationName, FederateHandle, ObjectHandle, []AttributeHandle, TransportType) error
	QueryAttributeTransportationType(context.Context, FederationName, FederateHandle, ObjectHandle, AttributeHandle) (TransportType, error)
	RequestInteractionTransportationTypeChange(context.Context, FederationName, FederateHandle, InteractionClassHandle, TransportType) error
	QueryInteractionTransportationType(context.Context, FederationName, FederateHandle, FederateHandle, InteractionClassHandle) (TransportType, error)
}

// FOMTransportationLookup resolves effective FOM defaults, including inherited
// declarations. Unknown handles return false, never an invented default.
type FOMTransportationLookup interface {
	TransportationForAttribute(ObjectClassHandle, AttributeHandle) (TransportType, bool)
	TransportationForInteraction(InteractionClassHandle) (TransportType, bool)
}
