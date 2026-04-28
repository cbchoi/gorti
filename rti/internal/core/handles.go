package core

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

const (
	InvalidFederateHandle         FederateHandle         = 0
	InvalidObjectHandle           ObjectHandle           = 0
	InvalidObjectClassHandle      ObjectClassHandle      = 0
	InvalidAttributeHandle        AttributeHandle        = 0
	InvalidInteractionClassHandle InteractionClassHandle = 0
	InvalidParameterHandle        ParameterHandle        = 0
)
