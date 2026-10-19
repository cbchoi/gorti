package core

// FOMSubscriptionEvaluation exposes the immutable federation-wide FDD switch.
// An undeclared switch uses generation-time candidate pruning.
type FOMSubscriptionEvaluation interface {
	DelaySubscriptionEvaluation() (enabled, declared bool)
}

// SubscriptionEvaluationRoster supplies candidates independently of interest.
// Messages are retained only for members joined when they are generated.
type SubscriptionEvaluationRoster interface {
	ListMembers(FederationName) []FederationMember
}

// FOMHandleReference follows committed immutable views without a repeated model
// lookup. The reference is stable; the returned handle is a revision snapshot.
type FOMHandleReference interface {
	CurrentFOMHandle() FOMHandle
}

type FOMHandleReferenceRepository interface {
	FOMHandleReference(FederationName) FOMHandleReference
}
