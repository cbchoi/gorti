package federate

import (
	codec "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"math"
)

type deletionDesignator struct{ sender, id uint64 }
type deletedObjectKnowledge struct {
	object, class uint64
	owned         map[uint64]struct{}
	originated    bool
}
type sdkAdvanceRequest struct{ flush bool }

func (f *Federate) snapshotDeletedObjectLocked(object uint64) (deletedObjectKnowledge, bool) {
	class, known := f.objectClasses[object]
	saved := deletedObjectKnowledge{object: object, class: class}
	if owned := f.ownedObjectAttributes[object]; owned != nil {
		saved.owned = make(map[uint64]struct{}, len(owned))
		for attr := range owned {
			saved.owned[attr] = struct{}{}
		}
	}
	return saved, known
}

func (f *Federate) rememberDeletedObjectLocked(key deletionDesignator, saved deletedObjectKnowledge) {
	if f.deletedObjects == nil {
		f.deletedObjects = make(map[deletionDesignator]deletedObjectKnowledge)
	}
	f.deletedObjects[key] = saved
}

func (f *Federate) discardDeletionKnowledgeLocked(object uint64) {
	delete(f.pendingObjectRemovals, object)
	delete(f.pendingFinalObjectRemovals, object)
	for key, saved := range f.deletedObjects {
		if saved.object == object {
			delete(f.deletedObjects, key)
		}
	}
}

func (f *Federate) resetDeletionKnowledgeLocked() {
	f.objectKnowledgeEpoch++
	f.deletedObjects = nil
	f.pendingObjectRemovals = nil
	f.pendingFinalObjectRemovals = nil
	f.timeAdvances = nil
}

func (f *Federate) rememberDiscoveredObject(object, class uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discardDeletionKnowledgeLocked(object)
	if f.objectClasses == nil {
		f.objectClasses = make(map[uint64]uint64)
	}
	f.objectClasses[object] = class
}

// Section 6.15.4 keeps a timestamped FQR removal known until logical time
// reaches its timestamp or constraint is disabled. Received RO is different.
func (f *Federate) applyObjectRemoval(remove *rtiv1.RemoveObjectInstance) {
	final := remove.LogicalTime != nil && f.legacyFinalTime(*remove.LogicalTime, remove.LogicalTimeValue)
	timestamp, err := codec.DecodeOptionalLogicalTimePair(f.timeRepresentation(), remove.LogicalTime, remove.LogicalTimeValue)
	if err != nil && !final {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	object, metadata := remove.ObjectHandle, remove.Metadata
	flush := f.flushRemovalLocked(metadata, timestamp != nil || final)
	saved, known := f.snapshotDeletedObjectLocked(object)
	retractable := retractableRemoval(metadata, timestamp != nil || final, flush)
	if !retractable {
		f.discardDeletionKnowledgeLocked(object)
	}
	if known && !f.localLRCClosing.Load() && retractable {
		f.rememberDeletedObjectLocked(deletionDesignator{sender: metadata.GetProducingFederate(), id: metadata.GetMessageRetractionHandle()}, saved)
	}
	if flush && known {
		if final {
			if f.pendingFinalObjectRemovals == nil {
				f.pendingFinalObjectRemovals = make(map[uint64]struct{})
			}
			delete(f.pendingObjectRemovals, object)
			f.pendingFinalObjectRemovals[object] = struct{}{}
			return
		}
		delete(f.pendingFinalObjectRemovals, object)
		if f.pendingObjectRemovals == nil {
			f.pendingObjectRemovals = make(map[uint64]logicaltime.Time)
		}
		f.pendingObjectRemovals[object] = *timestamp
		return
	}
	delete(f.objectClasses, object)
	delete(f.ownedObjectAttributes, object)
}

func (f *Federate) restoreDeletedObject(key deletionDesignator, originated bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.localLRCClosing.Load() {
		f.restoreDeletedObjectLocked(key, originated)
	}
}

func (f *Federate) restoreDeletedObjectLocked(key deletionDesignator, originated bool) {
	saved, exists := f.deletedObjects[key]
	if !exists || saved.originated != originated {
		return
	}
	delete(f.deletedObjects, key)
	delete(f.pendingObjectRemovals, saved.object)
	delete(f.pendingFinalObjectRemovals, saved.object)
	if _, alreadyKnown := f.objectClasses[saved.object]; alreadyKnown {
		return
	}
	if f.objectClasses == nil {
		f.objectClasses = make(map[uint64]uint64)
	}
	f.objectClasses[saved.object] = saved.class
	if saved.owned != nil {
		if f.ownedObjectAttributes == nil {
			f.ownedObjectAttributes = make(map[uint64]map[uint64]struct{})
		}
		f.ownedObjectAttributes[saved.object] = saved.owned
	}
}

// Serialize RPC admission, not callback processing. A later accepted request
// can precede receipt of an earlier grant; the queue preserves that boundary.
func (f *Federate) beginSDKAdvance(flush bool) *sdkAdvanceRequest {
	f.timeRequestMu.Lock()
	f.mu.Lock()
	defer f.mu.Unlock()
	request := &sdkAdvanceRequest{flush: flush}
	f.timeAdvances = append(f.timeAdvances, request)
	return request
}

func (f *Federate) finishSDKAdvance(request *sdkAdvanceRequest, err error) {
	if err == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for index, pending := range f.timeAdvances {
		if pending == request {
			f.timeAdvances = append(f.timeAdvances[:index], f.timeAdvances[index+1:]...)
			break
		}
	}
}

func (f *Federate) completeSDKGrant(at float64) {
	if math.IsInf(at, 1) && f.timeRepresentation() == logicaltime.HLAfloat64Time {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.timeAdvances) != 0 {
			f.timeAdvances = f.timeAdvances[1:]
		}
		f.retireSDKFinalRemovalsLocked()
		return
	}
	value, err := logicaltime.Float64(at)
	if err != nil {
		return
	}
	f.completeSDKGrantValue(value)
}

func (f *Federate) completeSDKGrantValue(at logicaltime.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.timeAdvances) != 0 {
		f.timeAdvances = f.timeAdvances[1:]
	}
	f.retireSDKRemovalsLocked(at)
}

func (f *Federate) observeSDKEnabledTime(at float64) {
	if math.IsInf(at, 1) && f.timeRepresentation() == logicaltime.HLAfloat64Time {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.retireSDKFinalRemovalsLocked()
		return
	}
	value, err := logicaltime.Float64(at)
	if err != nil {
		return
	}
	f.observeSDKEnabledTimeValue(value)
}

func (f *Federate) retireSDKFinalRemovalsLocked() {
	for object := range f.pendingObjectRemovals {
		delete(f.objectClasses, object)
		delete(f.ownedObjectAttributes, object)
		f.discardDeletionKnowledgeLocked(object)
	}
	for object := range f.pendingFinalObjectRemovals {
		delete(f.objectClasses, object)
		delete(f.ownedObjectAttributes, object)
		f.discardDeletionKnowledgeLocked(object)
	}
}

func (f *Federate) observeSDKEnabledTimeValue(at logicaltime.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retireSDKRemovalsLocked(at)
}

func (f *Federate) retireSDKRemovalsLocked(at logicaltime.Time) {
	for object, timestamp := range f.pendingObjectRemovals {
		if comparison, err := at.Compare(timestamp); err == nil && comparison >= 0 {
			delete(f.objectClasses, object)
			delete(f.ownedObjectAttributes, object)
			f.discardDeletionKnowledgeLocked(object)
		}
	}
}

func (f *Federate) completeSDKConstraintDisable(epoch uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.objectKnowledgeEpoch != epoch {
		return
	}
	for object := range f.pendingObjectRemovals {
		delete(f.objectClasses, object)
		delete(f.ownedObjectAttributes, object)
		// The sender may still retract; retain the designator's saved state.
		delete(f.pendingObjectRemovals, object)
	}
	for object := range f.pendingFinalObjectRemovals {
		delete(f.objectClasses, object)
		delete(f.ownedObjectAttributes, object)
		delete(f.pendingFinalObjectRemovals, object)
	}
}

func (f *Federate) flushRemovalLocked(metadata *rtiv1.ObjectCallbackMetadata, timed bool) bool {
	return len(f.timeAdvances) != 0 && f.timeAdvances[0].flush && timed && metadata != nil && metadata.GetReceivedOrder() == rtiv1.OrderType_ORDER_TYPE_TIMESTAMP
}

func retractableRemoval(metadata *rtiv1.ObjectCallbackMetadata, timed, flush bool) bool {
	return metadata != nil && metadata.GetSentOrder() == rtiv1.OrderType_ORDER_TYPE_TIMESTAMP && timed && metadata.GetProducingFederate() != 0 && metadata.GetMessageRetractionHandle() != 0 && (flush || metadata.GetReceivedOrder() == rtiv1.OrderType_ORDER_TYPE_RECEIVE)
}
