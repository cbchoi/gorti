package core

import "context"

// The object pin keeps knowledge, registrations and deletions stable until the
// declaration and ownership commits finish. Selection uses class-relative input
// handles and returns registered-class handles for the ownership manager.
type PreparedObjectClassUnpublication interface {
	SelectAttributes([]AttributeHandle) map[ObjectHandle][]AttributeHandle
	ProjectKnownObjectAttributes(context.Context, FederationName, FederateHandle, ObjectHandle, []AttributeHandle) (ObjectClassHandle, ObjectClassHandle, []AttributeHandle, error)
	Release()
}

type PreparedOwnershipUnpublicationProjection interface {
	PrepareCallbackProjections(func(FederateHandle, ObjectHandle, []AttributeHandle) (ObjectClassHandle, ObjectClassHandle, []AttributeHandle, error)) error
}

type ObjectClassUnpublicationPreparer interface {
	PrepareObjectClassUnpublication(context.Context, FederationName, FederateHandle, ObjectClassHandle, []AttributeHandle) (PreparedObjectClassUnpublication, error)
}

// Omission and a supplied empty attribute set have different IEEE 1516.1-2010
// section 5.3 meanings. The legacy combined method remains a compatibility API.
type DeclarationObjectUnpublication interface {
	UnpublishObjectClass(context.Context, FederationName, FederateHandle, ObjectClassHandle) error
	UnpublishObjectClassAttributeSet(context.Context, FederationName, FederateHandle, ObjectClassHandle, []AttributeHandle) error
}
