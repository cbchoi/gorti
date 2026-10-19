package core

import "context"

// MOMAttributePoint specifies the single HLAfederate axis used by 11.4.1(h).
// It is not an ordinary DDM interval or a mutable region designator.
type MOMAttributePoint struct {
	Subject    FederateHandle
	Dimension  DDMDimensionHandle
	Coordinate uint64
}

// MOMFederateAttributePublisher keeps subject routing out of the MOM services.
// Reflect must validate current interest, discover if needed, then reflect only
// eligible attributes, preserving callback order and reservation atomicity.
type MOMFederateAttributePublisher interface {
	PublishFederateAttributes(context.Context, FederationName, FederateHandle, ObjectHandle, ObjectClassHandle, map[AttributeHandle][]byte) error
	ReflectFederateAttributes(context.Context, FederationName, FederateHandle, FederateHandle, ObjectHandle, ObjectClassHandle, map[AttributeHandle][]byte) error
}

// MOMPointAttributeDelivery is the object delivery boundary for an immutable
// point. Implementations retain the point through queueing, rechecks and saves;
// they must not substitute an ordinary interval or invent a source region ID.
// Data callbacks use the configured receipt-tracking outbox, including targeted
// late reflection; direct queue sends must not bypass callback-entry accounting.
type MOMPointAttributeDelivery interface {
	PublishMOMAttributePoint(context.Context, FederationName, ObjectHandle, ObjectClassHandle, map[AttributeHandle][]byte, MOMAttributePoint) error
	ReflectMOMAttributePoint(context.Context, FederationName, FederateHandle, ObjectHandle, ObjectClassHandle, map[AttributeHandle][]byte, MOMAttributePoint) error
}
