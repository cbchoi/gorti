// Package object — DeleteObjectInstance (M23 W1 / TASK-247).
//
// IEEE 1516.1-2010 §6.16: owner federate signals that an object instance
// is no longer current. The RTI emits RemoveObjectInstance to every
// federate that subscribes to the class (excluding the owner itself),
// records ObjectDeleted in the event log (write-ahead), and removes the
// instance from per-federation state.
package object

import (
	"context"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Delete removes obj from the federation. The deleter must hold the current
// HLAprivilegeToDeleteObject ownership. Known recipients receive a removal.
//
// Errors:
//   - core.ErrObjectHandleInvalid if obj is not registered
//   - core.ErrObjectNotOwned if the deleter lacks deletion privilege
//
// The journal append precedes state mutation and callback fanout.
// Replay re-reads from the log; see SRS NFR-DET-1.
func (r *RegistryOf[T]) Delete(
	ctx context.Context,
	fed core.FederationName,
	deleter core.FederateHandle,
	obj core.ObjectHandle,
	ts *T,
	tag []byte,
) error {
	return r.deleteWithMetadata(ctx, fed, deleter, obj, ts, tag, 0)
}

func (r *RegistryOf[T]) deleteWithMetadata(ctx context.Context, fed core.FederationName, deleter core.FederateHandle, obj core.ObjectHandle, ts *T, tag []byte, retraction uint64) (result error) {
	if err := validateDeletionRequest(deleter, ts, retraction); err != nil {
		return err
	}

	st := r.stateFor(fed)
	st.mu.Lock()
	inst, ok := st.instances[obj]
	st.mu.Unlock()
	if !ok {
		return core.ErrObjectHandleInvalid
	}
	inst.deliveryMu.Lock()
	deliveryLocked := true
	defer func() {
		if deliveryLocked {
			inst.deliveryMu.Unlock()
		}
	}()
	subs, seq, err := r.prepareDeletionRecipients(ctx, fed, st, deleter, inst)
	if err != nil {
		return err
	}
	deliveryTs := r.deletionDeliveryTime(ctx, fed, inst, ts)
	if err := r.validateOutgoingTSO(fed, deleter, deliveryTs); err != nil {
		return err
	}
	admission, err := r.reserveRetractionID(ctx, fed, deleter, retraction, deliveryTs)
	if err != nil {
		return err
	}
	if admission != nil {
		defer admission.Release()
	}
	fanout, immediate, err := r.prepareRemoval(ctx, fed, deleter, inst, subs, seq, deliveryTs, ts, tag, retraction)
	if err != nil {
		return fmt.Errorf("object: delete admission: %w", err)
	}
	fanout.retractionAdmission = admission
	defer func() {
		fanout.abort()
		if deliveryLocked {
			inst.deliveryMu.Unlock()
			deliveryLocked = false
		}
		fanout.finish(ctx, result)
	}()
	if err := r.journalDeletion(ctx, fed, obj, ts); err != nil {
		return err
	}
	if err := r.commitDeletionState(ctx, fed, st, inst, deleter, ts, deliveryTs, tag, retraction, immediate, fanout); err != nil {
		return err
	}
	if err := r.commitDeletionDelivery(ctx, fed, st, inst, fanout, immediate); err != nil {
		return err
	}
	inst.deliveryMu.Unlock()
	deliveryLocked = false
	r.FinishDeletionDelivery(fed)
	if immediate && r.opts.OnDelete != nil {
		r.opts.OnDelete(fed, obj)
	}
	return nil
}

func (r *RegistryOf[T]) prepareRemoval(ctx context.Context, fed core.FederationName, producer core.FederateHandle, inst *objectInstance, subs []core.FederateHandle, seq uint64, ts, originalTime *T, tag []byte, retraction uint64) (*preparedRemovalOf[T], bool, error) {
	p := &preparedRemovalOf[T]{preparedAttributeFanoutOf: &preparedAttributeFanoutOf[T]{registry: r, fed: fed, producer: producer, retraction: retraction}}
	immediate := ts == nil || r.opts.TSOGate == nil
	_, canReserveTSO := r.opts.TSOGate.(core.ReservableTSODeliveryGateOf[T])
	timed := p.removalMessages(inst, subs, seq, ts, originalTime, tag, canReserveTSO)
	if canReserveTSO || len(timed) > 0 {
		var err error
		immediate, err = p.prepareRemovalTSO(ts, timed, canReserveTSO, immediate)
		if err != nil {
			return nil, false, err
		}
	}
	if len(p.buffered) > 0 && (len(p.buffered) > 1 || len(p.immediate) > 0) {
		p.abort()
		return nil, false, fmt.Errorf("atomic deletion requires reservable TSO gate")
	}
	{
		if err := p.prepareOwnership(ctx, inst.handle, immediate); err != nil {
			p.abort()
			return nil, false, err
		}
	}
	if err := p.reserveRemovalOutbox(ctx); err != nil {
		p.abort()
		return nil, false, err
	}
	return p, immediate, nil
}

// newObjectDeletedEvent builds the eventlog adapter for an
// ObjectDeleted event. Mirrors newObjectRegisteredEvent.
func newObjectDeletedEventOf[T core.LogicalTimeScalar](handle core.ObjectHandle, ts *T, wallNs int64) *core.ObjectJournalRecordOf[T] {
	return &core.ObjectJournalRecordOf[T]{
		Kind:        JournalObjectDeleted,
		WallNS:      journalWallBits(wallNs),
		Object:      handle,
		LogicalTime: copiedLogicalTimeOf[T](ts),
	}
}

func (r *RegistryOf[T]) prepareDeletionRecipients(ctx context.Context, fed core.FederationName, st *federationStateOf[T], deleter core.FederateHandle, inst *objectInstance) ([]core.FederateHandle, uint64, error) {
	obj := inst.handle
	st.mu.Lock()
	ok := st.instances[obj] == inst
	if !ok {
		st.mu.Unlock()
		// Distinguish never-existed (Invalid) from previously-deleted
		// (AlreadyDeleted) only if we tracked tombstones; cut-1 keeps
		// it simple — both surface as Invalid.
		return nil, 0, core.ErrObjectHandleInvalid
	}
	// The RTI's MOM producer is not a joined federate and has no discovery
	// record. It may retire only its own internally registered instances.
	internalOwner := deleter == internalProducer && inst.owner == internalProducer
	if _, known := st.discovered[discoverKey{sub: deleter, obj: obj}]; !known && !internalOwner {
		st.mu.Unlock()
		return nil, 0, core.ErrObjectHandleInvalid
	}
	if !r.hasDeletePrivilege(ctx, fed, deleter, inst) {
		st.mu.Unlock()
		return nil, 0, core.ErrObjectNotOwned
	}
	if _, pending := st.deletions[obj]; pending {
		st.mu.Unlock()
		return nil, 0, core.ErrObjectDeletionPending
	}

	// Knowledge survives an unsubscribe. Include the original registrant when
	// another federate has acquired the privilege to delete the instance.
	var subs []core.FederateHandle
	for k := range st.discovered {
		if k.obj == obj {
			subs = append(subs, k.sub)
		}
	}
	slices.Sort(subs)
	seq := st.nextOutboundSeqRangeLocked(len(subs))
	st.mu.Unlock()
	return subs, seq, nil
}

func (r *RegistryOf[T]) applyDeletionStateLocked(ctx context.Context, fed core.FederationName, st *federationStateOf[T], inst *objectInstance, deleter core.FederateHandle, ts, deliveryTs *T, tag []byte, retraction uint64, immediate bool, fanout *preparedRemovalOf[T]) {
	obj := inst.handle
	if deliveryTs != nil {
		if st.deletions == nil {
			st.deletions = make(map[core.ObjectHandle]DeletionStateOf[T])
		}
		deletion := DeletionStateOf[T]{Object: obj, ObjectName: inst.name, ObjectClass: inst.cls, Producer: deleter, Timestamp: *ts, RetractionHandle: retraction, Committed: immediate, Tag: slices.Clone(tag)}
		if retraction != 0 && !immediate {
			r.capturePreparedDeletionOwners(ctx, fed, inst, &deletion, fanout)
		}
		st.deletions[obj] = deletion
	}
	if immediate {
		delete(st.instances, obj)
		delete(st.nameToHandle, inst.name)
	}
}

func (r *RegistryOf[T]) capturePreparedDeletionOwners(ctx context.Context, fed core.FederationName, inst *objectInstance, deletion *DeletionStateOf[T], fanout *preparedRemovalOf[T]) {
	obj := inst.handle
	if pin, ok := fanout.ownership.(interface {
		PreparedAttributeOwners(core.ObjectHandle, []core.AttributeHandle) map[core.AttributeHandle]core.FederateHandle
	}); ok {
		deletion.OwnershipSnapshot = true
		for attr, owner := range pin.PreparedAttributeOwners(obj, r.classAttributes(ctx, fed, inst.cls)) {
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
	} else {
		r.captureDeletionOwners(ctx, fed, inst, deletion)
	}
}

func (r *RegistryOf[T]) commitDeletionDelivery(ctx context.Context, fed core.FederationName, st *federationStateOf[T], inst *objectInstance, fanout *preparedRemovalOf[T], immediate bool) error {
	obj := inst.handle
	if err := fanout.commit(ctx); err != nil {
		st.mu.Lock()
		st.instances[obj] = inst
		st.nameToHandle[inst.name] = obj
		delete(st.deletions, obj)
		st.mu.Unlock()
		return fmt.Errorf("object: delete delivery: %w", err)
	}
	if immediate {
		st.mu.Lock()
		// Restore the name temporarily for the common complete cleanup path.
		st.instances[obj] = inst
		r.retireDeletedObjectLocked(fed, st, obj)
		st.mu.Unlock()
	}
	return nil
}

func (p *preparedRemovalOf[T]) removalMessages(inst *objectInstance, subs []core.FederateHandle, seq uint64, ts, originalTime *T, tag []byte, canReserveTSO bool) []core.MessageDeliveryOf[T] {
	r, fed, producer, retraction := p.registry, p.fed, p.producer, p.retraction
	st := r.stateFor(fed)
	st.mu.Lock()
	classes := make(map[core.FederateHandle]core.ObjectClassHandle, len(subs))
	for _, sub := range subs {
		classes[sub] = st.knownClass(sub, inst)
	}
	st.mu.Unlock()
	var timed []core.MessageDeliveryOf[T]
	for _, sub := range subs {
		if sub == producer {
			continue
		}
		// The message sender owns deletion privilege, but 6.15's producing
		// federate is the original registrant, even after privilege transfer.
		event := &CallbackEventOf[T]{Sequence: seq, Kind: CallbackRemove, Object: inst.handle, ObjectClass: classes[sub], LogicalTime: copiedLogicalTimeOf[T](originalTime), UserSuppliedTag: slices.Clone(tag), Metadata: metadataForOf[T](inst.owner, ts, 0)}
		if ts != nil {
			event.Metadata.RetractionHandle = retraction
		}
		seq++
		if canReserveTSO || ts != nil && r.opts.TSOGate != nil {
			timed = append(timed, messageDeliveryOf[T](sub, producer, ts, event, retraction))
		} else {
			p.appendImmediate(core.OutboxDelivery{Recipient: sub, Event: event})
		}
	}
	if ts != nil && r.opts.TSOGate != nil {
		marker := &CallbackEventOf[T]{Kind: CallbackDeleteMarker, Object: inst.handle, LogicalTime: copiedLogicalTimeOf[T](ts), Metadata: metadataForOf[T](producer, ts, 0)}
		marker.Metadata.RetractionHandle = retraction
		timed = append(timed, core.MessageDeliveryOf[T]{Recipient: producer, Timestamp: *ts, Event: marker, Sender: producer, RetractionHandle: retraction})
	}
	return timed
}

func (p *preparedRemovalOf[T]) prepareRemovalTSO(ts *T, timed []core.MessageDeliveryOf[T], canReserveTSO, immediate bool) (bool, error) {
	r, fed, producer := p.registry, p.fed, p.producer
	if canReserveTSO {
		var err error
		p.tsoReservation, err = r.reserveOutgoingTSO(fed, producer, ts, timed)
		if err != nil {
			return false, err
		}
		if p.tsoReservation == nil {
			return false, fmt.Errorf("nil TSO reservation")
		}
		for _, d := range p.tsoReservation.Immediate() {
			if p.appendImmediateRemoval(d) {
				immediate = true
			}
		}
	} else {
		for _, d := range timed {
			if r.opts.TSOGate.ShouldDeliverNow(fed, d.Recipient, d.Timestamp) {
				if p.appendImmediateRemoval(d) {
					immediate = true
				}
			} else {
				p.buffered = append(p.buffered, bufferedAttributeDeliveryOf[T]{sub: d.Recipient, ts: d.Timestamp, event: d.Event})
			}
		}
	}
	return immediate, nil
}

func (p *preparedRemovalOf[T]) reserveRemovalOutbox(ctx context.Context) error {
	r, fed := p.registry, p.fed
	if len(p.immediate) > 0 {
		if out, ok := r.opts.Outbox.(core.ReservableOutbox); ok {
			reservation, err := out.Reserve(ctx, fed, p.immediate)
			if err != nil {
				return err
			}
			if reservation == nil {
				return fmt.Errorf("nil outbox reservation")
			}
			p.reservation = reservation
		} else if len(p.immediate) > 1 {
			return fmt.Errorf("atomic deletion requires reservable outbox")
		}
	}
	return nil
}

func (p *preparedRemovalOf[T]) appendImmediateRemoval(d core.MessageDeliveryOf[T]) bool {
	marker := false
	if internal, ok := d.Event.(core.InternalTSOEvent); ok && internal.InternalTSO() {
		marker = true
	}
	if internal, ok := d.Event.(core.InternalTSOEvent); !ok || !internal.InternalTSO() {
		p.appendImmediate(core.OutboxDelivery{Recipient: d.Recipient, Event: d.Event})
	}
	return marker
}

func (r *RegistryOf[T]) journalDeletion(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, ts *T) error {
	if r.opts.EventLog != nil {
		if err := r.opts.EventLog.Append(ctx, fed, newObjectDeletedEventOf[T](obj, ts, r.opts.Clock.Now().UnixNano())); err != nil {
			return fmt.Errorf("object: delete journal: %w", err)
		}
	}
	return nil
}

func (r *RegistryOf[T]) commitDeletionState(ctx context.Context, fed core.FederationName, st *federationStateOf[T], inst *objectInstance, deleter core.FederateHandle, ts, deliveryTs *T, tag []byte, retraction uint64, immediate bool, fanout *preparedRemovalOf[T]) error {
	if !fanout.objectPin {
		st.mu.Lock()
	}
	r.applyDeletionStateLocked(ctx, fed, st, inst, deleter, ts, deliveryTs, tag, retraction, immediate, fanout)
	if fanout.objectPin {
		// Keep object -> ownership ordering until both state mutations commit.
		if err := fanout.ownership.Commit(); err != nil {
			return err
		}
		fanout.unlockObject()
	} else {
		st.mu.Unlock()
	}
	return nil
}

func validateDeletionRequest[T core.LogicalTimeScalar](deleter core.FederateHandle, ts *T, retraction uint64) error {
	if deleter == core.InvalidFederateHandle {
		return core.ErrFederateNotJoined
	}
	if (retraction != 0 && ts == nil) || validateFiniteMessageTimestampOf(ts) != nil {
		return core.ErrTimeInvalidLogicalTime
	}
	return nil
}
