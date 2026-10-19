package core

import (
	"context"
	"errors"
)

var ErrRetractableDeletionUnsupported = errors.New("retractable object deletion requires reversible object lifecycle support")

type OrderType uint8

const (
	OrderUnspecified OrderType = iota
	OrderReceive
	OrderTimestamp
)

// Zero scalar values and nil Regions represent unavailable metadata.
type ObjectCallbackMetadata struct {
	SentOrder                OrderType
	ReceivedOrder            OrderType
	Transportation           TransportType
	Producer                 FederateHandle
	RetractionHandle         uint64
	Regions                  []DDMRegionHandleCore
	ConveyedRegions          []ConveyedRegionCopy                     `json:"conveyedRegions"`
	AttributeConveyedRegions map[AttributeHandle][]ConveyedRegionCopy `json:"attributeConveyedRegions,omitempty"`
	MOMAttributePoint        *MOMAttributePoint                       `json:"momAttributePoint,omitempty"`
	// DelayedSubscription retains canonical class-local payload handles until
	// delivery eligibility. This is internal snapshot metadata, not a wire flag.
	DelayedSubscription bool              `json:"delayedSubscription,omitempty"`
	RegisteredClass     ObjectClassHandle `json:"registeredClass,omitempty"`
}

type ObjectMessageOptions struct {
	Tag              []byte
	RetractionHandle uint64
}

type ObjectMetadataManagement = ObjectMetadataManagementOf[LogicalTime]

type ObjectMetadataManagementOf[T LogicalTimeScalar] interface {
	UpdateAttributesWithMetadata(context.Context, FederationName, FederateHandle, ObjectHandle, map[AttributeHandle][]byte, *T, ObjectMessageOptions) error
	SendInteractionWithMetadata(context.Context, FederationName, FederateHandle, InteractionClassHandle, map[ParameterHandle][]byte, *T, ObjectMessageOptions) error
	DeleteWithMetadata(context.Context, FederationName, FederateHandle, ObjectHandle, *T, ObjectMessageOptions) error
}

type OrderManagement interface {
	ChangeAttributeOrderType(context.Context, FederationName, FederateHandle, ObjectHandle, []AttributeHandle, OrderType) error
	ChangeInteractionOrderType(context.Context, FederationName, FederateHandle, InteractionClassHandle, OrderType) error
}

// Time delivery may supply classification at its existing admission boundary.
type ReceivedOrderEvent interface{ SetReceivedOrder(OrderType) }
