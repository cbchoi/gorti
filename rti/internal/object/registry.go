package object

import (
	"context"
	"errors"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
)

// ErrNotImplemented is returned by stub methods until Agent A implements them.
var ErrNotImplemented = errors.New("object: not implemented (Agent A M2 deliverable)")

// Registry implements core.ObjectRegistry. It owns per-federation object
// state (handle counter, instance map) and routes attribute updates and
// interactions to subscribers via Outbox.
type Registry struct {
	opts Options
	// internal state declared by Agent A in implementation
}

// Options bundles Registry dependencies. All MUST be non-nil; New
// validates and returns an error otherwise.
type Options struct {
	// EventLog persists every operation BEFORE state mutation
	// (write-ahead). Must be the same EventLog used by the federation
	// manager so replay sees the merged event sequence.
	EventLog core.EventLog

	// Declarations supplies SubscribersFor / PublishersFor lookups for
	// fanout. Pass the same declaration.Manager used by the gRPC
	// declaration handlers.
	Declarations *declaration.Manager

	// Outbox delivers Discover/Reflect/Receive events to subscribed
	// federate streams. Tests pass an in-memory recorder; production
	// passes the gRPC stream multiplexer.
	Outbox core.Outbox

	// Codec encodes attribute and parameter values for the wire.
	// Typically the production CodecFactory backed by rti/pkg/encoding.
	Codec core.CodecFactory

	// FOMs resolves class/attribute handles when the registry needs to
	// validate publication rights.
	FOMs core.FOMRepository

	// Clock stamps wall_ns into events. wall_ns is informational only;
	// determinism does not depend on it.
	Clock core.Clock
}

// New constructs a Registry. Returns an error if any required Options
// field is nil.
func New(opts Options) (*Registry, error) {
	return &Registry{opts: opts}, ErrNotImplemented
}

// Register implements core.ObjectRegistry. Assigns a monotonic
// ObjectHandle, persists ObjectRegistered to EventLog (write-ahead),
// fans out DiscoverObjectInstance to subscribers in deterministic order.
//
// Returns the assigned handle and the canonical object name (echoes
// caller-supplied name, or generates "<class>_<seq>" when name is empty).
//
// Errors:
//   - core.ErrObjectClassNotPublished — producer has not published the
//     class's attributes via DeclarationManager.
//   - core.ErrFederateNotJoined — producer is not a member of fed.
func (r *Registry) Register(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	cls core.ObjectClassHandle,
	name string,
) (core.ObjectHandle, string, error) {
	_ = ctx
	_ = fed
	_ = producer
	_ = cls
	_ = name
	return core.InvalidObjectHandle, "", ErrNotImplemented
}

// UpdateAttributes implements core.ObjectRegistry. Validates ownership,
// encodes attribute values via Codec, persists AttributeUpdated event
// (write-ahead), then fans out ReflectAttributeValues to subscribers.
//
// ts is the timestamp; nil means RO (Receive Order) delivery, non-nil
// means TSO (Time Stamp Order). M2 only verifies the routing — actual
// TSO semantics (LBTS, NER) come in M3.
func (r *Registry) UpdateAttributes(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	obj core.ObjectHandle,
	attrs map[core.AttributeHandle][]byte,
	ts *core.LogicalTime,
) error {
	_ = ctx
	_ = fed
	_ = producer
	_ = obj
	_ = attrs
	_ = ts
	return ErrNotImplemented
}

// SendInteraction implements core.ObjectRegistry. Symmetric to
// UpdateAttributes for interactions. Persists InteractionSent
// (write-ahead), fans out ReceiveInteraction to subscribers.
func (r *Registry) SendInteraction(
	ctx context.Context,
	fed core.FederationName,
	producer core.FederateHandle,
	cls core.InteractionClassHandle,
	params map[core.ParameterHandle][]byte,
	ts *core.LogicalTime,
) error {
	_ = ctx
	_ = fed
	_ = producer
	_ = cls
	_ = params
	_ = ts
	return ErrNotImplemented
}

// Compile-time assertion that Registry implements core.ObjectRegistry.
var _ core.ObjectRegistry = (*Registry)(nil)
