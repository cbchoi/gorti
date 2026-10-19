package savebundle

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

// RestoreGraph coordinates typed snapshot references without publishing any
// manager state. The caller holds exclusive checkpoint admission.
type RestoreGraphOf[T core.LogicalTimeScalar] struct {
	Snapshots map[string]core.CheckpointSnapshotter
	FOMs      core.FOMRepository
}

// CheckpointGraphValidator checks an adapter-owned candidate against the saved
// FOM, roster and canonical time family without publishing state. Present
// snapshots are validated; absent optional snapshots retain their legacy policy.
type CheckpointGraphValidator interface {
	ValidateCheckpoint(core.FederationName, core.FOMHandle, []core.FederateHandle, string, []byte) error
}

func (g RestoreGraphOf[T]) Validate(fed core.FederationName, manifest core.CheckpointManifestOf[T], model core.FOMHandle) error {
	if err := validateManifestScalar(manifest); err != nil {
		return err
	}
	model, names, err := g.restoreGraphModel(fed, model)
	if err != nil {
		return err
	}
	if manifest.Federation != fed || len(manifest.Federates) == 0 || len(manifest.Federates) != len(manifest.FederateNames) {
		return fmt.Errorf("saved graph roster is incomplete")
	}
	v := graphValidationOf[T]{model: model, names: names, members: map[core.FederateHandle]string{}, objects: map[core.ObjectHandle]object.InstanceState{}, knownClasses: map[[2]uint64]core.ObjectClassHandle{}, owners: map[[2]uint64]core.FederateHandle{}, regions: map[uint64]*rtiv1.DDMRegionEntry{}, deletions: map[core.ObjectHandle]object.DeletionStateOf[T]{}, ledger: map[core.FederateHandle][]timepkg.RetractionIDRange{}}
	seenNames, err := v.validateRoster(manifest)
	if err != nil {
		return err
	}
	for _, key := range []string{"object", "ownership", "declaration", "ddm", "time", "sync", "mom", "retractions"} {
		if _, ok := manifest.ManagerSnapshots[key]; !ok {
			return fmt.Errorf("missing graph snapshot %s", key)
		}
	}
	if _, err := g.References(manifest); err != nil {
		return err
	}
	if err := v.validateRetractions(manifest.ManagerSnapshots["retractions"]); err != nil {
		return err
	}
	if err := v.validateManagerGraphs(fed, manifest.ManagerSnapshots); err != nil {
		return err
	}
	if data, ok := manifest.ManagerSnapshots["automatic_resign"]; ok {
		if err := validateAutomaticResignGraph(data, seenNames); err != nil {
			return err
		}
	}
	return g.validateAdapterGraphs(fed, manifest, model)
}

type graphValidationOf[T core.LogicalTimeScalar] struct {
	model                core.FOMHandle
	names                core.FOMHandleNameLookup
	members              map[core.FederateHandle]string
	objects              map[core.ObjectHandle]object.InstanceState
	knownClasses         map[[2]uint64]core.ObjectClassHandle
	owners               map[[2]uint64]core.FederateHandle
	regions              map[uint64]*rtiv1.DDMRegionEntry
	deletions            map[core.ObjectHandle]object.DeletionStateOf[T]
	ledger               map[core.FederateHandle][]timepkg.RetractionIDRange
	messages             map[[2]uint64]timepkg.RetractionMessageStateOf[T]
	lastObject           uint64
	regionalObjects      map[[3]uint64]bool
	regionalInteractions map[[2]uint64]bool
}

func (v *graphValidationOf[T]) member(h core.FederateHandle) bool { return v.members[h] != "" }
func (v *graphValidationOf[T]) management(cls core.ObjectClassHandle) bool {
	n, ok := v.names.ObjectClassName(cls)
	return ok && (n == "HLAobjectRoot.HLAmanager" || strings.HasPrefix(n, "HLAobjectRoot.HLAmanager."))
}
func (v *graphValidationOf[T]) attribute(cls core.ObjectClassHandle, a core.AttributeHandle) bool {
	_, ok := v.names.AttributeName(cls, a)
	return a != 0 && ok
}
func (v *graphValidationOf[T]) objectAttribute(obj core.ObjectHandle, a core.AttributeHandle) bool {
	inst, ok := v.objects[obj]
	return ok && v.attribute(inst.Class, a)
}

func effectiveKnownClass(actual, known core.ObjectClassHandle) core.ObjectClassHandle {
	if known == 0 {
		return actual
	}
	return known
}

func (v *graphValidationOf[T]) validKnownClass(actual, known core.ObjectClassHandle) bool {
	if known == 0 || known == actual {
		return true
	}
	if actual == 0 {
		return false
	}
	if _, exists := v.names.ObjectClassName(known); !exists {
		return false
	}
	hierarchy, ok := v.model.(core.FOMClassAncestry)
	return ok && slices.Contains(hierarchy.ObjectClassAncestors(actual), known)
}

func (v *graphValidationOf[T]) accepted(h core.FederateHandle, id uint64) bool {
	if id == 0 {
		return true
	}
	for _, r := range v.ledger[h] {
		if r.First <= id && id <= r.Last {
			return true
		}
	}
	return false
}
func (v *graphValidationOf[T]) validateObjects(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	var wire objectSnapshotV1Of[T]
	if err := decodeSnapshotJSON(data, &wire); err != nil {
		return err
	}
	if err := validateObjectSnapshotVersionOf[T](wire); err != nil {
		return err
	}
	s := wire.State
	v.lastObject = s.LastObjectHandle
	if s.LastObjectHandle == math.MaxUint64 || s.LastOutboundSequence == math.MaxUint64 {
		return fmt.Errorf("exhausted saved object counters")
	}
	if err := v.validateObjectInstances(s); err != nil {
		return err
	}
	for _, d := range s.Deletions {
		if err := v.validateDeletion(d); err != nil {
			return err
		}
	}
	known := map[[2]uint64]bool{}
	for _, d := range s.Discoveries {
		if err := v.validateDiscovery(d, known); err != nil {
			return err
		}
	}
	if err := v.validateObjectKnowledge(s, known); err != nil {
		return err
	}
	return v.validateObjectDeliveryOptions(s)
}
func (v *graphValidationOf[T]) validateOwnership(data []byte) error {
	wire, _, offers, sessions, callbacks, err := decodeOwnershipSnapshotComplete(data)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if !v.member(session.Recipient) || session.FederationGeneration != uint64(session.Recipient)>>32 {
			return fmt.Errorf("ownership callback session outside saved execution")
		}
	}
	for _, callback := range callbacks {
		if err := v.validateOwnershipCallback(callback); err != nil {
			return err
		}
	}
	if err := v.validateOwners(wire); err != nil {
		return err
	}
	divests, err := v.validatePendingDivests(wire)
	if err != nil {
		return err
	}
	for _, offer := range offers {
		key := [2]uint64{uint64(offer.Object), uint64(offer.Attribute)}
		if !v.objectAttribute(offer.Object, offer.Attribute) || v.owners[key] != 0 || divests[key] ||
			(offer.Divester != 0 && !v.member(offer.Divester)) {
			return fmt.Errorf("invalid continuing ownership offer reference")
		}
	}
	return v.validatePendingAcquires(wire)
}

func (v *graphValidationOf[T]) validateRoster(manifest core.CheckpointManifestOf[T]) (map[string]bool, error) {
	seenNames := map[string]bool{}
	for i, h := range manifest.Federates {
		n := manifest.FederateNames[i]
		if h == 0 || h == ^core.FederateHandle(0) || v.members[h] != "" || n == "" || seenNames[n] {
			return nil, fmt.Errorf("invalid saved roster")
		}
		v.members[h] = n
		seenNames[n] = true
	}
	return seenNames, nil
}

func (v *graphValidationOf[T]) validateRetractions(data []byte) error {
	history, err := readRetractionsOf[T](data)
	if err != nil {
		return err
	}
	v.messages = make(map[[2]uint64]timepkg.RetractionMessageStateOf[T])
	for _, producer := range history.Producers {
		if producer.Producer == ^core.FederateHandle(0) {
			return fmt.Errorf("internal producer cannot own retraction IDs")
		}
		v.ledger[producer.Producer] = producer.Accepted
		for _, message := range producer.Messages {
			for _, recipient := range message.Delivered {
				if recipient == ^core.FederateHandle(0) || !message.Retracted && !v.member(recipient) {
					return fmt.Errorf("retraction message has invalid delivered recipient")
				}
			}
			v.messages[[2]uint64{uint64(producer.Producer), message.ID}] = message
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateManagerGraphs(fed core.FederationName, snapshots map[string][]byte) error {
	if err := v.validateObjects(snapshots["object"]); err != nil {
		return err
	}
	if err := v.validateOwnership(snapshots["ownership"]); err != nil {
		return err
	}
	if err := v.validateDDM(snapshots["ddm"]); err != nil {
		return err
	}
	if err := v.validateDeclaration(fed, snapshots["declaration"]); err != nil {
		return err
	}
	if err := v.validateTime(snapshots["time"]); err != nil {
		return err
	}
	if err := v.validateSync(snapshots["sync"]); err != nil {
		return err
	}
	if err := v.validateMOM(fed, snapshots["mom"]); err != nil {
		return err
	}
	return nil
}

func (g RestoreGraphOf[T]) validateAdapterGraphs(fed core.FederationName, manifest core.CheckpointManifestOf[T], model core.FOMHandle) error {
	keys := make([]string, 0, len(manifest.ManagerSnapshots))
	for key := range manifest.ManagerSnapshots {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		validator, ok := g.snapshotAdapter(key).(CheckpointGraphValidator)
		if !ok {
			continue
		}
		if err := validator.ValidateCheckpoint(fed, model, slices.Clone(manifest.Federates), core.ScalarRepresentation[T]().String(), bytes.Clone(manifest.ManagerSnapshots[key])); err != nil {
			return fmt.Errorf("snapshot %s: %w", key, err)
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateObjectInstances(s *object.RegistryStateOf[T]) error {
	names := map[string]bool{}
	for _, o := range s.Objects {
		if _, ok := v.names.ObjectClassName(o.Class); !ok || o.Handle == 0 || uint64(o.Handle) > s.LastObjectHandle || o.Name == "" || names[o.Name] || v.objects[o.Handle].Handle != 0 {
			return fmt.Errorf("invalid saved object %d/class %d", o.Handle, o.Class)
		}
		if o.Registrant == 0 || (o.Registrant == ^core.FederateHandle(0) && !v.management(o.Class)) {
			return fmt.Errorf("invalid object registrant")
		}
		names[o.Name] = true
		v.objects[o.Handle] = o
	}
	return nil
}

func (v *graphValidationOf[T]) validateDeletion(d object.DeletionStateOf[T]) error {
	_, live := v.objects[d.Object]
	if d.Object == 0 || uint64(d.Object) > v.lastObject || d.Producer == 0 || d.Producer == ^core.FederateHandle(0) || d.Committed == live || !validSnapshotScalar(d.Timestamp) || !v.accepted(d.Producer, d.RetractionHandle) {
		return fmt.Errorf("invalid saved deletion")
	}
	if _, dup := v.deletions[d.Object]; dup {
		return fmt.Errorf("duplicate deletion")
	}
	if err := v.validateDeletionIdentity(d); err != nil {
		return err
	}
	actualClass := d.ObjectClass
	if actualClass == 0 && live {
		actualClass = v.objects[d.Object].Class
	}
	if err := v.validateDeletionRecipients(d, actualClass); err != nil {
		return err
	}
	if err := v.validateDeletionOwners(d, live); err != nil {
		return err
	}
	v.deletions[d.Object] = d
	return nil
}

func (v *graphValidationOf[T]) validateDeletionIdentity(d object.DeletionStateOf[T]) error {
	if !d.OwnershipSnapshot && len(d.AttributeOwners) != 0 {
		return fmt.Errorf("deletion owners without ownership snapshot")
	}
	if (d.ObjectName == "") != (d.ObjectClass == 0) {
		return fmt.Errorf("incomplete deletion object identity")
	}
	if d.ObjectClass != 0 {
		if _, ok := v.names.ObjectClassName(d.ObjectClass); !ok {
			return fmt.Errorf("unknown deletion object class")
		}
		if instance, ok := v.objects[d.Object]; ok && (instance.Class != d.ObjectClass || instance.Name != d.ObjectName) {
			return fmt.Errorf("deletion object identity mismatch")
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateDeletionRecipients(d object.DeletionStateOf[T], actualClass core.ObjectClassHandle) error {
	var previousRecipient core.FederateHandle
	for _, recipient := range d.Recipients {
		if recipient.Recipient == 0 || recipient.Recipient == ^core.FederateHandle(0) || recipient.Recipient <= previousRecipient || !recipient.ReceivedTSO && !recipient.Forgotten || !recipient.Forgotten && !v.member(recipient.Recipient) {
			return fmt.Errorf("invalid saved deletion recipient")
		}
		if !v.validKnownClass(actualClass, recipient.KnownClass) {
			return fmt.Errorf("invalid deletion recipient known class %d for object class %d", recipient.KnownClass, actualClass)
		}
		v.knownClasses[[2]uint64{uint64(d.Object), uint64(recipient.Recipient)}] = effectiveKnownClass(actualClass, recipient.KnownClass)
		previousRecipient = recipient.Recipient
	}
	return nil
}

func (v *graphValidationOf[T]) validateDeletionOwners(d object.DeletionStateOf[T], live bool) error {
	var previous core.AttributeHandle
	for _, owner := range d.AttributeOwners {
		if owner.Attribute == 0 || owner.Attribute <= previous || live && !v.objectAttribute(d.Object, owner.Attribute) {
			return fmt.Errorf("invalid saved deletion attribute owner")
		}
		if live && owner.Owner != 0 && !v.member(owner.Owner) && !(owner.Owner == ^core.FederateHandle(0) && v.management(v.objects[d.Object].Class)) {
			return fmt.Errorf("saved deletion attribute owner is not a participant")
		}
		previous = owner.Attribute
	}
	return nil
}

func (v *graphValidationOf[T]) validateDiscovery(d object.DiscoveryState, known map[[2]uint64]bool) error {
	key := [2]uint64{uint64(d.Object), uint64(d.Federate)}
	if !v.member(d.Federate) || known[key] {
		return fmt.Errorf("invalid discovery reference")
	}
	actualClass := v.objects[d.Object].Class
	if v.objects[d.Object].Handle == 0 {
		deletion, ok := v.deletions[d.Object]
		if !ok || !deletion.Committed || deletion.ObjectClass == 0 || deletion.ObjectName == "" {
			return fmt.Errorf("discovery without live object or known tombstone")
		}
		actualClass = deletion.ObjectClass
	}
	if !v.validKnownClass(actualClass, d.KnownClass) {
		return fmt.Errorf("invalid discovery known class %d for object class %d", d.KnownClass, actualClass)
	}
	if err := v.validateDiscoveryDeletion(d, actualClass); err != nil {
		return err
	}
	known[key] = true
	v.knownClasses[key] = effectiveKnownClass(actualClass, d.KnownClass)
	return nil
}

func (v *graphValidationOf[T]) validateDiscoveryDeletion(d object.DiscoveryState, actualClass core.ObjectClassHandle) error {
	for _, recipient := range v.deletions[d.Object].Recipients {
		if recipient.Recipient != d.Federate {
			continue
		}
		if recipient.Forgotten {
			return fmt.Errorf("forgotten deletion recipient retains discovery")
		}
		if effectiveKnownClass(actualClass, recipient.KnownClass) != effectiveKnownClass(actualClass, d.KnownClass) {
			return fmt.Errorf("discovery and deletion recipient known classes differ")
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateObjectKnowledge(s *object.RegistryStateOf[T], known map[[2]uint64]bool) error {
	for _, scope := range s.Scopes {
		if !known[[2]uint64{uint64(scope.Object), uint64(scope.Federate)}] {
			return fmt.Errorf("scope without discovery")
		}
		for _, a := range scope.Attributes {
			if !v.objectAttribute(scope.Object, a) {
				return fmt.Errorf("scope attribute absent from object FOM")
			}
		}
	}
	for _, r := range s.Reservations {
		if !v.member(r.Holder) || r.Name == "" {
			return fmt.Errorf("invalid name reservation")
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateObjectDeliveryOptions(s *object.RegistryStateOf[T]) error {
	for _, a := range s.AttributeOrders {
		if !v.objectAttribute(a.Object, a.Attribute) {
			return fmt.Errorf("invalid attribute order reference")
		}
	}
	for _, a := range s.AttributeTransports {
		if !v.objectAttribute(a.Object, a.Attribute) {
			return fmt.Errorf("invalid attribute transport reference")
		}
	}
	for _, i := range s.InteractionOrders {
		if _, ok := v.names.InteractionClassName(i.Class); !ok || !v.member(i.Publisher) {
			return fmt.Errorf("invalid interaction order reference")
		}
	}
	for _, i := range s.InteractionTransports {
		if _, ok := v.names.InteractionClassName(i.Class); !ok || !v.member(i.Publisher) {
			return fmt.Errorf("invalid interaction transport reference")
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateOwnershipCallback(callback ownership.TerminalCallbackState) error {
	if !v.member(callback.Recipient) || uint64(callback.Object) > v.lastObject {
		return fmt.Errorf("terminal ownership callback references unknown recipient/object")
	}
	class := v.objects[callback.Object].Class
	if class == 0 {
		class = v.deletions[callback.Object].ObjectClass
	}
	if p := callback.RetainedProjection; p != nil {
		if class != 0 && class != p.RegisteredClass {
			return fmt.Errorf("retained ownership callback registered class differs")
		}
		class = p.RegisteredClass
		if !v.validKnownClass(p.RegisteredClass, p.KnownClass) {
			return fmt.Errorf("retained ownership callback class view is invalid")
		}
		for i, attr := range callback.Attributes {
			actualName, actualOK := v.names.AttributeName(class, attr)
			knownName, knownOK := v.names.AttributeName(p.KnownClass, p.Attributes[i])
			if !actualOK || !knownOK || actualName != knownName {
				return fmt.Errorf("retained ownership callback attribute mapping is invalid")
			}
		}
	}
	for _, attr := range callback.Attributes {
		if class != 0 && !v.attribute(class, attr) {
			return fmt.Errorf("terminal ownership callback references unknown attribute")
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateOwners(wire *rtiv1.OwnershipManagerState) error {
	for _, o := range wire.Owners {
		if o == nil {
			return fmt.Errorf("nil owner")
		}
		obj, attr, h := core.ObjectHandle(o.ObjectHandle), core.AttributeHandle(o.AttributeHandle), core.FederateHandle(o.OwnerFederate)
		key := [2]uint64{o.ObjectHandle, o.AttributeHandle}
		if !v.objectAttribute(obj, attr) {
			return fmt.Errorf("owner references unknown object attribute")
		}
		if h != 0 && !v.member(h) && !(h == ^core.FederateHandle(0) && v.management(v.objects[obj].Class)) {
			return fmt.Errorf("owner is not a saved participant")
		}
		if _, dup := v.owners[key]; dup {
			return fmt.Errorf("duplicate owner")
		}
		v.owners[key] = h
	}
	return nil
}

func (v *graphValidationOf[T]) validatePendingDivests(wire *rtiv1.OwnershipManagerState) (map[[2]uint64]bool, error) {
	divests := map[[2]uint64]bool{}
	for _, d := range wire.PendingDivests {
		if d == nil {
			return nil, fmt.Errorf("nil divest")
		}
		key := [2]uint64{d.ObjectHandle, d.AttributeHandle}
		if !v.objectAttribute(core.ObjectHandle(d.ObjectHandle), core.AttributeHandle(d.AttributeHandle)) || !v.member(core.FederateHandle(d.OwnerFederate)) || v.owners[key] != core.FederateHandle(d.OwnerFederate) || divests[key] {
			return nil, fmt.Errorf("invalid pending divest reference")
		}
		divests[key] = true
	}
	return divests, nil
}

func (v *graphValidationOf[T]) validatePendingAcquires(wire *rtiv1.OwnershipManagerState) error {
	acquires := map[[3]uint64]bool{}
	for _, a := range wire.PendingAcquires {
		if a == nil {
			return fmt.Errorf("nil acquire")
		}
		key := [2]uint64{a.ObjectHandle, a.AttributeHandle}
		entry := [3]uint64{a.ObjectHandle, a.AttributeHandle, a.AcquirerFederate}
		if !v.objectAttribute(core.ObjectHandle(a.ObjectHandle), core.AttributeHandle(a.AttributeHandle)) || !v.member(core.FederateHandle(a.AcquirerFederate)) || v.owners[key] == core.FederateHandle(a.AcquirerFederate) || acquires[entry] {
			return fmt.Errorf("invalid pending acquisition reference")
		}
		acquires[entry] = true
	}
	return nil
}

func (g RestoreGraphOf[T]) restoreGraphModel(fed core.FederationName, model core.FOMHandle) (core.FOMHandle, core.FOMHandleNameLookup, error) {
	if model == nil && g.FOMs != nil {
		var err error
		model, err = g.FOMs.Get(context.Background(), fed)
		if err != nil {
			return nil, nil, err
		}
	}
	names, ok := model.(core.FOMHandleNameLookup)
	if !ok || model == nil || !model.IsValid() {
		return nil, nil, fmt.Errorf("saved graph requires a valid reverse-lookup FOM")
	}
	return model, names, nil
}
