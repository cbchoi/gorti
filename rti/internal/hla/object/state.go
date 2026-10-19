package object

import (
	"cmp"
	"fmt"
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// RegistryState contains RTI-owned object state for one federation. Attribute
// values are not cached by Registry: federates own them and the registry routes
// updates. Pending callbacks belong to delivery/time state, not this snapshot.
// Name indexes and the FOM-derived management-class cache are rebuilt on import.
type RegistryStateOf[T core.LogicalTimeScalar] struct {
	Deletions []DeletionStateOf[T]
	// These are the last allocated values, including allocations for objects
	// subsequently deleted. The next allocation increments the saved value.
	LastObjectHandle      uint64
	LastOutboundSequence  uint64
	Objects               []InstanceState
	Discoveries           []DiscoveryState
	Scopes                []ScopeState
	Reservations          []NameReservationState
	AttributeTransports   []AttributeTransportState
	InteractionTransports []InteractionTransportState
	AttributeOrders       []AttributeOrderState
	InteractionOrders     []InteractionOrderState
}

type AttributeOrderState struct {
	Object    core.ObjectHandle
	Attribute core.AttributeHandle
	Order     core.OrderType
}
type InteractionOrderState struct {
	Publisher core.FederateHandle
	Class     core.InteractionClassHandle
	Order     core.OrderType
}

type InstanceState struct {
	Handle     core.ObjectHandle
	Name       string
	Class      core.ObjectClassHandle
	Registrant core.FederateHandle
}

type DiscoveryState struct {
	Object     core.ObjectHandle
	Federate   core.FederateHandle
	KnownClass core.ObjectClassHandle `json:",omitempty"`
}

type ScopeState struct {
	Object     core.ObjectHandle
	Federate   core.FederateHandle
	Attributes []core.AttributeHandle
}

type NameReservationState struct {
	Name   string
	Holder core.FederateHandle
}

type AttributeTransportState struct {
	Object         core.ObjectHandle
	Attribute      core.AttributeHandle
	Transportation core.TransportType
}

type InteractionTransportState struct {
	Publisher      core.FederateHandle
	Class          core.InteractionClassHandle
	Transportation core.TransportType
}

// ExportState returns an independently owned, canonically ordered image without
// creating a federation. The caller must quiesce federation operations and drain
// delivery work before a coordinated save; these locks alone do not make a
// multi-service save atomic.
func (r *RegistryOf[T]) ExportState(fed core.FederationName) *RegistryStateOf[T] {
	r.mu.RLock()
	defer r.mu.RUnlock()
	st := r.federations[fed]
	if st != nil {
		st.mu.Lock()
		defer st.mu.Unlock()
	}
	r.reservations.mu.Lock()
	defer r.reservations.mu.Unlock()
	r.transports.mu.RLock()
	defer r.transports.mu.RUnlock()

	s := &RegistryStateOf[T]{}
	if st != nil {
		s.exportInstancesLocked(st)
		s.exportKnowledgeLocked(st)
	}
	for name, holder := range r.reservations.reservations[fed] {
		s.Reservations = append(s.Reservations, NameReservationState{Name: name, Holder: holder})
	}
	for key, tt := range r.transports.attr {
		if key.fed == fed {
			s.AttributeTransports = append(s.AttributeTransports, AttributeTransportState{Object: key.obj, Attribute: key.attr, Transportation: tt})
		}
	}
	for key, tt := range r.transports.inter {
		if key.fed == fed {
			s.InteractionTransports = append(s.InteractionTransports, InteractionTransportState{Publisher: key.publisher, Class: key.cls, Transportation: tt})
		}
	}
	s.sort()
	return s
}

func (s *RegistryStateOf[T]) exportInstancesLocked(st *federationStateOf[T]) {
	s.LastObjectHandle = st.nextObjectHandle
	for _, deletion := range st.deletions {
		deletion.Tag = slices.Clone(deletion.Tag)
		deletion.AttributeOwners = slices.Clone(deletion.AttributeOwners)
		deletion.Recipients = slices.Clone(deletion.Recipients)
		s.Deletions = append(s.Deletions, deletion)
	}
	s.LastOutboundSequence = st.nextOutboundSeq
	for _, inst := range st.instances {
		s.Objects = append(s.Objects, InstanceState{Handle: inst.handle, Name: inst.name, Class: inst.cls, Registrant: inst.owner})
	}
	for obj, attrs := range st.attributeOrders {
		for attr, order := range attrs {
			s.AttributeOrders = append(s.AttributeOrders, AttributeOrderState{obj, attr, order})
		}
	}
	for publisher, classes := range st.interactionOrders {
		for cls, order := range classes {
			s.InteractionOrders = append(s.InteractionOrders, InteractionOrderState{publisher, cls, order})
		}
	}
}

func (s *RegistryStateOf[T]) exportKnowledgeLocked(st *federationStateOf[T]) {
	for key, cls := range st.discovered {
		if cls == st.registeredClass(key.obj) {
			cls = 0
		}
		s.Discoveries = append(s.Discoveries, DiscoveryState{Object: key.obj, Federate: key.sub, KnownClass: cls})
	}
	for obj, subs := range st.scope {
		for sub, attrs := range subs {
			if len(attrs) == 0 {
				continue
			}
			entry := ScopeState{Object: obj, Federate: sub}
			for attr := range attrs {
				entry.Attributes = append(entry.Attributes, attr)
			}
			slices.Sort(entry.Attributes)
			s.Scopes = append(s.Scopes, entry)
		}
	}
}

func (s *RegistryStateOf[T]) sort() {
	slices.SortFunc(s.Objects, func(a, b InstanceState) int { return cmp.Compare(a.Handle, b.Handle) })
	slices.SortFunc(s.Deletions, func(a, b DeletionStateOf[T]) int { return cmp.Compare(a.Object, b.Object) })
	slices.SortFunc(s.Discoveries, func(a, b DiscoveryState) int {
		if c := cmp.Compare(a.Object, b.Object); c != 0 {
			return c
		}
		return cmp.Compare(a.Federate, b.Federate)
	})
	slices.SortFunc(s.Scopes, func(a, b ScopeState) int {
		if c := cmp.Compare(a.Object, b.Object); c != 0 {
			return c
		}
		return cmp.Compare(a.Federate, b.Federate)
	})
	slices.SortFunc(s.Reservations, func(a, b NameReservationState) int { return cmp.Compare(a.Name, b.Name) })
	slices.SortFunc(s.AttributeTransports, func(a, b AttributeTransportState) int {
		if c := cmp.Compare(a.Object, b.Object); c != 0 {
			return c
		}
		return cmp.Compare(a.Attribute, b.Attribute)
	})
	slices.SortFunc(s.InteractionTransports, func(a, b InteractionTransportState) int {
		if c := cmp.Compare(a.Publisher, b.Publisher); c != 0 {
			return c
		}
		return cmp.Compare(a.Class, b.Class)
	})
	slices.SortFunc(s.AttributeOrders, func(a, b AttributeOrderState) int {
		if c := cmp.Compare(a.Object, b.Object); c != 0 {
			return c
		}
		return cmp.Compare(a.Attribute, b.Attribute)
	})
	slices.SortFunc(s.InteractionOrders, func(a, b InteractionOrderState) int {
		if c := cmp.Compare(a.Publisher, b.Publisher); c != 0 {
			return c
		}
		return cmp.Compare(a.Class, b.Class)
	})
}

// PrepareImportState validates and copies the entire image without changing
// live state. Nil means empty state. A successful commit is infallible and
// idempotent, replaces only fed, and emits no callbacks or registration hooks.
// The enclosing coordinator must quiesce operations through commit and verify
// federation generation, FOM identity and federate handles across all services.
// This method neither remaps handles nor establishes cross-service atomicity.
func (r *RegistryOf[T]) PrepareImportState(fed core.FederationName, state *RegistryStateOf[T]) (func(), error) {
	prepared, err := prepareRegistryStateOf[T](fed, state)
	if err != nil {
		return nil, fmt.Errorf("object snapshot for %q: %w", fed, err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.reservations.mu.Lock()
			defer r.reservations.mu.Unlock()
			r.transports.mu.Lock()
			defer r.transports.mu.Unlock()
			r.transports.retireTicketsLocked(func(c core.TransportationChange) bool { return c.Scope.Member.Federation == fed })
			if previous := r.federations[fed]; previous != nil {
				previous.knowledgeRetired.Store(true)
			}
			r.federations[fed] = prepared.state
			delete(r.reservations.reservations, fed)
			delete(r.reservations.registered, fed)
			if len(prepared.reservations) != 0 {
				r.reservations.reservations[fed] = prepared.reservations
			}
			if len(prepared.registered) != 0 {
				r.reservations.registered[fed] = prepared.registered
			}
			for key := range r.transports.attr {
				if key.fed == fed {
					delete(r.transports.attr, key)
				}
			}
			for key, tt := range prepared.attr {
				r.transports.attr[key] = tt
			}
			for key := range r.transports.inter {
				if key.fed == fed {
					delete(r.transports.inter, key)
				}
			}
			for key, tt := range prepared.inter {
				r.transports.inter[key] = tt
			}
		})
	}, nil
}

type preparedRegistryStateOf[T core.LogicalTimeScalar] struct {
	state        *federationStateOf[T]
	reservations map[string]core.FederateHandle
	registered   map[string]struct{}
	attr         map[attrTransportKey]core.TransportType
	inter        map[interTransportKey]core.TransportType
}

func prepareRegistryStateOf[T core.LogicalTimeScalar](fed core.FederationName, s *RegistryStateOf[T]) (*preparedRegistryStateOf[T], error) {
	if s == nil {
		s = &RegistryStateOf[T]{}
	}
	if s.LastObjectHandle == ^uint64(0) || s.LastOutboundSequence == ^uint64(0) {
		return nil, fmt.Errorf("exhausted allocation counter")
	}
	p := &preparedRegistryStateOf[T]{
		state:        newFederationStateOf[T](),
		reservations: make(map[string]core.FederateHandle, len(s.Reservations)),
		registered:   make(map[string]struct{}, len(s.Objects)),
		attr:         make(map[attrTransportKey]core.TransportType, len(s.AttributeTransports)),
		inter:        make(map[interTransportKey]core.TransportType, len(s.InteractionTransports)),
	}
	st := p.state
	st.nextObjectHandle, st.nextOutboundSeq = s.LastObjectHandle, s.LastOutboundSequence
	for _, prepare := range []func() error{
		func() error { return p.prepareObjects(s) },
		func() error { return p.prepareDiscoveries(s) },
		func() error { return p.prepareDeletions(s) },
		func() error { return p.prepareScopes(s) },
		func() error { return p.prepareReservations(s) },
		func() error { return p.prepareAttributeTransports(fed, s) },
		func() error { return p.prepareInteractionTransports(fed, s) },
		func() error { return p.prepareAttributeOrders(s) },
		func() error { return p.prepareInteractionOrders(s) },
	} {
		if err := prepare(); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *preparedRegistryStateOf[T]) prepareObjects(s *RegistryStateOf[T]) error {
	st := p.state
	for _, entry := range s.Objects {
		if entry.Handle == 0 || uint64(entry.Handle) > s.LastObjectHandle || entry.Class == 0 || entry.Registrant == 0 || entry.Name == "" || !utf8.ValidString(entry.Name) {
			return fmt.Errorf("invalid object %d", entry.Handle)
		}
		if _, exists := st.instances[entry.Handle]; exists {
			return fmt.Errorf("duplicate object %d", entry.Handle)
		}
		if _, exists := st.nameToHandle[entry.Name]; exists {
			return fmt.Errorf("duplicate object name %q", entry.Name)
		}
		st.instances[entry.Handle] = &objectInstance{handle: entry.Handle, name: entry.Name, cls: entry.Class, owner: entry.Registrant}
		st.nameToHandle[entry.Name] = entry.Handle
		p.registered[entry.Name] = struct{}{}
	}
	return nil
}

func (p *preparedRegistryStateOf[T]) prepareDiscoveries(s *RegistryStateOf[T]) error {
	st := p.state
	tombstones := make(map[core.ObjectHandle]DeletionStateOf[T])
	for _, deletion := range s.Deletions {
		if deletion.Committed && deletion.ObjectName != "" && deletion.ObjectClass != 0 {
			tombstones[deletion.Object] = deletion
		}
	}
	for _, entry := range s.Discoveries {
		_, tombstone := tombstones[entry.Object]
		if (st.instances[entry.Object] == nil && !tombstone) || entry.Federate == 0 {
			return fmt.Errorf("invalid discovery for object %d", entry.Object)
		}
		key := discoverKey{obj: entry.Object, sub: entry.Federate}
		if _, exists := st.discovered[key]; exists {
			return fmt.Errorf("duplicate discovery for object %d, federate %d", entry.Object, entry.Federate)
		}
		cls := entry.KnownClass
		if cls == 0 {
			if inst := st.instances[entry.Object]; inst != nil {
				cls = inst.cls
			} else {
				cls = tombstones[entry.Object].ObjectClass
			}
		}
		st.discovered[key] = cls
	}
	return nil
}

func (p *preparedRegistryStateOf[T]) prepareDeletions(s *RegistryStateOf[T]) error {
	st := p.state
	st.deletions = make(map[core.ObjectHandle]DeletionStateOf[T], len(s.Deletions))
	deletionIDs := make(map[[2]uint64]bool)
	for _, d := range s.Deletions {
		if d.Object == 0 || uint64(d.Object) > s.LastObjectHandle || d.Producer == 0 || validateFiniteMessageTimestampOf(&d.Timestamp) != nil {
			return fmt.Errorf("invalid pending deletion")
		}
		if _, duplicate := st.deletions[d.Object]; duplicate {
			return fmt.Errorf("duplicate deletion")
		}
		if (!d.Committed && st.instances[d.Object] == nil) || (d.Committed && st.instances[d.Object] != nil) {
			return fmt.Errorf("inconsistent deletion state")
		}
		if d.RetractionHandle != 0 {
			key := [2]uint64{uint64(d.Producer), d.RetractionHandle}
			if deletionIDs[key] {
				return fmt.Errorf("duplicate deletion retraction identity")
			}
			deletionIDs[key] = true
		}
		if err := p.validateDeletionSnapshot(d); err != nil {
			return err
		}
		d.Tag = slices.Clone(d.Tag)
		d.AttributeOwners = slices.Clone(d.AttributeOwners)
		d.Recipients = slices.Clone(d.Recipients)
		st.deletions[d.Object] = d
	}
	return nil
}

func (p *preparedRegistryStateOf[T]) validateDeletionSnapshot(d DeletionStateOf[T]) error {
	if !d.OwnershipSnapshot && len(d.AttributeOwners) != 0 {
		return fmt.Errorf("deletion has ownership entries without a snapshot")
	}
	var prior core.AttributeHandle
	for _, owner := range d.AttributeOwners {
		if owner.Attribute == 0 || owner.Attribute <= prior {
			return fmt.Errorf("deletion ownership has invalid or duplicate attribute")
		}
		prior = owner.Attribute
	}
	if (d.ObjectName == "") != (d.ObjectClass == 0) || !utf8.ValidString(d.ObjectName) {
		return fmt.Errorf("deletion has invalid object identity")
	}
	if instance := p.state.instances[d.Object]; instance != nil && d.ObjectName != "" && (instance.name != d.ObjectName || instance.cls != d.ObjectClass) {
		return fmt.Errorf("deletion object identity differs from live instance")
	}
	return p.validateDeletionRecipients(d)
}

func (p *preparedRegistryStateOf[T]) validateDeletionRecipients(d DeletionStateOf[T]) error {
	var priorRecipient core.FederateHandle
	for _, recipient := range d.Recipients {
		if recipient.Recipient == 0 || recipient.Recipient == ^core.FederateHandle(0) || recipient.Recipient <= priorRecipient || (!recipient.ReceivedTSO && !recipient.Forgotten) {
			return fmt.Errorf("deletion has invalid recipient state")
		}
		if _, known := p.state.discovered[discoverKey{obj: d.Object, sub: recipient.Recipient}]; known && recipient.Forgotten {
			return fmt.Errorf("forgotten deletion recipient retains object knowledge")
		}
		priorRecipient = recipient.Recipient
	}
	return nil
}

func (p *preparedRegistryStateOf[T]) prepareScopes(s *RegistryStateOf[T]) error {
	st := p.state
	for _, entry := range s.Scopes {
		if st.instances[entry.Object] == nil || entry.Federate == 0 || len(entry.Attributes) == 0 {
			return fmt.Errorf("invalid scope for object %d", entry.Object)
		}
		if st.scope == nil {
			st.scope = make(map[core.ObjectHandle]map[core.FederateHandle]map[core.AttributeHandle]struct{})
		}
		subs := st.scope[entry.Object]
		if subs == nil {
			subs = make(map[core.FederateHandle]map[core.AttributeHandle]struct{})
			st.scope[entry.Object] = subs
		}
		if _, exists := subs[entry.Federate]; exists {
			return fmt.Errorf("duplicate scope for object %d, federate %d", entry.Object, entry.Federate)
		}
		attrs := make(map[core.AttributeHandle]struct{}, len(entry.Attributes))
		for _, attr := range entry.Attributes {
			if attr == 0 {
				return fmt.Errorf("zero scope attribute for object %d", entry.Object)
			}
			if _, exists := attrs[attr]; exists {
				return fmt.Errorf("duplicate scope attribute %d", attr)
			}
			attrs[attr] = struct{}{}
		}
		subs[entry.Federate] = attrs
	}
	return nil
}

func (p *preparedRegistryStateOf[T]) prepareReservations(s *RegistryStateOf[T]) error {
	for _, entry := range s.Reservations {
		if entry.Name == "" || !utf8.ValidString(entry.Name) || entry.Holder == 0 {
			return fmt.Errorf("invalid name reservation")
		}
		if _, exists := p.reservations[entry.Name]; exists {
			return fmt.Errorf("duplicate reservation %q", entry.Name)
		}
		p.reservations[entry.Name] = entry.Holder
	}
	return nil
}

func (p *preparedRegistryStateOf[T]) prepareAttributeTransports(fed core.FederationName, s *RegistryStateOf[T]) error {
	st := p.state
	for _, entry := range s.AttributeTransports {
		if st.instances[entry.Object] == nil || entry.Attribute == 0 || !validTransportation(entry.Transportation) {
			return fmt.Errorf("invalid attribute transportation for object %d", entry.Object)
		}
		key := attrTransportKey{fed: fed, obj: entry.Object, attr: entry.Attribute}
		if _, exists := p.attr[key]; exists {
			return fmt.Errorf("duplicate attribute transportation for object %d, attribute %d", entry.Object, entry.Attribute)
		}
		p.attr[key] = entry.Transportation
	}
	return nil
}

func (p *preparedRegistryStateOf[T]) prepareInteractionTransports(fed core.FederationName, s *RegistryStateOf[T]) error {
	for _, entry := range s.InteractionTransports {
		if entry.Publisher == 0 || entry.Class == 0 || !validTransportation(entry.Transportation) {
			return fmt.Errorf("invalid interaction transportation")
		}
		key := interTransportKey{fed: fed, publisher: entry.Publisher, cls: entry.Class}
		if _, exists := p.inter[key]; exists {
			return fmt.Errorf("duplicate interaction transportation for publisher %d, class %d", entry.Publisher, entry.Class)
		}
		p.inter[key] = entry.Transportation
	}
	return nil
}

func (p *preparedRegistryStateOf[T]) prepareAttributeOrders(s *RegistryStateOf[T]) error {
	st := p.state
	st.attributeOrders = make(map[core.ObjectHandle]map[core.AttributeHandle]core.OrderType)
	for _, entry := range s.AttributeOrders {
		if st.instances[entry.Object] == nil || entry.Attribute == 0 || (entry.Order != core.OrderReceive && entry.Order != core.OrderTimestamp) {
			return fmt.Errorf("invalid attribute order")
		}
		if st.attributeOrders[entry.Object] == nil {
			st.attributeOrders[entry.Object] = make(map[core.AttributeHandle]core.OrderType)
		}
		if st.attributeOrders[entry.Object][entry.Attribute] != 0 {
			return fmt.Errorf("duplicate attribute order")
		}
		st.attributeOrders[entry.Object][entry.Attribute] = entry.Order
	}
	return nil
}

func (p *preparedRegistryStateOf[T]) prepareInteractionOrders(s *RegistryStateOf[T]) error {
	st := p.state
	st.interactionOrders = make(map[core.FederateHandle]map[core.InteractionClassHandle]core.OrderType)
	for _, entry := range s.InteractionOrders {
		if entry.Publisher == 0 || entry.Class == 0 || (entry.Order != core.OrderReceive && entry.Order != core.OrderTimestamp) {
			return fmt.Errorf("invalid interaction order")
		}
		if st.interactionOrders[entry.Publisher] == nil {
			st.interactionOrders[entry.Publisher] = make(map[core.InteractionClassHandle]core.OrderType)
		}
		if st.interactionOrders[entry.Publisher][entry.Class] != 0 {
			return fmt.Errorf("duplicate interaction order")
		}
		st.interactionOrders[entry.Publisher][entry.Class] = entry.Order
	}
	return nil
}
