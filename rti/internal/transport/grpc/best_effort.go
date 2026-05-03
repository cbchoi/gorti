package grpc

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/object"
)

// FOMOrderResolver is the optional richer FOMHandle contract a
// concrete fom handle implementation may satisfy to expose
// per-attribute / per-interaction declared order. The production
// `fomHandle` in `rti/cmd/rtid/foms.go` may implement this on top of
// its underlying *model.FOM; spec tests can extend `permissiveFOMHandle`
// the same way (see `rti/spec/M5/fixtures.go`).
//
// Order semantics match object.Order: 0 = TimeStamp, 1 = Receive.
//
// The interface is consumed by FOMRepoOrderLookup below to provide
// an object.AttributeOrderLookup that delegates into the FOM
// repository without requiring a contract change to core.FOMHandle.
type FOMOrderResolver interface {
	OrderForAttribute(cls core.ObjectClassHandle, attr core.AttributeHandle) (object.Order, bool)
	OrderForInteraction(cls core.InteractionClassHandle) (object.Order, bool)
}

// FOMRepoOrderLookup adapts a core.FOMRepository to an
// object.AttributeOrderLookup. It looks up the per-federation FOM
// handle from the repo, asserts it implements FOMOrderResolver, and
// delegates the per-attribute / per-interaction order question.
//
// When the repo's handle does NOT implement FOMOrderResolver — true
// today for the cut-1 fomHandle in rti/cmd/rtid/foms.go — every lookup
// returns (OrderTimeStamp, false). The object registry then treats the
// attribute / interaction as TimeStamp-order and preserves TSO
// delivery, matching the pre-TASK-077 behavior. A future cut can
// upgrade the production fomHandle to implement FOMOrderResolver
// (reading the existing model.Attribute.Order / model.InteractionClass.Order
// fields the parser already populates) without touching this adapter.
type FOMRepoOrderLookup struct {
	Repo core.FOMRepository
}

// AttributeOrder satisfies object.AttributeOrderLookup.
func (f FOMRepoOrderLookup) AttributeOrder(fed core.FederationName, cls core.ObjectClassHandle, attr core.AttributeHandle) (object.Order, bool) {
	if f.Repo == nil {
		return object.OrderTimeStamp, false
	}
	h, err := f.Repo.Get(context.Background(), fed)
	if err != nil || h == nil {
		return object.OrderTimeStamp, false
	}
	resolver, ok := h.(FOMOrderResolver)
	if !ok {
		return object.OrderTimeStamp, false
	}
	return resolver.OrderForAttribute(cls, attr)
}

// InteractionOrder satisfies object.AttributeOrderLookup.
func (f FOMRepoOrderLookup) InteractionOrder(fed core.FederationName, cls core.InteractionClassHandle) (object.Order, bool) {
	if f.Repo == nil {
		return object.OrderTimeStamp, false
	}
	h, err := f.Repo.Get(context.Background(), fed)
	if err != nil || h == nil {
		return object.OrderTimeStamp, false
	}
	resolver, ok := h.(FOMOrderResolver)
	if !ok {
		return object.OrderTimeStamp, false
	}
	return resolver.OrderForInteraction(cls)
}

// Compile-time assertion that FOMRepoOrderLookup satisfies the lookup
// interface required by object.Options.Orders.
var _ object.AttributeOrderLookup = FOMRepoOrderLookup{}
