package core

import "context"

// Typed handle types. Never use raw uint64 in service interfaces.
//
// Handle 0 is reserved as "invalid". First valid handle is 1. Assignment is
// monotonic per federation and reproducible across replays (see docs/sdd.md §5.1).

type FederationName string

type FederateHandle uint64

type ObjectHandle uint64

type ObjectClassHandle uint64

type AttributeHandle uint64

type InteractionClassHandle uint64

type ParameterHandle uint64

// DimensionHandle identifies a routing-space dimension declared in the
// FOM. M25 Phase B (§10.2 dimension services).
type DimensionHandle uint64

// ObjectInstanceNameReserver is the §6.1-6.5 reservation contract,
// split into its own interface so existing core.ObjectRegistry stubs
// (in tests + replayer) are not forced to implement reservation.
// M26 Phase F. Production *object.Registry satisfies this in addition
// to core.ObjectRegistry; the gRPC handler type-asserts.
type ObjectInstanceNameReserver interface {
	ReserveObjectInstanceName(ctx context.Context, fed FederationName, holder FederateHandle, name string) error
	ReleaseObjectInstanceName(ctx context.Context, fed FederationName, holder FederateHandle, name string) error
	ReserveMultipleObjectInstanceNames(ctx context.Context, fed FederationName, holder FederateHandle, names []string) error
}

// ObjectInstanceQuery is the §6.30 / §6.31 runtime instance-handle
// lookup contract. Split from core.ObjectRegistry for the same
// reason as ObjectInstanceNameReserver — test stubs aren't forced
// to implement it. M27 Phase C. Production *object.Registry
// satisfies this.
type ObjectInstanceQuery interface {
	// LookupObjectInstanceByName returns the handle of the registered
	// instance with the given name, or InvalidObjectHandle + false
	// if no such instance is registered in the federation.
	LookupObjectInstanceByName(fed FederationName, name string) (ObjectHandle, bool)

	// LookupObjectInstanceName returns the registered name for an
	// object handle, or "" + false if no such instance exists.
	LookupObjectInstanceName(fed FederationName, handle ObjectHandle) (string, bool)
}

const (
	InvalidFederateHandle         FederateHandle         = 0
	InvalidObjectHandle           ObjectHandle           = 0
	InvalidObjectClassHandle      ObjectClassHandle      = 0
	InvalidAttributeHandle        AttributeHandle        = 0
	InvalidInteractionClassHandle InteractionClassHandle = 0
	InvalidParameterHandle        ParameterHandle        = 0
	InvalidDimensionHandle        DimensionHandle        = 0
)
