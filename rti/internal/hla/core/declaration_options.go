package core

import (
	"context"
	"errors"
	"fmt"
)

// SubscriptionOptions is explicit: callers of the legacy API use Active=true.
type SubscriptionOptions struct {
	Active               bool
	UpdateRateDesignator string
}

type AdvisorySwitchKind uint8

const (
	AdvisoryObjectClassRelevance AdvisorySwitchKind = iota + 1
	AdvisoryAttributeRelevance
	AdvisoryAttributeScope
	AdvisoryInteractionRelevance
)

var ErrInvalidUpdateRateDesignator = errors.New("declaration: invalid update rate designator")
var ErrInvalidAdvisorySwitch = errors.New("declaration: invalid advisory switch")

type AdvisorySwitchAlreadySetError struct {
	Kind    AdvisorySwitchKind
	Enabled bool
}

func (e *AdvisorySwitchAlreadySetError) Error() string {
	return fmt.Sprintf("declaration: advisory switch %d already %t", e.Kind, e.Enabled)
}

type DeclarationSubscriptionOptions interface {
	SubscribeObjectClassAttributesWithOptions(context.Context, FederationName, FederateHandle, ObjectClassHandle, []AttributeHandle, SubscriptionOptions) error
	SubscribeInteractionClassWithOptions(context.Context, FederationName, FederateHandle, InteractionClassHandle, bool) error
}

type AdvisorySwitchReader interface {
	AdvisorySwitchEnabled(FederationName, FederateHandle, AdvisorySwitchKind) bool
}

type AdvisorySwitchManagement interface {
	AdvisorySwitchReader
	SetAdvisorySwitch(context.Context, FederationName, FederateHandle, AdvisorySwitchKind, bool) error
}

// Optional metadata ports on a FOMHandle. Unavailable metadata is not inferred.
type FOMUpdateRates interface{ UpdateRateValue(string) (float64, bool) }
type FOMAdvisoryDefaults interface {
	AdvisorySwitchDefault(AdvisorySwitchKind) (bool, bool)
}

// Class ancestry includes the class itself and its ancestors, nearest first.
type FOMClassAncestry interface {
	ObjectClassAncestors(ObjectClassHandle) []ObjectClassHandle
	InteractionClassAncestors(InteractionClassHandle) []InteractionClassHandle
}
