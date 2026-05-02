package declaration

import (
	"context"
	"errors"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrNotImplemented is returned by stub methods until Agent A implements them.
var ErrNotImplemented = errors.New("declaration: not implemented (Agent A M2 deliverable)")

// Manager holds per-federation pub/sub matrices and answers
// SubscribersFor / PublishersFor queries in deterministic handle order.
type Manager struct {
	// internal state declared by Agent A in implementation
}

// New constructs a Manager. No external dependencies — Manager is pure.
func New() *Manager {
	return &Manager{}
}

// PublishObjectClassAttributes records that federate `pub` publishes the
// given attributes of `cls` in `fed`. Idempotent — repeated calls do not
// duplicate publications.
func (m *Manager) PublishObjectClassAttributes(
	ctx context.Context,
	fed core.FederationName,
	pub core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
) error {
	_ = ctx
	_ = fed
	_ = pub
	_ = cls
	_ = attrs
	return ErrNotImplemented
}

// UnpublishObjectClassAttributes removes federate `pub`'s publication of
// the listed attributes. Removing a non-existent publication is a no-op.
func (m *Manager) UnpublishObjectClassAttributes(
	ctx context.Context,
	fed core.FederationName,
	pub core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
) error {
	_ = ctx
	_ = fed
	_ = pub
	_ = cls
	_ = attrs
	return ErrNotImplemented
}

// SubscribeObjectClassAttributes records federate `sub`'s subscription.
func (m *Manager) SubscribeObjectClassAttributes(
	ctx context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
) error {
	_ = ctx
	_ = fed
	_ = sub
	_ = cls
	_ = attrs
	return ErrNotImplemented
}

// UnsubscribeObjectClassAttributes is the symmetric remover.
func (m *Manager) UnsubscribeObjectClassAttributes(
	ctx context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
) error {
	_ = ctx
	_ = fed
	_ = sub
	_ = cls
	_ = attrs
	return ErrNotImplemented
}

// PublishInteractionClass records federate `pub` publishes interactions
// of `cls`.
func (m *Manager) PublishInteractionClass(
	ctx context.Context,
	fed core.FederationName,
	pub core.FederateHandle,
	cls core.InteractionClassHandle,
) error {
	_ = ctx
	_ = fed
	_ = pub
	_ = cls
	return ErrNotImplemented
}

// UnpublishInteractionClass removes the publication.
func (m *Manager) UnpublishInteractionClass(
	ctx context.Context,
	fed core.FederationName,
	pub core.FederateHandle,
	cls core.InteractionClassHandle,
) error {
	_ = ctx
	_ = fed
	_ = pub
	_ = cls
	return ErrNotImplemented
}

// SubscribeInteractionClass records federate `sub`'s subscription.
func (m *Manager) SubscribeInteractionClass(
	ctx context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	cls core.InteractionClassHandle,
) error {
	_ = ctx
	_ = fed
	_ = sub
	_ = cls
	return ErrNotImplemented
}

// UnsubscribeInteractionClass is the symmetric remover.
func (m *Manager) UnsubscribeInteractionClass(
	ctx context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	cls core.InteractionClassHandle,
) error {
	_ = ctx
	_ = fed
	_ = sub
	_ = cls
	return ErrNotImplemented
}

// SubscribersFor returns federate handles subscribed to ANY of attrs on cls
// in fed, in sorted handle order. Returns an empty slice (never nil) when
// no subscribers match.
//
// Deterministic order: callers (object registry update path) rely on this
// for reproducible fanout sequences.
func (m *Manager) SubscribersFor(
	ctx context.Context,
	fed core.FederationName,
	cls core.ObjectClassHandle,
	attrs []core.AttributeHandle,
) []core.FederateHandle {
	_ = ctx
	_ = fed
	_ = cls
	_ = attrs
	return nil
}

// InteractionSubscribersFor returns federate handles subscribed to cls in
// fed, in sorted handle order.
func (m *Manager) InteractionSubscribersFor(
	ctx context.Context,
	fed core.FederationName,
	cls core.InteractionClassHandle,
) []core.FederateHandle {
	_ = ctx
	_ = fed
	_ = cls
	return nil
}

// PublishersFor is the symmetric query for object class attributes.
// Used by the object registry to validate "does this federate publish?".
func (m *Manager) PublishersFor(
	ctx context.Context,
	fed core.FederationName,
	cls core.ObjectClassHandle,
	attr core.AttributeHandle,
) []core.FederateHandle {
	_ = ctx
	_ = fed
	_ = cls
	_ = attr
	return nil
}

// InteractionPublishersFor is the symmetric query for interaction classes.
func (m *Manager) InteractionPublishersFor(
	ctx context.Context,
	fed core.FederationName,
	cls core.InteractionClassHandle,
) []core.FederateHandle {
	_ = ctx
	_ = fed
	_ = cls
	return nil
}
