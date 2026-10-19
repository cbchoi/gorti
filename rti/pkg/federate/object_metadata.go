package federate

import (
	"context"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

type OrderType uint8

const (
	OrderUnspecified OrderType = iota
	OrderReceive
	OrderTimestamp
)

// Pointer presence is preserved from the wire; nil is not a default order.
type ObjectCallbackMetadata struct {
	SentOrder               *OrderType
	ReceivedOrder           *OrderType
	TransportationType      *TransportType
	ProducingFederate       *uint64
	MessageRetractionHandle *uint64
	ReceivedRegions         []uint64
}

type MessageOptions struct {
	Tag []byte
	// A caller-allocated sender-local ID; zero disables retraction tracking.
	RetractionHandle uint64
}

func copyUint64(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func metadataFromWire(m *rtiv1.ObjectCallbackMetadata) *ObjectCallbackMetadata {
	if m == nil {
		return nil
	}
	// Preserve the existing low-octet representation of unknown wire enum values.
	out := &ObjectCallbackMetadata{}
	if m.SentOrder != nil {
		v := OrderType(wireEnumOctet(int32(*m.SentOrder)))
		out.SentOrder = &v
	}
	if m.ReceivedOrder != nil {
		v := OrderType(wireEnumOctet(int32(*m.ReceivedOrder)))
		out.ReceivedOrder = &v
	}
	if m.TransportationType != nil {
		v := TransportType(wireEnumOctet(int32(*m.TransportationType)))
		out.TransportationType = &v
	}
	if m.ProducingFederate != nil {
		v := *m.ProducingFederate
		out.ProducingFederate = &v
	}
	if m.MessageRetractionHandle != nil {
		v := *m.MessageRetractionHandle
		out.MessageRetractionHandle = &v
	}
	if m.ReceivedRegions != nil {
		out.ReceivedRegions = append([]uint64{}, m.ReceivedRegions.RegionHandles...)
	}
	return out
}

type TurnUpdatesOnForObjectInstance struct {
	CallbackInfo
	ObjectHandle         uint64
	AttributeHandles     []uint64
	UpdateRateDesignator *string
}

func (TurnUpdatesOnForObjectInstance) isFederateEvent() {}

type TurnUpdatesOffForObjectInstance struct {
	CallbackInfo
	ObjectHandle     uint64
	AttributeHandles []uint64
}

func (TurnUpdatesOffForObjectInstance) isFederateEvent() {}

func (f *Federate) ChangeAttributeOrderType(ctx context.Context, object uint64, attrs []uint64, order OrderType) error {
	_, err := f.conn.obj.ChangeAttributeOrderType(ctx, &rtiv1.ChangeAttributeOrderRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), ObjectHandle: object, AttributeHandles: attrs, OrderType: rtiv1.OrderType(order)})
	return wrapStatusErr(err)
}

func (f *Federate) ChangeInteractionOrderType(ctx context.Context, class uint64, order OrderType) error {
	_, err := f.conn.obj.ChangeInteractionOrderType(ctx, &rtiv1.ChangeInteractionOrderRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), InteractionClassHandle: class, OrderType: rtiv1.OrderType(order)})
	return wrapStatusErr(err)
}

func (f *Federate) UpdateAttributeValuesByHandleWithOptions(ctx context.Context, object uint64, attrs map[uint64][]byte, ts *float64, options MessageOptions) error {
	return f.updateAttributeValuesByHandleConfirmed(ctx, object, attrs, ts, options)
}

func (f *Federate) SendInteractionByHandleWithOptions(ctx context.Context, class uint64, params map[uint64][]byte, ts *float64, options MessageOptions) error {
	return f.sendInteractionByHandleConfirmed(ctx, class, params, ts, options)
}

func (f *Federate) SendInteractionWithRegionsOptions(ctx context.Context, class uint64, params map[uint64][]byte, regions []uint64, ts *float64, options MessageOptions) error {
	legacy, exact, err := f.optionalTimeWire(ts)
	if err != nil {
		return err
	}
	_, err = f.conn.ddm.SendInteractionWithRegions(ctx, &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: f.CurrentHandle(), InteractionClassHandle: class, Parameters: params, RegionHandles: regions, LogicalTime: legacy, LogicalTimeValue: exact, UserSuppliedTag: options.Tag, MessageRetractionHandle: options.RetractionHandle})
	return wrapStatusErr(err)
}

func (f *Federate) Retract(ctx context.Context, handle uint64) error {
	f.deleteRequestMu.Lock()
	defer f.deleteRequestMu.Unlock()
	sender := f.CurrentHandle()
	f.mu.Lock()
	epoch := f.objectKnowledgeEpoch
	f.mu.Unlock()
	_, err := f.conn.obj.Retract(ctx, &rtiv1.RetractRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: sender, MessageRetractionHandle: handle})
	if err == nil {
		f.mu.Lock()
		if f.objectKnowledgeEpoch == epoch && !f.localLRCClosing.Load() {
			f.restoreDeletedObjectLocked(deletionDesignator{sender: sender, id: handle}, true)
		}
		f.mu.Unlock()
	}
	return wrapStatusErr(err)
}

func (f *Federate) DeleteObjectInstanceWithOptions(ctx context.Context, object uint64, ts *float64, options MessageOptions) error {
	return f.deleteObjectInstanceWithTime(ctx, object, ts, options)
}

func (f *Federate) deleteObjectInstanceWithTime(ctx context.Context, object uint64, ts any, options MessageOptions) error {
	timestamp, exact, encodeErr := f.optionalTimeWire(ts)
	if encodeErr != nil {
		return encodeErr
	}
	f.deleteRequestMu.Lock()
	defer f.deleteRequestMu.Unlock()
	sender := f.CurrentHandle()
	f.mu.Lock()
	saved, known := f.snapshotDeletedObjectLocked(object)
	epoch := f.objectKnowledgeEpoch
	f.mu.Unlock()
	_, err := f.conn.obj.DeleteObjectInstance(ctx, &rtiv1.DeleteObjectInstanceRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: sender, ObjectHandle: object, LogicalTime: timestamp, LogicalTimeValue: exact, UserSuppliedTag: append([]byte(nil), options.Tag...), MessageRetractionHandle: options.RetractionHandle})
	if err != nil {
		return wrapStatusErr(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.objectKnowledgeEpoch != epoch || f.localLRCClosing.Load() {
		return nil
	}
	f.discardDeletionKnowledgeLocked(object)
	if known && exact != nil && options.RetractionHandle != 0 {
		// Empty RPC responses do not classify sent order. This candidate can
		// only be restored after the server confirms a successful Retract.
		saved.originated = true
		f.rememberDeletedObjectLocked(deletionDesignator{sender: sender, id: options.RetractionHandle}, saved)
	}
	delete(f.objectClasses, object)
	delete(f.ownedObjectAttributes, object)
	return nil
}

type RequestRetraction struct {
	CallbackInfo
	SenderFederate   uint64
	RetractionHandle uint64
}

func (RequestRetraction) isFederateEvent() {}

// The public SDK enums have always retained the low octet, including unknown
// protobuf enum values. Preserve that representation without rejecting callbacks.
func wireEnumOctet(value int32) uint8 {
	return uint8(value) //nolint:gosec // G115: intentional low-octet representation for the existing uint8 API.
}
