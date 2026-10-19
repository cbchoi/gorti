package core

// FOMExtensionPreparation is a prepared immutable repository view. The federation's
// operation admission must cover preparation, dependent-service admission,
// journaling, and Commit. Commit publishes only the FOM repository.
type FOMExtensionPreparation interface {
	FOM() FOMHandle
	View() FOMView
	// PrepareCommit validates the base and leases the repository slot. Readers
	// still see the old view; writers wait until Commit or Release. Call this
	// before the durable journal append so a successful append cannot lose CAS.
	PrepareCommit() error
	Commit() error
	Release()
}

// FOMView carries exact source and authoritative assigned handles. Members
// include inherited declarations; their handles are scoped to their class.
// Producers return independent copies so consumers cannot mutate live views.
type FOMView struct {
	Revision           uint64
	Digest             string
	Modules            []FOMModule
	MIMModule          *FOMModule `json:",omitempty"`
	ObjectClasses      []FOMObjectClassBinding
	InteractionClasses []FOMInteractionClassBinding
	Dimensions         []FOMDimensionBinding
}

type FOMObjectClassBinding struct {
	Handle     ObjectClassHandle
	Name       string
	Attributes []FOMAttributeBinding
}

type FOMAttributeBinding struct {
	Handle AttributeHandle
	Name   string
}

type FOMInteractionClassBinding struct {
	Handle     InteractionClassHandle
	Name       string
	Parameters []FOMParameterBinding
}

type FOMParameterBinding struct {
	Handle ParameterHandle
	Name   string
}

type FOMDimensionBinding struct {
	Handle     DimensionHandle
	Name       string
	UpperBound uint64
}

// FOMRepositoryState persists exact sources and their history-dependent handle
// assignments. Re-parsing Modules alone cannot recover append-only handles.
type FOMRepositoryState struct {
	Version uint32
	View    FOMView
}
