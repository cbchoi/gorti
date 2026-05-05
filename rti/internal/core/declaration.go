package core

import "context"

// DeclarationManagement owns per-federation pub/sub declaration state
// (IEEE 1516.1-2010 §5) and answers SubscribersFor / PublishersFor
// queries in deterministic handle order. Phase 1 of the
// research-platform refactor (docs/research-platform.md §5.6) carves
// this out as the service-level interface.
//
// Production impl: rti/internal/declaration.Manager.
//
// Note re docs/idd.md §3: the cut-2 IDD note ("declaration is a pure
// local component, no abstraction layer") explicitly assumed declaration
// would never need an interface. The Phase 0 design pinned 2026-05-05
// (decision §3 / docs/research-platform.md §5.6) overrides that note
// because research-platform reachability outweighs the purity argument.
// The doc note has been updated alongside this commit; the production
// declaration.Manager remains the only impl.
//
// Methods exposed correspond to the eight §5 pub/sub RPCs (the only
// methods the gRPC handler in transport/grpc/declaration.go calls)
// plus the four lookup queries (the only methods the object.Registry
// hot path calls). Manager-private helpers, ErrNotImplemented, and
// the test-only state-introspection accessors are not exposed.
//
// Concurrency: implementations must be goroutine-safe.
type DeclarationManagement interface {
	// --- Object-class pub/sub (§5.2..§5.5) -------------------------------

	// PublishObjectClassAttributes records that federate `pub`
	// publishes the listed attributes of `cls` in `fed`.
	PublishObjectClassAttributes(
		ctx context.Context,
		fed FederationName,
		pub FederateHandle,
		cls ObjectClassHandle,
		attrs []AttributeHandle,
	) error

	// UnpublishObjectClassAttributes removes federate `pub`'s
	// publication of the listed attributes.
	UnpublishObjectClassAttributes(
		ctx context.Context,
		fed FederationName,
		pub FederateHandle,
		cls ObjectClassHandle,
		attrs []AttributeHandle,
	) error

	// SubscribeObjectClassAttributes records federate `sub`'s
	// subscription.
	SubscribeObjectClassAttributes(
		ctx context.Context,
		fed FederationName,
		sub FederateHandle,
		cls ObjectClassHandle,
		attrs []AttributeHandle,
	) error

	// UnsubscribeObjectClassAttributes is the symmetric remover.
	UnsubscribeObjectClassAttributes(
		ctx context.Context,
		fed FederationName,
		sub FederateHandle,
		cls ObjectClassHandle,
		attrs []AttributeHandle,
	) error

	// --- Interaction-class pub/sub (§5.6..§5.9) --------------------------

	// PublishInteractionClass records that federate `pub` publishes
	// interactions of `cls`.
	PublishInteractionClass(
		ctx context.Context,
		fed FederationName,
		pub FederateHandle,
		cls InteractionClassHandle,
	) error

	// UnpublishInteractionClass removes the publication.
	UnpublishInteractionClass(
		ctx context.Context,
		fed FederationName,
		pub FederateHandle,
		cls InteractionClassHandle,
	) error

	// SubscribeInteractionClass records federate `sub`'s
	// subscription.
	SubscribeInteractionClass(
		ctx context.Context,
		fed FederationName,
		sub FederateHandle,
		cls InteractionClassHandle,
	) error

	// UnsubscribeInteractionClass is the symmetric remover.
	UnsubscribeInteractionClass(
		ctx context.Context,
		fed FederationName,
		sub FederateHandle,
		cls InteractionClassHandle,
	) error

	// --- Lookup queries (object.Registry hot path) -----------------------

	// SubscribersFor returns federate handles subscribed to ANY of
	// attrs on cls in fed, in sorted handle order. Returns an empty
	// slice (never nil) when no subscribers match.
	SubscribersFor(
		ctx context.Context,
		fed FederationName,
		cls ObjectClassHandle,
		attrs []AttributeHandle,
	) []FederateHandle

	// InteractionSubscribersFor returns federate handles subscribed
	// to cls in fed, in sorted handle order.
	InteractionSubscribersFor(
		ctx context.Context,
		fed FederationName,
		cls InteractionClassHandle,
	) []FederateHandle

	// PublishersFor is the symmetric query for object-class
	// attributes — returns federate handles that publish the given
	// (cls, attr) in fed.
	PublishersFor(
		ctx context.Context,
		fed FederationName,
		cls ObjectClassHandle,
		attr AttributeHandle,
	) []FederateHandle

	// InteractionPublishersFor is the symmetric query for
	// interaction classes.
	InteractionPublishersFor(
		ctx context.Context,
		fed FederationName,
		cls InteractionClassHandle,
	) []FederateHandle
}
