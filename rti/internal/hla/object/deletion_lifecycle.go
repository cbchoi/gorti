package object

import (
	"context"
	"slices"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// DeletionState retains an admitted future deletion, or a query-only identity
// until its last recipient forgets it. Callback payloads live in time state.
type DeletionStateOf[T core.LogicalTimeScalar] struct {
	Object            core.ObjectHandle
	Producer          core.FederateHandle
	Timestamp         T
	RetractionHandle  uint64
	Committed         bool
	Tag               []byte
	OwnershipSnapshot bool                     `json:",omitempty"`
	AttributeOwners   []DeletionAttributeOwner `json:",omitempty"`
	ObjectName        string                   `json:",omitempty"`
	ObjectClass       core.ObjectClassHandle   `json:",omitempty"`
	Recipients        []DeletionRecipientState `json:",omitempty"`
}

type DeletionRecipientState struct {
	Recipient   core.FederateHandle
	ReceivedTSO bool
	Forgotten   bool
	KnownClass  core.ObjectClassHandle `json:",omitempty"`
}

type DeletionAttributeOwner struct {
	Attribute core.AttributeHandle
	Owner     core.FederateHandle
}

func (r *RegistryOf[T]) captureDeletionOwners(ctx context.Context, fed core.FederationName, inst *objectInstance, deletion *DeletionStateOf[T]) {
	if owners, ok := r.opts.Ownership.(interface {
		PrepareAttributeOwners(core.FederationName, core.ObjectHandle, []core.AttributeHandle) (map[core.AttributeHandle]core.FederateHandle, func())
	}); ok {
		attrs := r.classAttributes(ctx, fed, inst.cls)
		snapshot, release := owners.PrepareAttributeOwners(fed, inst.handle, attrs)
		defer release()
		deletion.OwnershipSnapshot = true
		for attr, owner := range snapshot {
			deletion.AttributeOwners = append(deletion.AttributeOwners, DeletionAttributeOwner{Attribute: attr, Owner: owner})
		}
		slices.SortFunc(deletion.AttributeOwners, func(a, b DeletionAttributeOwner) int {
			if a.Attribute < b.Attribute {
				return -1
			}
			if a.Attribute > b.Attribute {
				return 1
			}
			return 0
		})
	}
}

// discoverPendingDeletionLocked admits discovery and its eventual removal as
// one operation. The caller holds only the instance delivery mutex.
func (r *RegistryOf[T]) discoverPendingDeletionLocked(ctx context.Context, fed core.FederationName, st *federationStateOf[T], sub core.FederateHandle, inst *objectInstance, deletion DeletionStateOf[T], cls core.ObjectClassHandle) {
	gate, ok := r.opts.TSOGate.(core.ReservableTSODeliveryGateOf[T])
	if !ok {
		return
	}
	st.mu.Lock()
	seq := st.nextOutboundSeqRangeLocked(2)
	st.mu.Unlock()
	remove := &CallbackEventOf[T]{Sequence: seq + 1, Kind: CallbackRemove, Object: inst.handle, ObjectClass: cls, LogicalTime: copiedLogicalTimeOf[T](&deletion.Timestamp), UserSuppliedTag: slices.Clone(deletion.Tag), Metadata: metadataForOf[T](inst.owner, &deletion.Timestamp, 0)}
	remove.Metadata.RetractionHandle = deletion.RetractionHandle
	timed := []core.MessageDeliveryOf[T]{{Recipient: sub, Timestamp: deletion.Timestamp, Event: remove, Sender: deletion.Producer, RetractionHandle: deletion.RetractionHandle}}
	p := &preparedAttributeFanoutOf[T]{registry: r, state: st, fed: fed, producer: deletion.Producer, instance: inst}
	p.tsoReservation = gate.ReserveTSO(fed, timed)
	if p.tsoReservation == nil {
		return
	}
	defer p.abort()
	st.mu.Lock()
	current, pending := st.deletions[inst.handle]
	valid := st.instances[inst.handle] == inst && pending && !current.Committed && current.Producer == deletion.Producer && current.RetractionHandle == deletion.RetractionHandle
	st.mu.Unlock()
	if !valid {
		return
	}
	p.discoveries = []discoverKey{{obj: inst.handle, sub: sub}}
	p.discoveryClasses = map[discoverKey]core.ObjectClassHandle{p.discoveries[0]: cls}
	p.appendImmediate(core.OutboxDelivery{Recipient: sub, Event: &CallbackEventOf[T]{Sequence: seq, Kind: CallbackDiscover, Object: inst.handle, ObjectClass: cls, ObjectName: inst.name, Metadata: metadataForOf[T](inst.owner, nil, 0)}})
	immediate := p.tsoReservation.Immediate()
	for _, d := range immediate {
		p.appendImmediate(core.OutboxDelivery{Recipient: d.Recipient, Event: d.Event})
	}
	out, ok := r.opts.Outbox.(core.ReservableOutbox)
	if !ok {
		return
	}
	reservation, err := out.Reserve(ctx, fed, p.immediate)
	if err != nil || reservation == nil {
		if reservation != nil {
			reservation.Release()
		}
		return
	}
	p.reservation = reservation
	if err := p.commit(ctx); err != nil {
		return
	}
	// A receiver's speculative removal does not retire authoritative object or
	// ownership state. The sender's internal timestamp marker owns that boundary.
}

func (e *CallbackEventOf[T]) InternalTSO() bool { return e != nil && e.Kind == CallbackDeleteMarker }

func (e *CallbackEventOf[T]) DeletionObject() core.ObjectHandle {
	if e != nil && (e.Kind == CallbackRemove || e.Kind == CallbackDeleteMarker) {
		return e.Object
	}
	return 0
}

type deletionDeliveryOf[T core.LogicalTimeScalar] struct {
	r                  *RegistryOf[T]
	st                 *federationStateOf[T]
	fed                core.FederationName
	objects            []core.ObjectHandle
	removals           []core.OutboxDelivery
	once               sync.Once
	ownership          core.OutboxReservation
	ownerOnce          sync.Once
	ownerError         error
	finishOwnership    func(context.Context, error)
	ownershipCommitted bool
	finishQueued       bool
	admissionError     error
}

type deletionHook struct {
	object   core.ObjectHandle
	instance *objectInstance
	finish   func()
}

// PrepareDeferredDeletionDelivery is used before the time service reserves its
// fanout. The sender marker, not a receiver's speculative TSO removal, terminates
// global ownership; historical recipient class views survive prior RO removals.
func (r *RegistryOf[T]) PrepareDeferredDeletionDelivery(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, events []core.OutboundEvent) (core.OutboxReservation, []core.OutboxDelivery, error) {
	reservation := r.PrepareRecipientDeletionDelivery(fed, recipient, events)
	if reservation == nil {
		return nil, nil, nil
	}
	p := reservation.(*deletionDeliveryOf[T])
	port, ok := r.opts.Ownership.(interface {
		PrepareObjectDeletionAndRetentionProjected(context.Context, core.FederationName, []core.ObjectHandle, []core.ObjectHandle, func(core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (core.ObjectClassHandle, core.ObjectClassHandle, []core.AttributeHandle, error)) (core.OutboxReservation, []core.OutboxDelivery, func(context.Context, error), error)
	})
	if !ok {
		return p, nil, nil
	}
	var retiring []core.ObjectHandle
	for _, obj := range p.objects {
		if d, ok := p.st.deletions[obj]; ok && !d.Committed {
			retiring = append(retiring, obj)
		}
	}
	retained := append([]core.ObjectHandle(nil), retiring...)
	for _, delivery := range p.removals {
		retained = append(retained, delivery.Event.(*CallbackEventOf[T]).Object)
	}
	if len(retained) == 0 {
		return p, nil, nil
	}
	owners, outcomes, finish, err := port.PrepareObjectDeletionAndRetentionProjected(ctx, fed, retiring, retained, func(h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, core.ObjectClassHandle, []core.AttributeHandle, error) {
		return p.projectDeletionOwnership(ctx, h, obj, attrs)
	})
	if err != nil {
		p.Release()
		return nil, nil, err
	}
	p.ownership, p.finishOwnership = owners, finish
	for i, outcome := range outcomes {
		if event, ok := outcome.Event.(interface{ ProjectedOwnershipCallback() core.OutboundEvent }); ok {
			outcomes[i].Event = event.ProjectedOwnershipCallback()
		}
	}
	return p, outcomes, nil
}

func (p *deletionDeliveryOf[T]) CommitOwnership() error {
	p.ownerOnce.Do(func() {
		if p.ownership != nil {
			p.ownerError = p.ownership.Commit()
			p.ownershipCommitted = p.ownerError == nil
		}
	})
	return p.ownerError
}

func (p *deletionDeliveryOf[T]) SetDeletionAdmissionError(err error) { p.admissionError = err }

func (p *deletionDeliveryOf[T]) queueFinishLocked() {
	if p.ownershipCommitted && !p.finishQueued && p.finishOwnership != nil {
		p.finishQueued = true
		p.st.deletionHooks = append(p.st.deletionHooks, deletionHook{finish: func() { p.finishOwnership(context.Background(), p.admissionError) }})
	}
}

func (r *RegistryOf[T]) PrepareDeletionDelivery(fed core.FederationName, events []core.OutboundEvent) core.OutboxReservation {
	return r.PrepareRecipientDeletionDelivery(fed, 0, events)
}

func (r *RegistryOf[T]) PrepareRecipientDeletionDelivery(fed core.FederationName, recipient core.FederateHandle, events []core.OutboundEvent) core.OutboxReservation {
	deliveries := make([]core.OutboxDelivery, 0, len(events))
	for _, event := range events {
		deliveries = append(deliveries, core.OutboxDelivery{Recipient: recipient, Event: event})
	}
	return r.PrepareRemovalDelivery(fed, deliveries)
}

func (r *RegistryOf[T]) PrepareRemovalDelivery(fed core.FederationName, deliveries []core.OutboxDelivery) core.OutboxReservation {
	var objects []core.ObjectHandle
	var removals []core.OutboxDelivery
	seen := make(map[core.ObjectHandle]bool)
	for _, delivery := range deliveries {
		event := delivery.Event
		if callback, ok := event.(*CallbackEventOf[T]); ok && callback.Kind == CallbackRemove && delivery.Recipient != 0 {
			removals = append(removals, delivery)
		}
		if callback, ok := event.(*CallbackEventOf[T]); ok && callback.Kind == CallbackRemove && callback.Metadata != nil && callback.Metadata.SentOrder == core.OrderTimestamp {
			continue
		}
		if removal, ok := event.(core.ObjectDeletionEvent); ok {
			if obj := removal.DeletionObject(); obj != 0 && !seen[obj] {
				seen[obj] = true
				objects = append(objects, obj)
			}
		}
	}
	if len(objects) == 0 && len(removals) == 0 {
		return nil
	}
	st := r.stateFor(fed)
	st.mu.Lock()
	return &deletionDeliveryOf[T]{r: r, st: st, fed: fed, objects: objects, removals: removals}
}

func (p *deletionDeliveryOf[T]) Commit() error {
	if err := p.CommitOwnership(); err != nil {
		return err
	}
	p.once.Do(func() {
		defer p.st.mu.Unlock()
		p.queueFinishLocked()
		for _, delivery := range p.removals {
			p.recordDeletionRecipientLocked(delivery)
		}
		for _, obj := range p.objects {
			if deletion, ok := p.st.deletions[obj]; ok && !deletion.Committed {
				inst := p.st.instances[obj]
				p.r.retireDeletedObjectLocked(p.fed, p.st, obj)
				p.st.deletionHooks = append(p.st.deletionHooks, deletionHook{object: obj, instance: inst})
			}
		}
	})
	return nil
}

func (p *deletionDeliveryOf[T]) Release() {
	p.ownerOnce.Do(func() {
		if p.ownership != nil {
			p.ownership.Release()
		}
	})
	p.once.Do(func() { p.queueFinishLocked(); p.st.mu.Unlock() })
}

func (r *RegistryOf[T]) FinishDeletionDelivery(fed core.FederationName) {
	st := r.stateFor(fed)
	st.mu.Lock()
	if len(st.deletions) == 0 && len(st.deletionHooks) == 0 {
		st.mu.Unlock()
		return
	}
	needTime := deletionRecipientsNeedTimeLocked(st)
	st.mu.Unlock()
	var timeStates []core.TimeFederateStateOf[T]
	if clock, ok := r.opts.TSOGate.(interface {
		Snapshot(core.FederationName) core.TimeSnapshotOf[T]
	}); ok && needTime {
		timeStates = clock.Snapshot(fed).Federates
	}
	st.mu.Lock()
	finishDeletionRecipientsLocked(st, timeStates)
	pruneDeletionTombstonesLockedOf[T](st)
	var objects []core.ObjectHandle
	var finishes []func()
	var deferred []deletionHook
	for _, hook := range st.deletionHooks {
		if hook.finish != nil {
			finishes = append(finishes, hook.finish)
			continue
		}
		if hook.instance != nil && !hook.instance.deliveryMu.TryLock() {
			deferred = append(deferred, hook)
			continue
		}
		if hook.instance != nil {
			hook.instance.deliveryMu.Unlock()
		}
		objects = append(objects, hook.object)
	}
	st.deletionHooks = deferred
	st.mu.Unlock()
	for _, finish := range finishes {
		finish()
	}
	if r.opts.OnDelete != nil {
		for _, obj := range objects {
			r.opts.OnDelete(fed, obj)
		}
	}
}

func pruneDeletionTombstonesLockedOf[T core.LogicalTimeScalar](st *federationStateOf[T]) {
	known := make(map[core.ObjectHandle]bool)
	for key := range st.discovered {
		known[key.obj] = true
	}
	for object, deletion := range st.deletions {
		if !deletion.Committed {
			continue
		}
		if !known[object] {
			delete(st.deletions, object)
			continue
		}
		deletion.Recipients = slices.DeleteFunc(deletion.Recipients, func(recipient DeletionRecipientState) bool { return recipient.Forgotten })
		st.deletions[object] = deletion
	}
}

// retireDeletedObjectLocked only changes RTI-owned object/DDM state. Ownership
// and declaration hooks are deliberately deferred until all time locks are free.
func (r *RegistryOf[T]) retireDeletedObjectLocked(fed core.FederationName, st *federationStateOf[T], obj core.ObjectHandle) {
	d, timed := st.deletions[obj]
	if inst := st.instances[obj]; inst != nil {
		if timed {
			d.ObjectName, d.ObjectClass = inst.name, inst.cls
		}
		delete(st.nameToHandle, inst.name)
		r.reservations.ForgetRegistered(fed, inst.name)
	}
	delete(st.instances, obj)
	for k := range st.discovered {
		if k.obj == obj && (!timed || k.sub == d.Producer) {
			st.forgetKnowledgeLocked(k)
		}
	}
	for k := range st.knowledgeEpochs {
		if k.obj == obj && (!timed || k.sub == d.Producer) {
			st.forgetKnowledgeLocked(k)
		}
	}
	delete(st.scope, obj)
	delete(st.attributeOrders, obj)
	r.retireDeletedTransportation(fed, obj)
	r.forgetObjectRegions(fed, obj)
	if timed {
		d.Committed = true
		d.OwnershipSnapshot = false
		d.AttributeOwners = nil
		st.deletions[obj] = d
	}
}

// RetractWithError validates the originating TSO service and cancels a timed
// deletion under the same evaluator/outbox transaction as message retraction.
func (r *RegistryOf[T]) RetractWithError(ctx context.Context, fed core.FederationName, sender core.FederateHandle, id uint64) error {
	st := r.stateFor(fed)
	st.mu.Lock()
	var object core.ObjectHandle
	for obj, d := range st.deletions {
		if d.Producer == sender && d.RetractionHandle == id && id != 0 {
			object = obj
			break
		}
	}
	st.mu.Unlock()
	gate, ok := r.opts.TSOGate.(interface {
		RetractWithCleanup(context.Context, core.FederationName, core.FederateHandle, uint64, func() (core.OutboxReservation, error)) error
	})
	if !ok {
		return core.ErrRetractionHistoryUnavailable
	}
	if object == 0 {
		return gate.RetractWithCleanup(ctx, fed, sender, id, nil)
	}
	err := gate.RetractWithCleanup(ctx, fed, sender, id, func() (core.OutboxReservation, error) {
		return r.prepareDeletionRetraction(st, fed, object)
	})
	if err == nil {
		if observer, ok := r.opts.Ownership.(interface {
			NotifyOwnershipChanged(context.Context, core.FederationName, core.ObjectHandle)
		}); ok {
			observer.NotifyOwnershipChanged(ctx, fed, object)
		}
	}
	return err
}

type deletionRetractionOf[T core.LogicalTimeScalar] struct {
	st        *federationStateOf[T]
	object    core.ObjectHandle
	once      sync.Once
	ownership core.OutboxReservation
}

func (p *deletionRetractionOf[T]) Commit() error {
	p.once.Do(func() {
		if p.ownership != nil {
			_ = p.ownership.Commit()
		}
		for _, recipient := range p.st.deletions[p.object].Recipients {
			cls := recipient.KnownClass
			if cls == 0 {
				cls = p.st.registeredClass(p.object)
			}
			p.st.discovered[discoverKey{obj: p.object, sub: recipient.Recipient}] = cls
		}
		delete(p.st.deletions, p.object)
		p.st.mu.Unlock()
	})
	return nil
}

func (p *deletionRetractionOf[T]) Release() {
	p.once.Do(func() {
		if p.ownership != nil {
			p.ownership.Release()
		}
		p.st.mu.Unlock()
	})
}

func (p *deletionDeliveryOf[T]) projectDeletionOwnership(ctx context.Context, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (core.ObjectClassHandle, core.ObjectClassHandle, []core.AttributeHandle, error) {
	r, fed := p.r, p.fed
	inst := p.st.instances[obj]
	if inst == nil {
		return 0, 0, nil, core.ErrObjectHandleInvalid
	}
	known := p.st.discovered[discoverKey{sub: h, obj: obj}]
	if known == 0 {
		for _, prior := range p.st.deletions[obj].Recipients {
			if prior.Recipient == h {
				known = prior.KnownClass
				if known == 0 {
					known = inst.cls
				}
				break
			}
		}
	}
	if known == 0 {
		return 0, 0, nil, core.ErrObjectHandleInvalid
	}
	fom, err := r.transportationFOM(ctx, fed)
	if err != nil {
		return 0, 0, nil, err
	}
	mapped := make([]core.AttributeHandle, len(attrs))
	for i, attr := range attrs {
		if err := transportationAttributeDefined(fom, inst.cls, attr); err != nil {
			return 0, 0, nil, err
		}
		var valid bool
		mapped[i], valid = r.attributeAtClass(ctx, fed, inst.cls, known, attr)
		if !valid {
			return 0, 0, nil, core.ErrAttributeNotFound
		}
	}
	return inst.cls, known, mapped, nil
}

func (p *deletionDeliveryOf[T]) recordDeletionRecipientLocked(delivery core.OutboxDelivery) {
	event := delivery.Event.(*CallbackEventOf[T])
	receivedTSO := event.Metadata != nil && event.Metadata.ReceivedOrder == core.OrderTimestamp
	if d, ok := p.st.deletions[event.Object]; ok {
		if d.ObjectName == "" {
			if instance := p.st.instances[event.Object]; instance != nil {
				d.ObjectName, d.ObjectClass = instance.name, instance.cls
			}
		}
		found := false
		for _, prior := range d.Recipients {
			found = found || prior.Recipient == delivery.Recipient
		}
		if !found {
			cls := p.st.discovered[discoverKey{obj: event.Object, sub: delivery.Recipient}]
			if cls == d.ObjectClass {
				cls = 0
			}
			d.Recipients = append(d.Recipients, DeletionRecipientState{Recipient: delivery.Recipient, ReceivedTSO: receivedTSO, Forgotten: !receivedTSO, KnownClass: cls})
			slices.SortFunc(d.Recipients, func(a, b DeletionRecipientState) int {
				if a.Recipient < b.Recipient {
					return -1
				}
				if a.Recipient > b.Recipient {
					return 1
				}
				return 0
			})
			p.st.deletions[event.Object] = d
		}
	}
	if !receivedTSO {
		p.st.forgetKnowledgeLocked(discoverKey{obj: event.Object, sub: delivery.Recipient})
	}
}

func finishDeletionRecipientsLocked[T core.LogicalTimeScalar](st *federationStateOf[T], timeStates []core.TimeFederateStateOf[T]) {
	for object, deletion := range st.deletions {
		for i := range deletion.Recipients {
			recipient := &deletion.Recipients[i]
			if recipient.Forgotten {
				continue
			}
			for _, state := range timeStates {
				if state.Handle == recipient.Recipient && (!state.Constrained || state.CurrentTime >= deletion.Timestamp) {
					recipient.Forgotten = true
					st.forgetKnowledgeLocked(discoverKey{obj: object, sub: recipient.Recipient})
					break
				}
			}
		}
		st.deletions[object] = deletion
	}
}

func (r *RegistryOf[T]) retireDeletedTransportation(fed core.FederationName, obj core.ObjectHandle) {
	r.transports.mu.Lock()
	r.transports.retireTicketsLocked(func(c core.TransportationChange) bool { return c.Scope.Member.Federation == fed && c.Object == obj })
	for k := range r.transports.attr {
		if k.fed == fed && k.obj == obj {
			delete(r.transports.attr, k)
		}
	}
	r.transports.mu.Unlock()
}

func (r *RegistryOf[T]) prepareDeletionRetraction(st *federationStateOf[T], fed core.FederationName, object core.ObjectHandle) (core.OutboxReservation, error) {
	st.mu.Lock()
	d, exists := st.deletions[object]
	if !exists {
		st.mu.Unlock()
		return nil, nil
	}
	if d.Committed {
		st.mu.Unlock()
		return nil, core.ErrRetractionTooLate
	}
	var ownership core.OutboxReservation
	if r.opts.Ownership != nil {
		port, ok := r.opts.Ownership.(interface {
			PrepareObjectReconstitution(core.FederationName, core.ObjectHandle, map[core.AttributeHandle]core.FederateHandle) core.OutboxReservation
		})
		if !ok || !d.OwnershipSnapshot {
			st.mu.Unlock()
			return nil, core.ErrRetractionHistoryUnavailable
		}
		owners := make(map[core.AttributeHandle]core.FederateHandle, len(d.AttributeOwners))
		for _, owner := range d.AttributeOwners {
			owners[owner.Attribute] = owner.Owner
		}
		ownership = port.PrepareObjectReconstitution(fed, object, owners)
		if ownership == nil {
			st.mu.Unlock()
			return nil, core.ErrRetractionHistoryUnavailable
		}
	}
	return &deletionRetractionOf[T]{st: st, object: object, ownership: ownership}, nil
}

func deletionRecipientsNeedTimeLocked[T core.LogicalTimeScalar](st *federationStateOf[T]) bool {
	needTime := false
	for _, deletion := range st.deletions {
		for _, recipient := range deletion.Recipients {
			needTime = needTime || !recipient.Forgotten
		}
	}
	return needTime
}
