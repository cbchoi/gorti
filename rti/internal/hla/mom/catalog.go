package mom

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// CatalogSources supplies coherent, detached views of the owning services.
// A nil callback is unavailable, never an empty/zero observation. Providers
// must include regional subscriptions and preserve the actual update-rate name.
type CatalogSources struct {
	Subscriptions             func(context.Context, core.FederationName, core.FederateHandle) (SubscriptionSnapshot, error)
	ObjectInstanceCounts      func(context.Context, core.FederationName, core.FederateHandle, ObjectInstanceCountKind) (map[uint64]uint32, error)
	ObjectInstanceInformation func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle) (ObjectInstanceInformation, error)
	// Target zero requests the federation's modules; otherwise only modules
	// supplied at that target's join. Indicators and list order are authoritative.
	FOMModules            func(context.Context, core.FederationName, core.FederateHandle) ([]FOMModule, error)
	SynchronizationPoints func(context.Context, core.FederationName) ([]SynchronizationPoint, error)
	// Grant validates known class/publication and excludes RTI-owned MOM
	// attributes. Changing ownership must not generate ownership callbacks.
	ModifyAttributeState func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, core.AttributeHandle, bool) error
	// Report routing includes subscription/DDM filtering in the owner. Service
	// is populated for service reports so its service-group dimension can be used.
	ReportRecipients func(context.Context, core.FederationName, core.FederateHandle, string, string) ([]core.FederateHandle, error)
}

type SubscriptionSnapshot struct {
	Objects      []ObjectSubscription
	Interactions []InteractionSubscription
}

// Exactly one entry per class/active combination, with the union of attributes
// and highest subscribed rate selected by the provider using its rate table.
type ObjectSubscription struct {
	Class         core.ObjectClassHandle
	Active        bool
	MaxUpdateRate string
	Attributes    []core.AttributeHandle
}

type InteractionSubscription struct {
	Class  core.InteractionClassHandle
	Active bool
}

type ObjectInstanceCountKind string

const (
	ObjectsDeletable ObjectInstanceCountKind = "deletable"
	ObjectsUpdated   ObjectInstanceCountKind = "updated"
	ObjectsReflected ObjectInstanceCountKind = "reflected"
)

// Counts are distinct object identities by REGISTERED class. Updated/reflected
// history uses the existing RTI acceptance boundary, not physical receipt.
type ObjectInstanceInformation struct {
	Known           bool
	RegisteredClass core.ObjectClassHandle
	KnownClass      core.ObjectClassHandle
	OwnedAttributes []core.AttributeHandle
}

type FOMModule struct {
	Indicator  int32
	Designator string
	Data       string
}

type SynchronizationPoint struct {
	Name      string
	Federates []SynchronizationFederate
}

type SynchronizationFederate struct {
	Handle core.FederateHandle
	// Embedded MIM HLAsynchPointStatus: 0 NoActivity, 1 AttemptingToRegister,
	// 2 MovingToSynchPoint, 3 WaitingForRestOfFederation.
	Status uint32
}

func (d *Dispatcher) SetCatalogSources(sources CatalogSources) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.catalog = sources
	if d.mom != nil {
		d.mom.mu.Lock()
		d.mom.moduleSource = sources.FOMModules
		d.mom.mu.Unlock()
	}
}
