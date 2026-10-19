package object

import "github.com/cbchoi/gorti/rti/internal/hla/core"

var (
	_ core.ObjectRegistryOf[core.LogicalTime]           = (*Registry)(nil)
	_ core.ObjectRegistryOf[int64]                      = (*IntegerRegistry)(nil)
	_ core.ObjectMetadataManagementOf[core.LogicalTime] = (*Registry)(nil)
	_ core.ObjectMetadataManagementOf[int64]            = (*IntegerRegistry)(nil)
	_ core.WireApplierOf[core.LogicalTime]              = (*Registry)(nil)
	_ core.WireApplierOf[int64]                         = (*IntegerRegistry)(nil)
	_ core.DeletionLifecycle                            = (*IntegerRegistry)(nil)
	_ core.QueuedMessageFilter                          = (*IntegerRegistry)(nil)
	_ core.TransportationManagement                     = (*IntegerRegistry)(nil)
	_ core.TransportationActivation                     = (*IntegerRegistry)(nil)
)
