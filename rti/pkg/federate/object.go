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

// LocalDeleteObjectInstance — IEEE 1516.1-2010 §6.18 (M23 W2).
//
// Federate-local cleanup; no peer notification. Cut-1 simplification:
// the server records the event but does NOT mutate global instance
// state — other subscribers continue to see the instance.
func (f *Federate) LocalDeleteObjectInstance(ctx context.Context, obj uint64) error {
	_, err := f.conn.obj.LocalDeleteObjectInstance(ctx, &rtiv1.LocalDeleteObjectInstanceRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
		ObjectHandle:   obj,
	})
	return wrapStatusErr(err)
}

// RequestAttributeValueUpdate — IEEE 1516.1-2010 §6.24 (M23 W2).
//
// Asks the owner of obj to emit fresh values for the listed
// attributes. Owner receives a ProvideAttributeValueUpdate event;
// it's expected to respond with UpdateAttributeValues.
func (f *Federate) RequestAttributeValueUpdate(ctx context.Context, obj uint64, attrs []uint64, tag []byte) error {
	_, err := f.conn.obj.RequestAttributeValueUpdate(ctx, &rtiv1.RequestAttributeValueUpdateRequest{
		WireVersion:      rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:   f.federationName,
		FederateHandle:   f.federateHandle,
		ObjectHandle:     obj,
		AttributeHandles: append([]uint64(nil), attrs...),
		UserSuppliedTag:  append([]byte(nil), tag...),
	})
	return wrapStatusErr(err)
}

// RequestClassAttributeValueUpdate — IEEE 1516.1-2010 §6.25 (M23 W2).
//
// Class-scoped variant: every unique owner of an instance of cls
// receives a ProvideAttributeValueUpdate event.
func (f *Federate) RequestClassAttributeValueUpdate(ctx context.Context, cls uint64, attrs []uint64, tag []byte) error {
	_, err := f.conn.obj.RequestClassAttributeValueUpdate(ctx, &rtiv1.RequestClassAttributeValueUpdateRequest{
		WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:    f.federationName,
		FederateHandle:    f.federateHandle,
		ObjectClassHandle: cls,
		AttributeHandles:  append([]uint64(nil), attrs...),
		UserSuppliedTag:   append([]byte(nil), tag...),
	})
	return wrapStatusErr(err)
}
