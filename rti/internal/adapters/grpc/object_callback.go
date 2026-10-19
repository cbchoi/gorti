package grpc

import (
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	objectpkg "github.com/cbchoi/gorti/rti/internal/hla/object"
)

func encodeObjectCallback[T core.LogicalTimeScalar](v *objectpkg.CallbackEventOf[T]) (*rtiv1.FederateEvent, error) {
	if v == nil {
		return nil, errOutboundEventNotConvertible
	}
	out := &rtiv1.FederateEvent{Seq: v.Seq()}
	attrs := make([]uint64, len(v.AttributeHandles))
	for i, h := range v.AttributeHandles {
		attrs[i] = uint64(h)
	}
	logicalTime, exact, err := encodeOptionalObjectTime(v.LogicalTime)
	if err != nil {
		return nil, err
	}
	switch v.Kind {
	case objectpkg.CallbackDiscover:
		out.Event = &rtiv1.FederateEvent_Discover{Discover: encodeObjectDiscovery(v)}
	case objectpkg.CallbackReflect:
		out.Event = &rtiv1.FederateEvent_Reflect{Reflect: &rtiv1.ReflectAttributeValues{ObjectHandle: uint64(v.Object), ObjectClassHandle: uint64(v.ObjectClass), Attributes: v.Attributes, LogicalTime: logicalTime, LogicalTimeValue: exact, UserSuppliedTag: append([]byte(nil), v.UserSuppliedTag...), Metadata: callbackMetadata(v.Metadata)}}
	case objectpkg.CallbackReceive:
		out.Event = &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{InteractionClassHandle: uint64(v.InteractionClass), Parameters: v.Parameters, LogicalTime: logicalTime, LogicalTimeValue: exact, UserSuppliedTag: append([]byte(nil), v.UserSuppliedTag...), Metadata: callbackMetadata(v.Metadata)}}
	case objectpkg.CallbackRemove:
		out.Event = &rtiv1.FederateEvent_Remove{Remove: &rtiv1.RemoveObjectInstance{ObjectHandle: uint64(v.Object), LogicalTime: logicalTime, LogicalTimeValue: exact, UserSuppliedTag: append([]byte(nil), v.UserSuppliedTag...), Metadata: callbackMetadata(v.Metadata)}}
	case objectpkg.CallbackProvideUpdate:
		out.Event = &rtiv1.FederateEvent_ProvideUpdate{ProvideUpdate: &rtiv1.ProvideAttributeValueUpdate{ObjectHandle: uint64(v.Object), AttributeHandles: attrs, UserSuppliedTag: append([]byte(nil), v.UserSuppliedTag...)}}
	case objectpkg.CallbackAttributesInScope:
		out.Event = &rtiv1.FederateEvent_AttributesInScope{AttributesInScope: &rtiv1.AttributesInScope{ObjectHandle: uint64(v.Object), AttributeHandles: attrs}}
	case objectpkg.CallbackAttributesOutOfScope:
		out.Event = &rtiv1.FederateEvent_AttributesOutOfScope{AttributesOutOfScope: &rtiv1.AttributesOutOfScope{ObjectHandle: uint64(v.Object), AttributeHandles: attrs}}
	case objectpkg.CallbackReservationSucceeded:
		out.Event = &rtiv1.FederateEvent_ReservationSucceeded{ReservationSucceeded: &rtiv1.ObjectInstanceNameReservationSucceeded{ObjectName: v.ObjectName}}
	case objectpkg.CallbackReservationFailed:
		out.Event = &rtiv1.FederateEvent_ReservationFailed{ReservationFailed: &rtiv1.ObjectInstanceNameReservationFailed{ObjectName: v.ObjectName}}
	case objectpkg.CallbackMultipleReservationSucceeded:
		out.Event = &rtiv1.FederateEvent_ReservationMultiSucceeded{ReservationMultiSucceeded: &rtiv1.MultipleObjectInstanceNameReservationSucceeded{ObjectNames: append([]string(nil), v.ObjectNames...)}}
	case objectpkg.CallbackMultipleReservationFailed:
		out.Event = &rtiv1.FederateEvent_ReservationMultiFailed{ReservationMultiFailed: &rtiv1.MultipleObjectInstanceNameReservationFailed{RequestedNames: append([]string(nil), v.ObjectNames...), CollidingNames: append([]string(nil), v.CollidingNames...)}}
	default:
		return nil, errOutboundEventNotConvertible
	}
	return out, nil
}

func encodeObjectDiscovery[T core.LogicalTimeScalar](v *objectpkg.CallbackEventOf[T]) *rtiv1.DiscoverObjectInstance {
	out := &rtiv1.DiscoverObjectInstance{ObjectHandle: uint64(v.Object), ObjectClassHandle: uint64(v.ObjectClass), ObjectName: v.ObjectName}
	if v.Metadata != nil && v.Metadata.Producer != 0 {
		producer := uint64(v.Metadata.Producer)
		out.ProducingFederate = &producer
	}
	return out
}
