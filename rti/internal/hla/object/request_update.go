package object

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func (r *RegistryOf[T]) RequestAttributeValueUpdate(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, tag []byte) error {
	if obj == 0 {
		return core.ErrObjectHandleInvalid
	}
	return r.requestValueUpdates(ctx, fed, caller, obj, 0, attrs, nil, tag, false)
}

func (r *RegistryOf[T]) RequestClassAttributeValueUpdate(ctx context.Context, fed core.FederationName, caller core.FederateHandle, cls core.ObjectClassHandle, attrs []core.AttributeHandle, tag []byte) error {
	return r.requestValueUpdates(ctx, fed, caller, 0, cls, attrs, nil, tag, false)
}
