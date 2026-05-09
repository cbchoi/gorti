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

// TransportType — M23 W3. Mirrors rti.v1.TransportationType. Use
// TransportTypeReliable / TransportTypeBestEffort with the Change*
// methods. TransportTypeUnspecified (the zero value) is rejected.
type TransportType uint8

const (
	// TransportTypeUnspecified — zero value; rejected by the manager.
	TransportTypeUnspecified TransportType = iota
	TransportTypeReliable
	TransportTypeBestEffort
)

func (t TransportType) wire() rtiv1.TransportationType {
	switch t {
	case TransportTypeReliable:
		return rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE
	case TransportTypeBestEffort:
		return rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT
	default:
		return rtiv1.TransportationType_TRANSPORTATION_TYPE_UNSPECIFIED
	}
}

// ChangeAttributeTransportationType — IEEE 1516.1-2010 §6.20 (M23 W3).
// Per-instance per-attribute transport override. Owner-only; recorded
// at the manager but the wire path doesn't yet route per-message
// transport (record-only in M23, per the dispatch plan).
func (f *Federate) ChangeAttributeTransportationType(ctx context.Context, obj uint64, attrs []uint64, tt TransportType) error {
	_, err := f.conn.obj.ChangeAttributeTransportationType(ctx, &rtiv1.ChangeAttributeTransportRequest{
		WireVersion:      rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:   f.federationName,
		FederateHandle:   f.federateHandle,
		ObjectHandle:     obj,
		AttributeHandles: append([]uint64(nil), attrs...),
		TransportType:    tt.wire(),
	})
	return wrapStatusErr(err)
}

// ChangeInteractionTransportationType — IEEE 1516.1-2010 §6.22 (M23 W3).
// Per-publisher per-class transport override.
func (f *Federate) ChangeInteractionTransportationType(ctx context.Context, cls uint64, tt TransportType) error {
	_, err := f.conn.obj.ChangeInteractionTransportationType(ctx, &rtiv1.ChangeInteractionTransportRequest{
		WireVersion:            rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:         f.federationName,
		FederateHandle:         f.federateHandle,
		InteractionClassHandle: cls,
		TransportType:          tt.wire(),
	})
	return wrapStatusErr(err)
}
