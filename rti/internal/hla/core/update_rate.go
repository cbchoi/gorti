package core

import (
	"context"
	"errors"
)

var ErrUpdateRateValueUnavailable = errors.New("update-rate value unavailable: no named active demand")

// UpdateRateDemand describes producer advice, not a transport filter. Default
// means an active subscriber did not request a named limit. Rate is meaningful
// only when Relevant && !Default; an actual named zero rate remains distinct.
type UpdateRateDemand struct {
	Relevant   bool
	Default    bool
	Designator string
	Rate       float64
}

type UpdateRateAdvisory struct {
	Sequence             uint64
	Object               ObjectHandle
	Attributes           []AttributeHandle
	Enabled              bool
	UpdateRateDesignator *string
}

func (e *UpdateRateAdvisory) Seq() uint64 { return e.Sequence }

type AttributeUpdateRateReader interface {
	EffectiveAttributeUpdateRate(FederationName, ObjectHandle, AttributeHandle) (UpdateRateDemand, bool)
}

// Lifecycle calls commit mandatory owner/object changes even when callbacks
// cannot be admitted. Such callbacks remain pending for a later retry.
type ObjectUpdateRateLifecycle interface {
	SyncObjectUpdateRateOwners(context.Context, FederationName, ObjectHandle, ObjectClassHandle, map[AttributeHandle]FederateHandle) error
	RemoveObjectUpdateRate(context.Context, FederationName, ObjectHandle) error
}

type AttributeUpdateRateQuery interface {
	GetUpdateRateValueForAttribute(context.Context, FederationName, FederateHandle, ObjectHandle, AttributeHandle) (float64, error)
}

type RegionalUpdateRateSubscription struct {
	Object     ObjectHandle
	Attribute  AttributeHandle
	Subscriber FederateHandle
	Options    SubscriptionOptions
}

type RegionalUpdateRateSubscriptions interface {
	SetRegionalUpdateRateSubscriptions(context.Context, FederationName, []RegionalUpdateRateSubscription) error
}
