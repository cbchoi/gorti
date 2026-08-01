// TASK-249 (M23 W1) — Go federate SDK §6 object-management surface.
//
// Mirrors the gRPC ObjectService RPCs onto the Federate type. M23
// adds DeleteObjectInstance; subsequent waves add the §6 additions
// (local_delete, request_attribute_value_update, change_*_transport).

package federate

import (
	"context"
	"errors"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
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
	f.rememberRegisteredObject(objectHandle, classHandle)
	return objectHandle, nil
}

// UpdateAttributeValues updates attributes on a registered object instance.
// Attribute names are resolved against the object's class. timestamp is nil
// for receive-order delivery and non-nil for timestamp-order delivery.
func (f *Federate) UpdateAttributeValues(
	ctx context.Context, objectHandle uint64, attributes map[string][]byte, timestamp *float64,
) error {
	wireAttributes, err := f.resolveAttributeValues(objectHandle, attributes)
	if err != nil {
		return err
	}
	return f.UpdateAttributeValuesByHandle(ctx, objectHandle, wireAttributes, timestamp)
}

// UpdateAttributeValuesConfirmed waits for the RTI server to accept or reject
// the update. It is the explicit synchronous counterpart to the default
// receive-order LocalLRC path.
func (f *Federate) UpdateAttributeValuesConfirmed(
	ctx context.Context, objectHandle uint64, attributes map[string][]byte, timestamp *float64,
) error {
	wireAttributes, err := f.resolveAttributeValues(objectHandle, attributes)
	if err != nil {
		return err
	}
	return f.UpdateAttributeValuesByHandleConfirmed(ctx, objectHandle, wireAttributes, timestamp)
}

func (f *Federate) resolveAttributeValues(
	objectHandle uint64, attributes map[string][]byte,
) (map[uint64][]byte, error) {
	classHandle, ok := f.objectClassFor(objectHandle)
	if !ok {
		return nil, fmt.Errorf("federate: object instance %d class is unknown", objectHandle)
	}
	className, ok := f.handles.objectClassName(classHandle)
	if !ok {
		return nil, fmt.Errorf("federate: object class handle %d not in FOM", classHandle)
	}

	wireAttributes := make(map[uint64][]byte, len(attributes))
	for name, payload := range attributes {
		attributeHandle, found := f.handles.attributeHandle(className, name)
		if !found {
			return nil, fmt.Errorf("federate: attribute %q not in object class %q", name, className)
		}
		wireAttributes[attributeHandle] = append([]byte(nil), payload...)
	}
	return wireAttributes, nil
}

// UpdateAttributeValuesByHandle updates attributes using handles resolved
// before the call. Receive-order payloads are copied into LocalLRC before the
// call returns; confirmed and timestamp-order calls retain synchronous rules.
func (f *Federate) UpdateAttributeValuesByHandle(
	ctx context.Context, objectHandle uint64, attributes map[uint64][]byte, timestamp *float64,
) error {
	if timestamp == nil && f.useLocalLRCForReceiveOrder() {
		_, err := f.QueueAttributeValuesByHandle(ctx, objectHandle, attributes)
		if !errors.Is(err, ErrLocalLRCUnavailable) {
			return err
		}
	}
	return f.UpdateAttributeValuesByHandleConfirmed(ctx, objectHandle, attributes, timestamp)
}

// UpdateAttributeValuesByHandleConfirmed waits for a server result and does
// not use LocalLRC, even for receive-order traffic.
func (f *Federate) UpdateAttributeValuesByHandleConfirmed(
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
	if f.interactionClosing.Load() {
		return ErrNotJoined
	}
	f.interactionStreamMu.Lock()
	defer f.interactionStreamMu.Unlock()
	if f.interactionClosing.Load() {
		return ErrNotJoined
	}
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	if f.interactionContext != nil {
		select {
		case <-f.interactionContext.Done():
			return ErrNotJoined
		default:
		}
	}
	confirmed := f.sendConfirmedObjectStreamLocked(ctx, &rtiv1.ConfirmedObjectRequest{
		Operation: &rtiv1.ConfirmedObjectRequest_AttributeUpdate{AttributeUpdate: req},
	})
	f.recordConfirmedObjectOutcome(confirmed)
	if confirmed.handled {
		return confirmed.err
	}
	unaryCtx, finishUnary := f.confirmedUnaryContext(ctx)
	f.confirmedObjectStats.unarySent.Add(1)
	_, err := f.conn.obj.UpdateAttributeValues(unaryCtx, req, grpc.MaxRetryRPCBufferSize(0))
	finishUnary()
	if err == nil {
		f.confirmedObjectStats.unaryAcked.Add(1)
	} else if interactionResultIndeterminate(err) {
		f.confirmedObjectStats.indeterminate.Add(1)
	}
	return wrapStatusErr(err)
}

// UpdateAttributes is a compatibility alias for UpdateAttributeValues.
func (f *Federate) UpdateAttributes(
	ctx context.Context, objectHandle uint64, attributes map[string][]byte, timestamp *float64,
) error {
	return f.UpdateAttributeValues(ctx, objectHandle, attributes, timestamp)
}

// UpdateAttributesConfirmed is a compatibility alias for
// UpdateAttributeValuesConfirmed.
func (f *Federate) UpdateAttributesConfirmed(
	ctx context.Context, objectHandle uint64, attributes map[string][]byte, timestamp *float64,
) error {
	return f.UpdateAttributeValuesConfirmed(ctx, objectHandle, attributes, timestamp)
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
	delete(f.ownedObjectAttributes, objectHandle)
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
