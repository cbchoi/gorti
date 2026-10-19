package core

import "context"

type OwnershipUnpublicationGuard interface {
	PrepareUnpublication(context.Context, FederationName, FederateHandle, map[ObjectHandle][]AttributeHandle) (func(), error)
}

type PreparedOwnershipUnpublication interface {
	Commit()
	Release()
	Dispatch(context.Context) error
}

type OwnershipUnpublicationCoordinator interface {
	PrepareUnpublicationCommit(context.Context, FederationName, FederateHandle, map[ObjectHandle][]AttributeHandle) (PreparedOwnershipUnpublication, error)
}
