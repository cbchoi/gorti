package federate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"

	codec "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const sdkRuntimeLimit = 1 << 20

type sdkRuntimeObject struct {
	Object uint64   `json:"object_handle"`
	Class  uint64   `json:"class_handle"`
	Owned  []uint64 `json:"owned_attributes,omitempty"`
}

type sdkRuntimeDeletion struct {
	sdkRuntimeObject
	Sender     uint64 `json:"sender_federate"`
	Retraction uint64 `json:"retraction_handle"`
	Originated bool   `json:"originated"`
}

type sdkRuntimeRemoval struct {
	Object uint64          `json:"object_handle"`
	Time   json.RawMessage `json:"logical_time_value"`
}

type sdkRuntimePendingData struct {
	InvocationState string `json:"invocation_state"`
	Identity        string `json:"callback_identity"`
}

type sdkRuntimeState struct {
	Generation      uint64                  `json:"-"`
	Self            uint64                  `json:"-"`
	Epoch           uint64                  `json:"callback_epoch"`
	Recovery        string                  `json:"callback_recovery"`
	Scalar          string                  `json:"-"`
	Objects         []sdkRuntimeObject      `json:"known_objects"`
	Deleted         []sdkRuntimeDeletion    `json:"deleted_objects,omitempty"`
	Removals        []sdkRuntimeRemoval     `json:"pending_removals,omitempty"`
	Final           []uint64                `json:"final_removals,omitempty"`
	Advances        []bool                  `json:"time_advances,omitempty"`
	Callbacks       []json.RawMessage       `json:"pending_callbacks,omitempty"`
	PendingData     []sdkRuntimePendingData `json:"pending_data,omitempty"`
	PendingControls []sdkRuntimePendingData `json:"pending_controls,omitempty"`
	PendingOutcomes []sdkRuntimeOutcome     `json:"pending_outcomes"`
}

type sdkRuntimeEnvelope struct {
	Magic      string          `json:"magic"`
	Version    uint32          `json:"version"`
	Language   string          `json:"language"`
	Scalar     string          `json:"scalar"`
	Generation uint64          `json:"generation"`
	Self       uint64          `json:"self"`
	State      json.RawMessage `json:"state"`
	Checksum   string          `json:"checksum"`
}

func runtimeChecksum(envelope sdkRuntimeEnvelope) (string, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, envelope.State); err != nil {
		return "", err
	}
	header := envelope.Magic + "\n" + strconv.FormatUint(uint64(envelope.Version), 10) + "\n" + envelope.Language + "\n" + envelope.Scalar + "\n" + strconv.FormatUint(envelope.Generation, 10) + "\n" + strconv.FormatUint(envelope.Self, 10) + "\n"
	digest := sha256.New()
	_, _ = digest.Write([]byte(header))
	_, _ = digest.Write(compact.Bytes())
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func encodeRuntimeState(s sdkRuntimeState) ([]byte, error) {
	state, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	envelope := sdkRuntimeEnvelope{Magic: "GRS1", Version: 1, Language: "go", Scalar: s.Scalar, Generation: s.Generation, Self: s.Self, State: state}
	envelope.Checksum, err = runtimeChecksum(envelope)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(envelope)
	if len(encoded) > sdkRuntimeLimit {
		return nil, errors.New("federate: SDK runtime checkpoint exceeds 1 MiB")
	}
	return encoded, err
}

type sdkQueuedCallback struct {
	id       uint64
	wire     *rtiv1.FederateEvent
	identity string
}

type sdkPreparedRuntime struct {
	state    sdkRuntimeState
	removals map[uint64]logicaltime.Time
	wires    []*rtiv1.FederateEvent
}

func sortedAttributes(attrs map[uint64]struct{}) []uint64 {
	values := make([]uint64, 0, len(attrs))
	for attr := range attrs {
		values = append(values, attr)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values
}

// The channel is not an invocation ledger. Retain detached wire payloads until
// explicit callback entry, even when a caller has already read Events.
func (f *Federate) trackRuntimeCallback(wire *rtiv1.FederateEvent, info CallbackInfo) CallbackInfo {
	info.runtimeOwner, info.runtimeEpoch = f, f.runtimeEpoch
	info.localEntry = localEntryWireEligible(wire)
	if !runtimeCallback(wire) {
		return info
	}
	for _, pending := range f.runtimePending {
		if info.identity != "" && info.identity == pending.identity {
			info.runtimeID = pending.id
			return info
		}
	}
	f.runtimeNext++
	info.runtimeID = f.runtimeNext
	// Recovery identity is immutable for this detached wire image. Retain it
	// once instead of hashing every queued receipt on each incoming callback.
	f.runtimePending = append(f.runtimePending, sdkQueuedCallback{id: info.runtimeID, wire: proto.Clone(wire).(*rtiv1.FederateEvent), identity: info.identity})
	return info
}

func runtimeCallback(wire *rtiv1.FederateEvent) bool {
	if wire == nil || wire.Event == nil {
		return false
	}
	if len(wire.CallbackReceipt) != 0 {
		if wire.GetCallbackRecovery() != nil {
			return true
		}
		switch wire.Event.(type) {
		case *rtiv1.FederateEvent_SaveInitiate, *rtiv1.FederateEvent_RestoreInitiate, *rtiv1.FederateEvent_RestoreBegun:
			return false
		}
		return true
	}
	switch wire.Event.(type) {
	case *rtiv1.FederateEvent_SaveInitiate, *rtiv1.FederateEvent_SaveCompleted, *rtiv1.FederateEvent_SaveFailed,
		*rtiv1.FederateEvent_RestoreInitiate, *rtiv1.FederateEvent_RestoreCompleted, *rtiv1.FederateEvent_RestoreFailed,
		*rtiv1.FederateEvent_RestoreRequestSucceeded, *rtiv1.FederateEvent_RestoreRequestFailed,
		*rtiv1.FederateEvent_RestoreBegun, *rtiv1.FederateEvent_LocalValidationBarrier:
		return false
	default:
		return true
	}
}

func runtimeDataCallback(wire *rtiv1.FederateEvent) bool {
	switch wire.Event.(type) {
	case *rtiv1.FederateEvent_Discover, *rtiv1.FederateEvent_Reflect, *rtiv1.FederateEvent_Receive, *rtiv1.FederateEvent_Remove:
		return true
	default:
		return false
	}
}

func runtimeCallbackCapability(wire *rtiv1.FederateEvent) bool {
	if len(wire.CallbackReceipt) != 0 || wire.OwnershipReceipt != nil || wire.GetTransportationChangeConfirmed() != nil {
		return true
	}
	var metadata *rtiv1.ObjectCallbackMetadata
	switch event := wire.Event.(type) {
	case *rtiv1.FederateEvent_Reflect:
		metadata = event.Reflect.GetMetadata()
	case *rtiv1.FederateEvent_Receive:
		metadata = event.Receive.GetMetadata()
	case *rtiv1.FederateEvent_Remove:
		metadata = event.Remove.GetMetadata()
	}
	return metadata != nil && metadata.GetReceivedRegions() != nil && len(metadata.GetReceivedRegions().GetRegionHandles()) != 0
}

func runtimeInfo(event Event) (CallbackInfo, bool) {
	carrier, ok := event.(interface{ runtimeCallbackInfo() CallbackInfo })
	if !ok {
		return CallbackInfo{}, false
	}
	info := carrier.runtimeCallbackInfo()
	return info, info.runtimeOwner != nil
}

func (f *Federate) validateRuntimeEventLocked(event Event) error {
	if f.runtimeRestoring {
		return errors.New("federate: callback admission suspended during restore")
	}
	if info, ok := runtimeInfo(event); ok && (info.runtimeOwner != f || info.runtimeEpoch != f.runtimeEpoch) {
		return errors.New("federate: stale SDK callback from an earlier restore")
	}
	return nil
}

func (f *Federate) retireRuntimeEventLocked(event Event) {
	info, ok := runtimeInfo(event)
	if !ok || info.runtimeOwner != f || info.runtimeEpoch != f.runtimeEpoch {
		return
	}
	f.removeRuntimePending(info.runtimeID)
}

func (f *Federate) runtimeEventConsumedLocked(event Event) bool {
	info, ok := runtimeInfo(event)
	if !ok || info.runtimeID == 0 {
		return false
	}
	if f.runtimeAdmission[info.runtimeID] {
		return true
	}
	for _, pending := range f.runtimePending {
		if pending.id == info.runtimeID {
			return false
		}
	}
	return true
}

const (
	runtimeRecoveryServerV2    = "server-v2"
	runtimeInvocationUnentered = "unentered"
)

func (f *Federate) snapshotRuntime() ([]byte, error) {
	f.checkpointMu.Lock()
	defer f.checkpointMu.Unlock()
	if f.runtimeRestoring {
		return nil, errors.New("federate: checkpoint suspended during restore")
	}
	if f.runtimeActive != 0 {
		return nil, errors.New("federate: cannot checkpoint an invoking callback")
	}
	outcomes, finished, err := f.snapshotFinishedCallbacks()
	if err != nil {
		return nil, err
	}
	f.ownershipCallbackMu.Lock()
	unresolved := len(f.ownershipPending) != 0
	f.ownershipCallbackMu.Unlock()
	if unresolved {
		return nil, errors.New("federate: unresolved ownership outcome prevents checkpoint")
	}
	s := sdkRuntimeState{Generation: f.federationGeneration, Self: f.CurrentHandle(), Epoch: f.CallbackEpoch(), Recovery: runtimeRecoveryServerV2, Scalar: f.timeRepresentation().String(), Objects: []sdkRuntimeObject{}, PendingOutcomes: outcomes}
	sort.Slice(s.PendingOutcomes, func(i, j int) bool { return s.PendingOutcomes[i].Identity < s.PendingOutcomes[j].Identity })
	if err := f.snapshotObjectKnowledge(&s); err != nil {
		return nil, err
	}
	sort.Slice(s.Objects, func(i, j int) bool { return s.Objects[i].Object < s.Objects[j].Object })
	sort.Slice(s.Deleted, func(i, j int) bool {
		a, b := s.Deleted[i], s.Deleted[j]
		if a.Sender == b.Sender {
			return a.Retraction < b.Retraction
		}
		return a.Sender < b.Sender
	})
	sort.Slice(s.Removals, func(i, j int) bool { return s.Removals[i].Object < s.Removals[j].Object })
	sort.Slice(s.Final, func(i, j int) bool { return s.Final[i] < s.Final[j] })
	if err := f.snapshotPendingCallbacks(&s, finished); err != nil {
		return nil, err
	}
	return encodeRuntimeState(s)
}

func (f *Federate) decodeRuntime(encoded []byte) (*sdkPreparedRuntime, error) {
	if len(encoded) == 0 {
		return nil, nil
	}
	envelope, err := f.decodeRuntimeEnvelope(encoded)
	if err != nil {
		return nil, err
	}
	prepared := &sdkPreparedRuntime{removals: make(map[uint64]logicaltime.Time)}
	d := json.NewDecoder(bytes.NewReader(envelope.State))
	d.DisallowUnknownFields()
	if err := d.Decode(&prepared.state); err != nil {
		return nil, err
	}
	s := &prepared.state
	s.Generation, s.Self, s.Scalar = envelope.Generation, envelope.Self, envelope.Scalar
	if err := validateRuntimeRecovery(s); err != nil {
		return nil, err
	}
	objects, err := validateRuntimeObjects(s)
	if err != nil {
		return nil, err
	}
	if err := f.decodeRuntimeRemovals(prepared, objects); err != nil {
		return nil, err
	}
	if err := validateRuntimeOutcomes(s); err != nil {
		return nil, err
	}
	if err := f.decodeRuntimeCallbacks(prepared); err != nil {
		return nil, err
	}
	return prepared, nil
}

// Duplicate members have different interpretations across language decoders.
// Reject them before provenance validation or SDK state hydration.
func validateRuntimeJSON(encoded []byte) error {
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 32 {
			return errors.New("federate: runtime JSON nesting limit exceeded")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]bool)
			for d.More() {
				token, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok || seen[key] {
					return errors.New("federate: duplicate runtime JSON member")
				}
				seen[key] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("federate: invalid runtime JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("federate: trailing runtime JSON data")
	}
	return nil
}

func attributesSet(values []uint64) map[uint64]struct{} {
	attrs := make(map[uint64]struct{}, len(values))
	for _, attr := range values {
		attrs[attr] = struct{}{}
	}
	return attrs
}

func (f *Federate) installRuntimeKnowledge(prepared *sdkPreparedRuntime) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := prepared.state
	f.objectClasses = make(map[uint64]uint64, len(s.Objects))
	f.ownedObjectAttributes = make(map[uint64]map[uint64]struct{})
	for _, object := range s.Objects {
		f.objectClasses[object.Object] = object.Class
		f.ownedObjectAttributes[object.Object] = attributesSet(object.Owned)
	}
	f.deletedObjects = make(map[deletionDesignator]deletedObjectKnowledge, len(s.Deleted))
	for _, object := range s.Deleted {
		f.deletedObjects[deletionDesignator{object.Sender, object.Retraction}] = deletedObjectKnowledge{object: object.Object, class: object.Class, owned: attributesSet(object.Owned), originated: object.Originated}
	}
	f.pendingObjectRemovals = prepared.removals
	f.pendingFinalObjectRemovals = make(map[uint64]struct{})
	for _, object := range s.Final {
		f.pendingFinalObjectRemovals[object] = struct{}{}
	}
	f.timeAdvances = nil
	for _, flush := range s.Advances {
		f.timeAdvances = append(f.timeAdvances, &sdkAdvanceRequest{flush: flush})
	}
}

func (f *Federate) stageRuntimeRestore(event InitiateFederateRestore) error {
	f.checkpointMu.Lock()
	defer f.checkpointMu.Unlock()
	f.runtimeRestoreReady = false
	f.runtimeRestoreError = nil
	f.runtimeStaged = nil
	prepared, err := f.decodeRuntime(event.ClientState)
	if err != nil {
		f.runtimeRestoreError = fmt.Errorf("SDK runtime restore: %w", err)
		return f.runtimeRestoreError
	}
	f.runtimeStaged = prepared
	// Empty legacy state is validated too, despite having no prepared runtime.
	f.runtimeRestoreReady = true
	return nil
}

func (f *Federate) resetRuntimeRestoreReadiness(clearFailure bool) {
	f.checkpointMu.Lock()
	defer f.checkpointMu.Unlock()
	f.runtimeRestoreReady = false
	f.runtimeStaged = nil
	if clearFailure {
		f.runtimeRestoreError = nil
	}
}

func (f *Federate) publishRuntimeEvent(ctx context.Context, event Event) bool {
	var recovered []Event
	info, _ := runtimeInfo(event)
	if _, terminal := event.(FederationRestored); terminal && !info.recovered {
		// Remove only old-epoch buffered values. Values already held by callers
		// are fenced by their SDK token, not assumed invoked by a channel read.
		for {
			select {
			case <-f.eventCh:
				continue
			default:
			}
			break
		}
		f.checkpointMu.Lock()
		recovered, f.runtimeRecovered = f.runtimeRecovered, nil
		f.checkpointMu.Unlock()
	}
	for _, next := range append([]Event{event}, recovered...) {
		select {
		case f.eventCh <- next:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

func (f *Federate) decodeRuntimeEnvelope(encoded []byte) (*sdkRuntimeEnvelope, error) {
	if len(encoded) > sdkRuntimeLimit {
		return nil, errors.New("federate: SDK runtime checkpoint exceeds 1 MiB")
	}
	if err := validateRuntimeJSON(encoded); err != nil {
		return nil, err
	}
	var envelope sdkRuntimeEnvelope
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.DisallowUnknownFields()
	if err := d.Decode(&envelope); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("federate: trailing runtime checkpoint data")
	}
	checksum, err := runtimeChecksum(envelope)
	if err != nil {
		return nil, err
	}
	if envelope.Magic != "GRS1" || envelope.Version != 1 || envelope.Language != "go" || envelope.Scalar != f.timeRepresentation().String() || envelope.Generation != f.federationGeneration || envelope.Self == 0 || envelope.Checksum != checksum {
		return nil, errors.New("federate: incompatible SDK runtime checkpoint")
	}
	return &envelope, nil
}

func validateRuntimeRecovery(s *sdkRuntimeState) error {
	if s.Recovery != runtimeRecoveryLegacy && s.Recovery != runtimeRecoveryServerV2 {
		return errors.New("federate: unsupported callback recovery authority")
	}
	if s.Recovery == runtimeRecoveryServerV2 && s.PendingOutcomes == nil {
		return errors.New("federate: server-v2 checkpoint requires pending_outcomes")
	}
	if s.Recovery == runtimeRecoveryServerV2 && len(s.Callbacks) != 0 {
		return errors.New("federate: server-v2 checkpoint cannot replay SDK-owned callback bodies")
	}
	if s.Recovery == runtimeRecoveryLegacy && (len(s.PendingOutcomes) != 0 || len(s.PendingControls) != 0) {
		return errors.New("federate: legacy checkpoint cannot carry v2 callback recovery")
	}
	return nil
}

func validateRuntimeObject(object sdkRuntimeObject) error {
	if object.Object == 0 || object.Class == 0 {
		return errors.New("federate: zero runtime object/class")
	}
	attrs := make(map[uint64]bool)
	for _, a := range object.Owned {
		if a == 0 || attrs[a] {
			return errors.New("federate: invalid runtime ownership")
		}
		attrs[a] = true
	}
	return nil
}

func validateRuntimeObjects(s *sdkRuntimeState) (map[uint64]bool, error) {
	objects := make(map[uint64]bool)
	for _, object := range s.Objects {
		if err := validateRuntimeObject(object); err != nil {
			return nil, err
		}
		if objects[object.Object] {
			return nil, errors.New("federate: duplicate runtime object")
		}
		objects[object.Object] = true
	}
	deleted := make(map[deletionDesignator]bool)
	for _, object := range s.Deleted {
		if err := validateRuntimeObject(object.sdkRuntimeObject); err != nil {
			return nil, err
		}
		key := deletionDesignator{object.Sender, object.Retraction}
		if key.sender == 0 || key.id == 0 || deleted[key] {
			return nil, errors.New("federate: invalid runtime retraction")
		}
		deleted[key] = true
	}
	return objects, nil
}

func (f *Federate) decodeRuntimeRemovals(prepared *sdkPreparedRuntime, objects map[uint64]bool) error {
	s := &prepared.state
	for _, removal := range s.Removals {
		if !objects[removal.Object] {
			return errors.New("federate: runtime removal of unknown object")
		}
		if _, exists := prepared.removals[removal.Object]; exists {
			return errors.New("federate: duplicate runtime removal")
		}
		wire := new(rtiv1.LogicalTimeValue)
		if err := protojson.Unmarshal(removal.Time, wire); err != nil {
			return err
		}
		at, err := codec.DecodeLogicalTime(f.timeRepresentation(), wire)
		if err != nil {
			return err
		}
		prepared.removals[removal.Object] = at
	}
	final := make(map[uint64]bool)
	for _, object := range s.Final {
		_, timed := prepared.removals[object]
		if !objects[object] || final[object] || timed {
			return errors.New("federate: invalid final runtime removal")
		}
		final[object] = true
	}
	return nil
}

func validateRuntimeOutcomes(s *sdkRuntimeState) error {
	receipts := make(map[string]bool)
	for _, pending := range append(append([]sdkRuntimePendingData(nil), s.PendingData...), s.PendingControls...) {
		if !validCallbackIdentity(pending.Identity) || pending.InvocationState != runtimeInvocationUnentered || receipts[pending.Identity] {
			return errors.New("federate: invalid pending data invocation state")
		}
		receipts[pending.Identity] = true
	}
	for _, outcome := range s.PendingOutcomes {
		if !validCallbackIdentity(outcome.Identity) || receipts[outcome.Identity] || (outcome.Success && outcome.Exception != "") || (!outcome.Success && outcome.Exception == "") {
			return errors.New("federate: invalid finished callback outcome")
		}
		receipts[outcome.Identity] = true
	}
	return nil
}

func (f *Federate) decodeRuntimeCallbacks(prepared *sdkPreparedRuntime) error {
	s := &prepared.state
	for _, encoded := range s.Callbacks {
		wire := new(rtiv1.FederateEvent)
		if err := protojson.Unmarshal(encoded, wire); err != nil {
			return err
		}
		if !runtimeCallback(wire) {
			return errors.New("federate: invalid pending runtime callback")
		}
		if runtimeCallbackCapability(wire) {
			return errors.New("federate: pending callback capability is not portable")
		}
		if _, err := f.wireEventTime(wire); err != nil {
			return err
		}
		probe := &Federate{federateHandle: s.Self, federationGeneration: s.Generation, logicalTimeImplementationName: s.Scalar, handles: f.handles}
		translated := probe.translate(wire)
		if translated == nil {
			return errors.New("federate: unsupported pending runtime callback")
		}
		if failure, ok := translated.(CallbackProtocolError); ok {
			return failure
		}
		prepared.wires = append(prepared.wires, wire)
	}
	return nil
}

func (f *Federate) snapshotFinishedCallbacks() ([]sdkRuntimeOutcome, map[string]bool, error) {
	f.genericCallbackMu.Lock()
	defer f.genericCallbackMu.Unlock()
	outcomes := make([]sdkRuntimeOutcome, 0)
	finished := make(map[string]bool)
	for _, entry := range f.genericCallbacks {
		if !entry.complete {
			return nil, nil, errors.New("federate: unresolved callback outcome prevents checkpoint")
		}
	}
	for identity, entry := range f.genericCallbacks {
		if !validCallbackIdentity(identity) {
			return nil, nil, errors.New("federate: invalid finished callback identity")
		}
		finished[identity] = true
		outcomes = append(outcomes, sdkRuntimeOutcome{Identity: identity, Success: entry.success, Exception: entry.exception})
	}
	return outcomes, finished, nil
}

func (f *Federate) snapshotObjectKnowledge(s *sdkRuntimeState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for object, class := range f.objectClasses {
		s.Objects = append(s.Objects, sdkRuntimeObject{Object: object, Class: class, Owned: sortedAttributes(f.ownedObjectAttributes[object])})
	}
	for key, deleted := range f.deletedObjects {
		s.Deleted = append(s.Deleted, sdkRuntimeDeletion{sdkRuntimeObject: sdkRuntimeObject{Object: deleted.object, Class: deleted.class, Owned: sortedAttributes(deleted.owned)}, Sender: key.sender, Retraction: key.id, Originated: deleted.originated})
	}
	for object, at := range f.pendingObjectRemovals {
		wire, err := codec.EncodeLogicalTime(f.timeRepresentation(), at)
		if err != nil {
			return err
		}
		encoded, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(wire)
		if err != nil {
			return err
		}
		s.Removals = append(s.Removals, sdkRuntimeRemoval{Object: object, Time: encoded})
	}
	for object := range f.pendingFinalObjectRemovals {
		s.Final = append(s.Final, object)
	}
	for _, advance := range f.timeAdvances {
		s.Advances = append(s.Advances, advance.flush)
	}
	return nil
}

func (f *Federate) snapshotPendingCallbacks(s *sdkRuntimeState, finished map[string]bool) error {
	for _, pending := range f.runtimePending {
		if len(pending.wire.CallbackReceipt) != 0 {
			info, err := callbackRecoveryInfo(pending.wire)
			if err != nil {
				return err
			}
			if finished[info.identity] {
				continue
			}
			if info.outcomeOnly {
				return errors.New("federate: recovery carrier has no finished outcome")
			}
			metadata := sdkRuntimePendingData{InvocationState: runtimeInvocationUnentered, Identity: info.identity}
			if runtimeDataCallback(pending.wire) {
				s.PendingData = append(s.PendingData, metadata)
			} else {
				s.PendingControls = append(s.PendingControls, metadata)
			}
			continue
		}
		if runtimeCallbackCapability(pending.wire) {
			return errors.New("federate: pending control callback capability is not portable")
		}
		return errors.New("federate: server-v2 checkpoint cannot reconcile a receiptless pending callback")
	}
	return nil
}

const runtimeRecoveryLegacy = "server"
