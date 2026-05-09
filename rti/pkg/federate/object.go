// TASK-249 (M23 W1) — Go federate SDK §6 object-management surface.
//
// Mirrors the gRPC ObjectService RPCs onto the Federate type. M23
// adds DeleteObjectInstance; subsequent waves add the §6 additions
// (local_delete, request_attribute_value_update, change_*_transport).

package federate

import (
	"context"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// DeleteObjectInstance — IEEE 1516.1-2010 §6.16 (M23 W1).
//
// Deletes an object instance the federate owns. Subscribers receive a
// RemoveObjectInstance event on their Events() channel.
//
// Errors:
//   - ErrObjectNotOwned if the federate is not the owner
//   - ErrObjectAlreadyDeleted if the instance was already deleted
//
// ts may be nil for RO delete; non-nil for TSO delete with that
// logical timestamp. tag is passed through verbatim to subscribers.
func (f *Federate) DeleteObjectInstance(ctx context.Context, obj uint64, tag []byte, ts *float64) error {
	req := &rtiv1.DeleteObjectInstanceRequest{
		WireVersion:     rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:  f.federationName,
		FederateHandle:  f.federateHandle,
		ObjectHandle:    obj,
		UserSuppliedTag: append([]byte(nil), tag...),
	}
	if ts != nil {
		v := *ts
		req.LogicalTime = &v
	}
	_, err := f.conn.obj.DeleteObjectInstance(ctx, req)
	return wrapStatusErr(err)
}
