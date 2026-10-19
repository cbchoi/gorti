package savebundle

import (
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/proto"
)

func (v *graphValidationOf[T]) validateTime(data []byte) error {
	if integerScalar[T]() {
		return validateTimeWireOf[T, integerSnapshotNumber](v, data)
	}
	return validateTimeWireOf[T, timeSnapshotNumber](v, data)
}

func validateTimeWireOf[T, N core.LogicalTimeScalar](v *graphValidationOf[T], data []byte) error {
	if len(data) == 0 {
		return validateDeletionRecipientTimesOf[T, N](v, nil)
	}
	var s timeSnapshotOf[N]
	if err := decodeSnapshotJSON(data, &s); err != nil {
		return err
	}
	if err := validateTimeSnapshotVersionOf[N](s); err != nil {
		return err
	}
	seen := map[core.FederateHandle]bool{}
	for _, f := range s.Federates {
		if !v.member(f.Handle) || seen[f.Handle] {
			return fmt.Errorf("time state for missing/duplicate participant")
		}
		seen[f.Handle] = true
		for i, queue := range [][]timeQueuedSnapshotOf[N]{f.Queue, f.ROQueue} {
			ro := i == 1
			for _, q := range queue {
				if err := validateQueuedTimeOf(v, f.Handle, q, ro); err != nil {
					return err
				}
			}
		}
	}
	return validateDeletionRecipientTimesOf[T, N](v, s.Federates)
}

func (v *graphValidationOf[T]) validateSync(data []byte) error {
	if isStrictSynchronization(data) {
		return v.validateStrictSynchronization(data)
	}
	var s rtiv1.SyncManagerState
	if err := proto.Unmarshal(data, &s); err != nil {
		return err
	}
	labels := map[string]bool{}
	for _, p := range s.Points {
		if p == nil || p.Label == "" || labels[p.Label] || (p.State != rtiv1.SyncPointSnapshotState_SYNC_POINT_SNAPSHOT_STATE_ANNOUNCED && p.State != rtiv1.SyncPointSnapshotState_SYNC_POINT_SNAPSHOT_STATE_ACHIEVED) {
			return fmt.Errorf("invalid synchronization point")
		}
		labels[p.Label] = true
		if err := validateSyncFailureHistory(p); err != nil {
			return err
		}
		if err := validateSyncParticipantSets(p.Required, p.Achieved, p.State); err != nil {
			return err
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateMOM(fed core.FederationName, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	wire, runtime, err := decodeMOMGraph(data)
	if err != nil {
		return err
	}
	if wire.Federation == nil || wire.Federation.Name != string(fed) {
		return fmt.Errorf("MOM federation mismatch")
	}
	if err := v.validateMOMRoster(wire); err != nil {
		return err
	}
	if runtime == nil {
		return nil
	}
	if len(runtime.Federates) != len(v.members) {
		return fmt.Errorf("MOM runtime participant count mismatch")
	}

	if err := v.validateMOMObjects(runtime.Objects); err != nil {
		return err
	}
	if err := v.validateMOMRuntimeFederates(runtime); err != nil {
		return err
	}
	return v.validateMOMTraffic(fed, runtime.Traffic)
}

func (v *graphValidationOf[T]) validateHistoricalObject(object core.ObjectHandle, class core.ObjectClassHandle) error {
	if _, ok := v.names.ObjectClassName(class); !ok || object == 0 || uint64(object) > v.lastObject {
		return fmt.Errorf("MOM historical object/class mismatch")
	}
	if live, ok := v.objects[object]; ok && live.Class != class {
		return fmt.Errorf("MOM history conflicts with live object")
	}
	return nil
}

func (v *graphValidationOf[T]) validateHistoricalReflection(object core.ObjectHandle, class core.ObjectClassHandle) error {
	if _, ok := v.names.ObjectClassName(class); !ok || object == 0 || uint64(object) > v.lastObject {
		return fmt.Errorf("MOM historical reflection object/class mismatch")
	}
	// Earlier callbacks may precede local deletion and rediscovery at a new class.
	if live, ok := v.objects[object]; ok && !v.validKnownClass(live.Class, class) {
		return fmt.Errorf("MOM reflection history conflicts with live object ancestry")
	}
	return nil
}

func validateAutomaticResignGraph(data []byte, names map[string]bool) error {
	var state federation.AutomaticResignState
	if err := decodeSnapshotJSON(data, &state); err != nil {
		return err
	}
	if err := federation.ValidateAutomaticResignState(state); err != nil {
		return err
	}
	if len(state.Directives) != len(names) {
		return fmt.Errorf("automatic resign participant count mismatch")
	}
	for _, d := range state.Directives {
		if !names[d.Name] {
			return fmt.Errorf("automatic resign directive for unknown saved name")
		}
	}
	return nil
}

func validateQueuedTimeOf[T, N core.LogicalTimeScalar](v *graphValidationOf[T], recipient core.FederateHandle, q timeQueuedSnapshotOf[N], ro bool) error {
	if err := validateQueuedTimeIdentityOf(v, recipient, q, ro); err != nil {
		return err
	}
	e := q.Event
	switch e.Kind {
	case checkpointReceive:
		if err := validateQueuedInteractionOf(v, q); err != nil {
			return err
		}
	case checkpointReflect:
		if err := validateQueuedReflectionOf(v, q); err != nil {
			return err
		}
	case checkpointRemove, checkpointDeleteMarker:
		if err := validateQueuedRemovalOf(v, q, ro); err != nil {
			return err
		}
	}
	if q.Sender == ^core.FederateHandle(0) && (e.Kind != checkpointReflect || !v.management(e.ObjectClass)) {
		return fmt.Errorf("invalid internal queued producer")
	}
	return nil
}

func validateQueuedTimeIdentityOf[T, N core.LogicalTimeScalar](v *graphValidationOf[T], recipient core.FederateHandle, q timeQueuedSnapshotOf[N], ro bool) error {
	if q.Destination != recipient || !v.accepted(q.Sender, q.RetractionHandle) {
		return fmt.Errorf("queued destination or accepted retraction identity mismatch")
	}
	if message, ok := v.messages[[2]uint64{uint64(q.Sender), q.RetractionHandle}]; ok && (message.Retracted || message.Timestamp != T(q.Timestamp)) {
		return fmt.Errorf("queued message conflicts with retraction ledger")
	}
	e := q.Event
	if _, err := decodeTimeDeliveryOf[T, N](q, ro); err != nil {
		return err
	}
	if e.Metadata != nil {
		if e.Metadata.Producer != 0 && e.Metadata.Producer != q.Sender || e.Metadata.RetractionHandle != 0 && e.Metadata.RetractionHandle != q.RetractionHandle {
			return fmt.Errorf("queued callback producer/retraction mismatch")
		}
	}
	return nil
}

func validateQueuedInteractionOf[T, N core.LogicalTimeScalar](v *graphValidationOf[T], q timeQueuedSnapshotOf[N]) error {
	e := q.Event
	if _, ok := v.names.InteractionClassName(e.InteractionClass); !ok {
		return fmt.Errorf("queued unknown interaction class")
	}
	for p := range e.Parameters {
		if _, ok := v.names.ParameterName(e.InteractionClass, core.ParameterHandle(p)); !ok {
			return fmt.Errorf("queued unknown interaction parameter")
		}
	}
	return nil
}

func validateQueuedReflectionOf[T, N core.LogicalTimeScalar](v *graphValidationOf[T], q timeQueuedSnapshotOf[N]) error {
	e := q.Event
	cls, err := queuedReflectionClassOf(v, q)
	if err != nil {
		return err
	}
	if _, ok := v.names.ObjectClassName(cls); !ok {
		return fmt.Errorf("queued unknown object class")
	}
	for a := range e.Attributes {
		if !v.attribute(cls, core.AttributeHandle(a)) {
			return fmt.Errorf("queued unknown object attribute")
		}
	}
	for _, a := range e.AttributeHandles {
		if !v.attribute(cls, a) {
			return fmt.Errorf("queued unknown attribute reference")
		}
	}
	return nil
}

func validateQueuedRemovalOf[T, N core.LogicalTimeScalar](v *graphValidationOf[T], q timeQueuedSnapshotOf[N], ro bool) error {
	e := q.Event
	if e.Object == 0 || uint64(e.Object) > v.lastObject {
		return fmt.Errorf("queued removal of never-allocated object")
	}
	if err := validateQueuedRemovalClassOf(v, q); err != nil {
		return err
	}
	deletion, found := v.deletions[e.Object]
	if !ro && found && (deletion.Producer != q.Sender || deletion.Timestamp != T(q.Timestamp) || deletion.RetractionHandle != q.RetractionHandle) {
		return fmt.Errorf("queued deletion identity mismatch")
	}
	if e.Kind == checkpointDeleteMarker && (!found || q.Destination != q.Sender) {
		return fmt.Errorf("internal deletion marker without matching intent")
	}
	return nil
}

func queuedReflectionClassOf[T, N core.LogicalTimeScalar](v *graphValidationOf[T], q timeQueuedSnapshotOf[N]) (core.ObjectClassHandle, error) {
	e := q.Event
	cls := e.ObjectClass
	actual := v.objects[e.Object].Class
	if actual == 0 {
		actual = v.deletions[e.Object].ObjectClass
	}
	known := v.knownClasses[[2]uint64{uint64(e.Object), uint64(q.Destination)}]
	if e.Metadata != nil && e.Metadata.DelayedSubscription {
		if cls != 0 && known != 0 && cls != known {
			return 0, fmt.Errorf("delayed callback discovery class mismatch")
		}
		var err error
		cls, err = v.delayedCanonicalClass(e.Object, known, actual, e.Metadata)
		if err != nil {
			return 0, err
		}
	} else {
		if known == 0 {
			known = actual
		}
		if known != 0 {
			if cls != 0 && cls != known {
				return 0, fmt.Errorf("queued object class mismatch")
			}
			cls = known
		} else if e.Object == 0 || uint64(e.Object) > v.lastObject {
			return 0, fmt.Errorf("queued object was never allocated")
		}
	}
	return cls, nil
}

func validateQueuedRemovalClassOf[T, N core.LogicalTimeScalar](v *graphValidationOf[T], q timeQueuedSnapshotOf[N]) error {
	e := q.Event
	if e.ObjectClass != 0 {
		if _, ok := v.names.ObjectClassName(e.ObjectClass); !ok {
			return fmt.Errorf("queued removal of unknown object class")
		}
		expected := v.objects[e.Object].Class
		if expected == 0 {
			expected = v.deletions[e.Object].ObjectClass
		}
		// Internal markers remain canonical; federate removals use
		// the class conveyed to their destination at discovery.
		if e.Kind == checkpointRemove {
			if known := v.knownClasses[[2]uint64{uint64(e.Object), uint64(q.Destination)}]; known != 0 {
				expected = known
			}
		}
		if expected != 0 && expected != e.ObjectClass {
			return fmt.Errorf("queued removal object class mismatch")
		}
	}
	return nil
}

func validateSyncParticipantSets(required, achieved []uint64, state rtiv1.SyncPointSnapshotState) error {
	sets := make([]map[uint64]bool, 2)
	for i, handles := range [][]uint64{required, achieved} {
		sets[i] = map[uint64]bool{}
		for _, h := range handles {
			if h == 0 || h == ^uint64(0) || sets[i][h] {
				return fmt.Errorf("invalid sync participant set")
			}
			sets[i][h] = true
		}
	}
	// Existing API admits achievements by non-required participants. Preserve
	// that history; only a completed point requires every required achievement.
	if state == rtiv1.SyncPointSnapshotState_SYNC_POINT_SNAPSHOT_STATE_ACHIEVED {
		for h := range sets[0] {
			if !sets[1][h] {
				return fmt.Errorf("completed sync point has outstanding participant")
			}
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateMOMRoster(wire *rtiv1.MOMManagerState) error {
	seen := map[core.FederateHandle]bool{}
	for _, h := range wire.Federation.FederateHandles {
		fh := core.FederateHandle(h)
		if !v.member(fh) || seen[fh] {
			return fmt.Errorf("MOM federation roster mismatch")
		}
		seen[fh] = true
	}
	if len(seen) != len(v.members) || len(wire.Federates) != len(v.members) {
		return fmt.Errorf("MOM participant count mismatch")
	}
	seen = map[core.FederateHandle]bool{}
	for _, f := range wire.Federates {
		if f == nil {
			return fmt.Errorf("nil MOM participant")
		}
		representation := logicaltime.HLAfloat64Time
		if integerScalar[T]() {
			representation = logicaltime.HLAinteger64Time
		}
		if _, _, err := decodeMOMTimeRecord(f, representation); err != nil {
			return fmt.Errorf("MOM time representation: %w", err)
		}
		h := core.FederateHandle(f.Handle)
		if !v.member(h) || v.members[h] != f.Name || seen[h] {
			return fmt.Errorf("MOM name/handle mismatch")
		}
		seen[h] = true
	}
	return nil
}

func (v *graphValidationOf[T]) momObjectMatches(h core.ObjectHandle, cls core.ObjectClassHandle, name string) bool {
	o, ok := v.objects[h]
	return ok && o.Class == cls && o.Name == name && o.Registrant == ^core.FederateHandle(0)
}

func (v *graphValidationOf[T]) validateMOMObjects(o mom.ObjectInstanceState) error {
	if o.Enabled {
		fedClass, ok1 := v.model.LookupObjectClass(mom.ClassHLAfederation)
		federateClass, ok2 := v.model.LookupObjectClass(mom.ClassHLAfederate)
		if !ok1 || !ok2 || fedClass != o.FederationClass || federateClass != o.FederateClass || !v.momObjectMatches(o.FederationObject, fedClass, o.FederationObjectName) {
			return fmt.Errorf("MOM federation object/FOM mismatch")
		}
		for name, h := range map[string]core.AttributeHandle{"HLAfederationName": o.AttributeFederationName, "HLAfederatesInFederation": o.AttributeFederates} {
			expected, ok := v.model.LookupAttribute(fedClass, name)
			if !ok || expected != h {
				return fmt.Errorf("MOM federation attribute mismatch")
			}
		}
		for name, h := range map[string]core.AttributeHandle{"HLAfederateHandle": o.AttributeFederateHandle, "HLAfederateName": o.AttributeFederateName, "HLAfederateType": o.AttributeFederateType} {
			expected, ok := v.model.LookupAttribute(federateClass, name)
			if !ok || expected != h {
				return fmt.Errorf("MOM federate attribute mismatch")
			}
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateMOMRuntimeFederates(runtime *momSnapshotRuntime) error {
	o := runtime.Objects
	seen := map[core.FederateHandle]bool{}
	for _, f := range runtime.Federates {
		if !v.member(f.Handle) || seen[f.Handle] {
			return fmt.Errorf("MOM runtime identity mismatch")
		}
		seen[f.Handle] = true
		if o.Enabled && !v.momObjectMatches(f.ObjectHandle, o.FederateClass, f.ObjectName) {
			return fmt.Errorf("MOM federate object mismatch")
		}
		if f.RuntimeMetrics != nil {
			for object, class := range f.RuntimeMetrics.ReflectedObjects {
				if err := v.validateHistoricalReflection(object, class); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateMOMTraffic(fed core.FederationName, traffic *mom.TrafficState) error {
	if traffic != nil {
		if traffic.Federation != fed {
			return fmt.Errorf("MOM traffic federation mismatch")
		}
		for _, f := range traffic.Federates {
			if !v.member(f.Handle) {
				return fmt.Errorf("MOM traffic participant mismatch")
			}
			for _, count := range f.Counts {
				var ok bool
				if count.Kind == mom.InteractionsSent || count.Kind == mom.InteractionsReceived {
					_, ok = v.names.InteractionClassName(core.InteractionClassHandle(count.Class))
				} else {
					_, ok = v.names.ObjectClassName(core.ObjectClassHandle(count.Class))
				}
				if !ok {
					return fmt.Errorf("MOM traffic unknown class")
				}
			}
			if h := f.ObjectHistory; h != nil {
				// Traffic observes registered-class fanout, not projected callback receipt.
				for _, set := range [][]mom.ObjectHistoryEntry{h.Updated, h.Reflected} {
					for _, entry := range set {
						if err := v.validateHistoricalObject(entry.Object, entry.Class); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

const checkpointRemove = "remove"

const checkpointDeleteMarker = "delete_marker"
