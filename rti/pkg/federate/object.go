// TASK-249 (M23 W1) — Go federate SDK §6 object-management surface.
//
// Mirrors the gRPC ObjectService RPCs onto the Federate type. M23
// adds DeleteObjectInstance; subsequent waves add the §6 additions
// (local_delete, request_attribute_value_update, change_*_transport).

package federate

import (
	"context"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// ReserveObjectInstanceName requests exclusive reservation of objectName.
// Completion is reported asynchronously as an
// ObjectInstanceNameReservationSucceeded or ObjectInstanceNameReservationFailed
// event.
func (f *Federate) ReserveObjectInstanceName(ctx context.Context, objectName string) error {
	_, err := f.conn.obj.ReserveObjectInstanceName(ctx, &rtiv1.ReserveObjectInstanceNameRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
		ObjectName:     objectName,
	})
	return wrapStatusErr(err)
}

// RegisterObjectInstance registers an instance of the named object class and
// returns the RTI-assigned object handle. objectName may be empty to request an
// RTI-generated name.
func (f *Federate) RegisterObjectInstance(ctx context.Context, className, objectName string) (uint64, error) {
	classHandle, ok := f.handles.objectClassHandle(className)
	if !ok {
		return 0, fmt.Errorf("federate: object class %q not in FOM", className)
	}
	resp, err := f.conn.obj.RegisterObjectInstance(ctx, &rtiv1.RegisterObjectRequest{
		WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:    f.federationName,
		FederateHandle:    f.federateHandle,
		ObjectClassHandle: classHandle,
		ObjectName:        objectName,
	})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	objectHandle := resp.GetObjectHandle()
	f.rememberObjectClass(objectHandle, classHandle)
	return objectHandle, nil
}

// UpdateAttributeValues updates attributes on a registered object instance.
// Attribute names are resolved against the object's class. timestamp is nil
// for receive-order delivery and non-nil for timestamp-order delivery.
func (f *Federate) UpdateAttributeValues(
	ctx context.Context, objectHandle uint64, attributes map[string][]byte, timestamp *float64,
) error {
	classHandle, ok := f.objectClassFor(objectHandle)
	if !ok {
		return fmt.Errorf("federate: object instance %d class is unknown", objectHandle)
	}
	className, ok := f.handles.objectClassName(classHandle)
	if !ok {
		return fmt.Errorf("federate: object class handle %d not in FOM", classHandle)
	}

	wireAttributes := make(map[uint64][]byte, len(attributes))
	for name, payload := range attributes {
		attributeHandle, found := f.handles.attributeHandle(className, name)
		if !found {
			return fmt.Errorf("federate: attribute %q not in object class %q", name, className)
		}
		wireAttributes[attributeHandle] = append([]byte(nil), payload...)
	}
	return f.UpdateAttributeValuesByHandle(ctx, objectHandle, wireAttributes, timestamp)
}

// UpdateAttributeValuesByHandle updates attributes using handles resolved
// before the call. The caller must not mutate attributes or its payloads until
// the call returns.
func (f *Federate) UpdateAttributeValuesByHandle(
	ctx context.Context, objectHandle uint64, attributes map[uint64][]byte, timestamp *float64,
) error {
	req := &rtiv1.UpdateAttributeValuesRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
		FederateHandle: f.federateHandle,
		ObjectHandle:   objectHandle,
		Attributes:     attributes,
	}
	if timestamp != nil {
		value := *timestamp
		req.LogicalTime = &value
	}
	_, err := f.conn.obj.UpdateAttributeValues(ctx, req)
	return wrapStatusErr(err)
}

// UpdateAttributes is a compatibility alias for UpdateAttributeValues.
func (f *Federate) UpdateAttributes(
	ctx context.Context, objectHandle uint64, attributes map[string][]byte, timestamp *float64,
) error {
	return f.UpdateAttributeValues(ctx, objectHandle, attributes, timestamp)
}

func (f *Federate) rememberObjectClass(objectHandle, classHandle uint64) {
	f.mu.Lock()
	if f.objectClasses == nil {
		f.objectClasses = make(map[uint64]uint64)
	}
	f.objectClasses[objectHandle] = classHandle
	f.mu.Unlock()
}

func (f *Federate) forgetObjectClass(objectHandle uint64) {
	f.mu.Lock()
	delete(f.objectClasses, objectHandle)
	f.mu.Unlock()
}

func (f *Federate) objectClassFor(objectHandle uint64) (uint64, bool) {
	f.mu.Lock()
	classHandle, ok := f.objectClasses[objectHandle]
	f.mu.Unlock()
	return classHandle, ok
}

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
	if err != nil {
		return wrapStatusErr(err)
	}
	f.forgetObjectClass(obj)
	return nil
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
	if err != nil {
		return wrapStatusErr(err)
	}
	f.forgetObjectClass(obj)
	return nil
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
