package core

import "context"

// FOMRepository loads and serves immutable FOM models for federations.
// The concrete model type lives in rti/pkg/fom/model; this interface keeps
// `core` free of dependencies on the data layer (see docs/idd.md §3).
type FOMRepository interface {
	// Load parses + validates the modules and returns an opaque FOM handle.
	// Implementations may return an error that wraps *fom.ValidationError;
	// callers should errors.As to extract diagnostic codes.
	Load(ctx context.Context, modules []FOMModule) (FOMHandle, error)

	// Get returns the FOM handle for an existing federation.
	Get(ctx context.Context, fed FederationName) (FOMHandle, error)
}

// FOMHandle is an opaque, immutable reference to a parsed FOM.
// Implementations satisfy this with a pointer to fom.model.FOM.
type FOMHandle interface {
	// IsValid reports whether the handle refers to a successfully loaded FOM.
	IsValid() bool

	// Lookup resolves an HLA-qualified name (e.g. "HLAobjectRoot.Vehicle") to
	// an ObjectClassHandle, returning false if not found.
	LookupObjectClass(name string) (ObjectClassHandle, bool)
	LookupInteractionClass(name string) (InteractionClassHandle, bool)
	LookupAttribute(cls ObjectClassHandle, name string) (AttributeHandle, bool)
	LookupParameter(cls InteractionClassHandle, name string) (ParameterHandle, bool)
}

// FOMHandleNameLookup is the reverse-lookup half of FOMHandle, split into
// its own interface so existing stubs that only need forward lookup are
// not forced to implement the reverse direction. Production fomHandle
// satisfies both. M25 Phase B (§10.2 handle-name services).
type FOMHandleNameLookup interface {
	ObjectClassName(ObjectClassHandle) (string, bool)
	InteractionClassName(InteractionClassHandle) (string, bool)
	AttributeName(ObjectClassHandle, AttributeHandle) (string, bool)
	ParameterName(InteractionClassHandle, ParameterHandle) (string, bool)

	// Dimension services (§10.2). DimensionHandle is a uint64 alias
	// declared in handles.go.
	LookupDimension(name string) (DimensionHandle, bool)
	DimensionName(DimensionHandle) (string, bool)
	DimensionUpperBound(DimensionHandle) (uint64, bool)
}
