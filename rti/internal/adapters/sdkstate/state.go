package sdkstate

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"

	codec "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type runtimeObject struct {
	Object uint64   `json:"object_handle"`
	Class  uint64   `json:"class_handle"`
	Owned  []uint64 `json:"owned_attributes,omitempty"`
}
type runtimeDeletion struct {
	runtimeObject
	Sender     uint64 `json:"sender_federate"`
	Retraction uint64 `json:"retraction_handle"`
	Originated bool   `json:"originated"`
}
type runtimeRemoval struct {
	Object uint64          `json:"object_handle"`
	Time   json.RawMessage `json:"logical_time_value"`
}
type pendingData struct {
	InvocationState string `json:"invocation_state"`
	Identity        string `json:"callback_identity"`
}
type goState struct {
	Epoch           uint64              `json:"callback_epoch"`
	Objects         []runtimeObject     `json:"known_objects"`
	Deleted         []runtimeDeletion   `json:"deleted_objects,omitempty"`
	Removals        []runtimeRemoval    `json:"pending_removals,omitempty"`
	Final           []uint64            `json:"final_removals,omitempty"`
	Advances        []bool              `json:"time_advances,omitempty"`
	Callbacks       []json.RawMessage   `json:"pending_callbacks,omitempty"`
	PendingData     []pendingData       `json:"pending_data,omitempty"`
	PendingControls []pendingData       `json:"pending_controls,omitempty"`
	Outcomes        []pendingOutcome    `json:"pending_outcomes,omitempty"`
	Locals          []localNotification `json:"pending_local_notifications,omitempty"`
	Recovery        string              `json:"callback_recovery"`
}
type objectKnowledge struct {
	Latest  [][]uint64 `json:"latest"`
	Retired [][]uint64 `json:"retired"`
}
type pythonState struct {
	Epoch     uint64              `json:"callback_epoch"`
	Time      json.RawMessage     `json:"logical_time"`
	Knowledge objectKnowledge     `json:"object_knowledge"`
	Next      *uint64             `json:"next_retraction_handle"`
	Used      []uint64            `json:"used_retraction_handles"`
	Callbacks []json.RawMessage   `json:"pending_callbacks"`
	Recovery  string              `json:"callback_recovery"`
	Outcomes  []pendingOutcome    `json:"pending_outcomes,omitempty"`
	Locals    []localNotification `json:"pending_local_notifications,omitempty"`
}

func validateState(e Envelope) error {
	switch e.Language {
	case "go":
		return validateGoState(e)
	case checkpointPython:
		return validatePythonState(e)
	case checkpointJava:
		return validateJavaState(e)
	}
	return errors.New("unknown SDK language")
}

func requireFields(raw json.RawMessage, keys ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("SDK state must be an object")
	}
	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("missing SDK state field %s", key)
		}
	}
	return nil
}
func uniqueNonzero(values []uint64) error {
	seen := map[uint64]bool{}
	for _, v := range values {
		if v == 0 || seen[v] {
			return errors.New("zero or duplicate SDK handle")
		}
		seen[v] = true
	}
	return nil
}
func validFederate(h, g uint64) bool { return h != 0 && h&math.MaxUint32 != 0 && h>>32 == g }
func validateTime(raw json.RawMessage, scalar string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if len(fields) != 1 {
		return errors.New("SDK time must have exactly one typed value")
	}
	if scalar == checkpointHLAinteger64Time {
		var value string
		if err := json.Unmarshal(fields["integer64_value"], &value); err != nil {
			return err
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 || strconv.FormatInt(parsed, 10) != value {
			return errors.New("SDK Integer64 time is not canonical")
		}
	}
	wire := new(rtiv1.LogicalTimeValue)
	if err := protojson.Unmarshal(raw, wire); err != nil {
		return err
	}
	family, err := logicaltime.ParseRepresentation(scalar)
	if err != nil {
		return err
	}
	_, err = codec.DecodeLogicalTime(family, wire)
	return err
}

func parseCallback(raw json.RawMessage, e Envelope, serverOnly bool) (*rtiv1.FederateEvent, error) {
	wire := new(rtiv1.FederateEvent)
	if err := protojson.Unmarshal(raw, wire); err != nil {
		return nil, err
	}
	recovery, _ := recoveryState(e)
	v2 := recovery == "server-v2"
	completed, err := validateCallbackRecovery(wire, v2)
	if err != nil {
		return nil, err
	}
	if completed {
		return wire, nil
	}
	if wire.Event == nil {
		return nil, errors.New("SDK pending callback has no event")
	}
	if err := validateCallbackProtocol(wire, e.Scalar, v2); err != nil {
		return nil, err
	}
	if err := validateCallbackCapability(wire, e, v2, serverOnly); err != nil {
		return nil, err
	}
	if err := walkMessage(wire.ProtoReflect(), func(_ protoreflect.Message, f protoreflect.FieldDescriptor, v protoreflect.Value) error {
		return validateCallbackField(e, f, v)
	}); err != nil {
		return nil, err
	}
	return wire, nil
}
func validateCallbacks(raws []json.RawMessage, e Envelope, serverOnly bool) error {
	_, saved := recoveryState(e)
	outcomes := map[string]bool{}
	for _, outcome := range saved {
		outcomes[outcome.Identity] = true
	}
	for _, raw := range raws {
		wire, err := parseCallback(raw, e, serverOnly)
		if err != nil {
			return err
		}
		if recovery := wire.GetCallbackRecovery(); recovery != nil && recovery.OutcomeOnly && !recovery.Completed && !outcomes[stableCallbackIdentity(wire)] {
			return errors.New("outcome-only SDK history has no known completed result")
		}
	}
	return nil
}
func isFederateField(name protoreflect.Name) bool {
	return name == "producing_federate" || name == "sender_federate" || name == "required_federates" || name == "failed_federates" || name == "failed_to_sync" || name == "divesting_federate" || name == "owning_federate"
}
func walkMessage(m protoreflect.Message, visit func(protoreflect.Message, protoreflect.FieldDescriptor, protoreflect.Value) error) error {
	var err error
	m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if f.IsMap() {
			return true
		}
		values := []protoreflect.Value{v}
		if f.IsList() {
			values = nil
			list := v.List()
			for i := 0; i < list.Len(); i++ {
				values = append(values, list.Get(i))
			}
		}
		for _, item := range values {
			if err = visit(m, f, item); err != nil {
				return false
			}
			if f.Kind() == protoreflect.MessageKind {
				if err = walkMessage(item.Message(), visit); err != nil {
					return false
				}
			}
		}
		return true
	})
	return err
}

// References contributes SDK-only producers to the complete graph allocation.
// It excludes object, FOM, DDM and retraction-sequence handles.
func References(encoded []byte) ([]core.FederateHandle, error) {
	e, err := Decode(encoded)
	if err != nil {
		return nil, err
	}
	refs := map[core.FederateHandle]bool{core.FederateHandle(e.Self): true}
	var callbacks []json.RawMessage
	switch e.Language {
	case "go":
		var s goState
		_ = json.Unmarshal(e.State, &s)
		for _, d := range s.Deleted {
			refs[core.FederateHandle(d.Sender)] = true
		}
		callbacks = s.Callbacks
	case checkpointPython:
		var s pythonState
		_ = json.Unmarshal(e.State, &s)
		callbacks = s.Callbacks
	case checkpointJava:
		return javaReferences(e)
	}
	for _, raw := range callbacks {
		wire, err := parseCallback(raw, e, e.Language == checkpointPython)
		if err != nil {
			return nil, err
		}
		_ = walkMessage(wire.ProtoReflect(), func(_ protoreflect.Message, f protoreflect.FieldDescriptor, v protoreflect.Value) error {
			if isFederateField(f.Name()) {
				refs[core.FederateHandle(v.Uint())] = true
			}
			return nil
		})
	}
	localRefs, err := localReferences(e)
	if err != nil {
		return nil, err
	}
	for _, h := range localRefs {
		refs[h] = true
	}
	result := make([]core.FederateHandle, 0, len(refs))
	for h := range refs {
		result = append(result, h)
	}
	slices.Sort(result)
	return result, nil
}

func Rebind(ctx context.Context, in savepoint.ClientStateRebindRequest) ([]byte, error) {
	if err := Validate(ctx, savepoint.ClientStateValidationRequest{Federation: in.Federation, Label: in.Label, SourceGeneration: in.SourceGeneration, SourceHandle: in.SourceHandle, LogicalTimeImplementationName: in.LogicalTimeImplementationName, State: in.State}); err != nil {
		return nil, err
	}
	e, err := Decode(in.State)
	if err != nil {
		return nil, err
	}
	if !validFederate(uint64(in.TargetHandle), in.TargetGeneration) {
		return nil, errors.New("invalid target SDK identity")
	}
	// A graph candidate may use a complete map containing former producers. Do
	// not translate its already-target-scoped SDK state for a second time.
	if len(in.ReboundState) != 0 && !bytes.Equal(in.ReboundState, in.State) {
		return validateReboundState(e, in)
	}
	refs, err := References(in.State)
	if err != nil {
		return nil, err
	}
	seenTargets := map[core.FederateHandle]bool{}
	for _, source := range refs {
		target, ok := in.FederateHandles[source]
		if !ok || !validFederate(uint64(target), in.TargetGeneration) || seenTargets[target] {
			return nil, errors.New("missing or duplicate SDK federate mapping")
		}
		seenTargets[target] = true
	}
	if in.FederateHandles[in.SourceHandle] != in.TargetHandle {
		return nil, errors.New("SDK self mapping differs")
	}
	state, err := rewriteState(e, in.FederateHandles)
	if err != nil {
		return nil, err
	}
	e.Generation, e.Self, e.State = in.TargetGeneration, uint64(in.TargetHandle), state
	encoded, err := Encode(e)
	if err != nil {
		return nil, err
	}
	if _, err = Decode(encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

func stateInvariant(e Envelope) (any, error) {
	encoded, err := Encode(e)
	if err != nil {
		return nil, err
	}
	refs, err := References(encoded)
	if err != nil {
		return nil, err
	}
	mapping := map[core.FederateHandle]core.FederateHandle{}
	for _, h := range refs {
		mapping[h] = 1
	}
	normalized, err := rewriteState(e, mapping)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(normalized))
	d.UseNumber()
	var result any
	if err := d.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func rewriteState(e Envelope, mapping map[core.FederateHandle]core.FederateHandle) (json.RawMessage, error) {
	var state map[string]json.RawMessage
	if err := json.Unmarshal(e.State, &state); err != nil {
		return nil, err
	}
	if e.Language == checkpointJava {
		return rewriteJavaState(e, mapping)
	}
	if e.Language == "go" {
		var s goState
		_ = json.Unmarshal(e.State, &s)
		for i := range s.Deleted {
			s.Deleted[i].Sender = uint64(mapping[core.FederateHandle(s.Deleted[i].Sender)])
		}
		if _, present := state["deleted_objects"]; present {
			state["deleted_objects"], _ = json.Marshal(s.Deleted)
		}
	}
	var callbacks []json.RawMessage
	if raw, present := state["pending_callbacks"]; present {
		if err := json.Unmarshal(raw, &callbacks); err != nil {
			return nil, err
		}
		for i, raw := range callbacks {
			wire, err := parseCallback(raw, e, e.Language == checkpointPython)
			if err != nil {
				return nil, err
			}
			if err := rewriteMessage(wire.ProtoReflect(), mapping); err != nil {
				return nil, err
			}
			callbacks[i], err = protojson.MarshalOptions{UseProtoNames: true}.Marshal(wire)
			if err != nil {
				return nil, err
			}
		}
		state["pending_callbacks"], _ = json.Marshal(callbacks)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return rewriteLocalNotifications(e, encoded, mapping)
}
func rewriteMessage(m protoreflect.Message, mapping map[core.FederateHandle]core.FederateHandle) error {
	var err error
	m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if f.IsMap() {
			return true
		}
		if f.IsList() {
			list := v.List()
			for i := 0; i < list.Len(); i++ {
				item := list.Get(i)
				if isFederateField(f.Name()) {
					target, ok := mapping[core.FederateHandle(item.Uint())]
					if !ok {
						err = errors.New("missing callback federate mapping")
						return false
					}
					list.Set(i, protoreflect.ValueOfUint64(uint64(target)))
				} else if f.Kind() == protoreflect.MessageKind {
					if err = rewriteMessage(item.Message(), mapping); err != nil {
						return false
					}
				}
			}
			return true
		}
		if isFederateField(f.Name()) {
			target, ok := mapping[core.FederateHandle(v.Uint())]
			if !ok {
				err = errors.New("missing callback federate mapping")
				return false
			}
			m.Set(f, protoreflect.ValueOfUint64(uint64(target)))
		} else if f.Kind() == protoreflect.MessageKind {
			err = rewriteMessage(v.Message(), mapping)
		}
		return err == nil
	})
	return err
}

func marshalCallback(wire *rtiv1.FederateEvent) ([]byte, error) {
	return protojson.MarshalOptions{UseProtoNames: true}.Marshal(wire)
}

func validateGoState(e Envelope) error {
	var s goState
	if err := strictDecode(e.State, &s); err != nil {
		return err
	}
	if err := requireFields(e.State, "callback_epoch", "known_objects", "callback_recovery"); err != nil {
		return err
	}
	if err := validateRecoveryState(e, s.Recovery, s.Outcomes); err != nil {
		return err
	}
	if err := validateGoObjects(s, e); err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, p := range append(slices.Clone(s.PendingData), s.PendingControls...) {
		decoded, err := hex.DecodeString(p.Identity)
		if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != p.Identity || p.InvocationState != "unentered" || ids[p.Identity] {
			return errors.New("invalid SDK pending data identity")
		}
		ids[p.Identity] = true
	}
	return validateCallbacks(s.Callbacks, e, false)
}

func validatePythonState(e Envelope) error {
	var s pythonState
	if err := strictDecode(e.State, &s); err != nil {
		return err
	}
	if err := requireFields(e.State, "callback_epoch", "logical_time", "object_knowledge", "next_retraction_handle", "used_retraction_handles", "pending_callbacks", "callback_recovery"); err != nil {
		return err
	}
	if err := validateRecoveryState(e, s.Recovery, s.Outcomes); err != nil {
		return err
	}
	if !bytes.Equal(bytes.TrimSpace(s.Time), []byte("null")) {
		if err := validateTime(s.Time, e.Scalar); err != nil {
			return err
		}
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(e.State, &fields)
	if err := requireFields(fields["object_knowledge"], "latest", "retired"); err != nil {
		return err
	}
	for _, rows := range [][][]uint64{s.Knowledge.Latest, s.Knowledge.Retired} {
		seen := map[uint64]bool{}
		for _, r := range rows {
			if len(r) != 2 || r[0] == 0 || seen[r[0]] {
				return errors.New("invalid Python object knowledge")
			}
			seen[r[0]] = true
		}
	}
	if err := uniqueNonzero(s.Used); err != nil {
		return err
	}
	return validateCallbacks(s.Callbacks, e, true)
}

func validateGoObjects(s goState, e Envelope) error {
	objects := map[uint64]bool{}

	for _, o := range s.Objects {
		if err := validateRuntimeObject(o); err != nil {
			return err
		}
		if objects[o.Object] {
			return errors.New("duplicate SDK object")
		}
		objects[o.Object] = true
	}
	if err := validateGoDeletions(s.Deleted, e.Generation); err != nil {
		return err
	}
	removed := map[uint64]bool{}
	for _, r := range s.Removals {
		if !objects[r.Object] || removed[r.Object] {
			return errors.New("invalid SDK pending removal")
		}
		removed[r.Object] = true
		if err := validateTime(r.Time, e.Scalar); err != nil {
			return err
		}
	}
	for _, h := range s.Final {
		if !objects[h] || removed[h] {
			return errors.New("invalid SDK final removal")
		}
		removed[h] = true
	}
	return nil
}

func validateRuntimeObject(o runtimeObject) error {
	if o.Object == 0 || o.Class == 0 {
		return errors.New("zero SDK object/class")
	}
	return uniqueNonzero(o.Owned)
}

func validateGoDeletions(deleted []runtimeDeletion, generation uint64) error {
	deletions := map[[2]uint64]bool{}
	for _, o := range deleted {
		if err := validateRuntimeObject(o.runtimeObject); err != nil {
			return err
		}
		key := [2]uint64{o.Sender, o.Retraction}
		if !validFederate(o.Sender, generation) || o.Retraction == 0 || deletions[key] {
			return errors.New("invalid SDK deletion designator")
		}
		deletions[key] = true
	}
	return nil
}

func validateCallbackRecovery(wire *rtiv1.FederateEvent, v2 bool) (bool, error) {
	if metadata := wire.GetCallbackRecovery(); metadata != nil {
		if !v2 || len(metadata.InvocationIdentity) != 32 || metadata.Completed && !metadata.OutcomeOnly {
			return false, errors.New("invalid SDK callback recovery metadata")
		}
		if metadata.Completed {
			if wire.Event != nil || len(wire.CallbackReceipt) != 0 || wire.OwnershipReceipt != nil {
				return false, errors.New("completed reconciliation marker carries body or capability")
			}
			return true, nil
		}
	}
	return false, nil
}

func validateCallbackProtocol(wire *rtiv1.FederateEvent, scalar string, v2 bool) error {
	if scalar == checkpointHLAinteger64Time &&
		(wire.GetGrant() != nil && wire.GetGrant().GetLogicalTimeValue() == nil ||
			wire.GetTimeRegulationEnabled() != nil && wire.GetTimeRegulationEnabled().GetLogicalTimeValue() == nil ||
			wire.GetTimeConstrainedEnabled() != nil && wire.GetTimeConstrainedEnabled().GetLogicalTimeValue() == nil) {
		return errors.New("SDK Integer64 time callback lacks its exact typed time")
	}
	switch wire.Event.(type) {
	case *rtiv1.FederateEvent_SaveInitiate, *rtiv1.FederateEvent_SaveCompleted, *rtiv1.FederateEvent_SaveFailed, *rtiv1.FederateEvent_RestoreInitiate, *rtiv1.FederateEvent_RestoreCompleted, *rtiv1.FederateEvent_RestoreFailed, *rtiv1.FederateEvent_RestoreRequestSucceeded, *rtiv1.FederateEvent_RestoreRequestFailed, *rtiv1.FederateEvent_RestoreBegun, *rtiv1.FederateEvent_LocalValidationBarrier, *rtiv1.FederateEvent_Halted:
		if !v2 || wire.GetLocalValidationBarrier() != nil || wire.GetHalted() != nil {
			return errors.New("unsupported SDK checkpoint protocol callback")
		}
	}
	return nil
}

func validateCallbackCapability(wire *rtiv1.FederateEvent, e Envelope, v2, serverOnly bool) error {
	data := isDataCallback(wire)
	switch {
	case v2:
		if len(wire.CallbackReceipt) != 32 {
			return errors.New("v2 SDK historical callback requires its original receipt identity")
		}
		// Capabilities in private history are never replay tickets. V2 SDKs
		// discard these carriers and receive fresh server ledger carriers.
	case serverOnly:
		if !data || len(wire.CallbackReceipt) == 0 || wire.OwnershipReceipt != nil {
			return errors.New("Python checkpoint requires server-authoritative receipt-bearing data")
		}
	case e.Language == checkpointJava && len(wire.CallbackReceipt) != 0 && data && wire.OwnershipReceipt == nil:
		// Historical receipts are reconciliation keys, never SDK replay tickets.
	case len(wire.CallbackReceipt) != 0 || wire.OwnershipReceipt != nil || wire.GetTransportationChangeConfirmed() != nil || data:
		return errors.New("SDK local replay lacks portable fresh capability admission")
	}
	return nil
}

func validateReboundState(e Envelope, in savepoint.ClientStateRebindRequest) ([]byte, error) {
	candidate, err := Decode(in.ReboundState)
	if err != nil {
		return nil, err
	}
	if candidate.Generation != in.TargetGeneration || candidate.Self != uint64(in.TargetHandle) || candidate.Scalar != e.Scalar || candidate.Language != e.Language {
		return nil, errors.New("invalid prepared SDK target identity")
	}
	original, err := stateInvariant(e)
	if err != nil {
		return nil, err
	}
	translated, err := stateInvariant(candidate)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(original, translated) {
		return nil, errors.New("prepared SDK candidate changed immutable runtime state")
	}
	return bytes.Clone(in.ReboundState), nil
}

func validateCallbackField(e Envelope, f protoreflect.FieldDescriptor, v protoreflect.Value) error {
	if isFederateField(f.Name()) && !validFederate(v.Uint(), e.Generation) {
		return errors.New("pending SDK callback federate belongs to another execution")
	}
	if (f.Name() == "logical_time" || f.Name() == "save_time") && (f.Kind() == protoreflect.DoubleKind || f.Kind() == protoreflect.FloatKind) {
		at := v.Float()
		if e.Scalar != "HLAfloat64Time" || at < 0 || math.IsNaN(at) || math.IsInf(at, 0) {
			return errors.New("pending SDK callback has mixed/invalid scalar")
		}
	}
	if f.Kind() == protoreflect.MessageKind && f.Message().FullName() == "rti.v1.LogicalTimeValue" {
		family, _ := logicaltime.ParseRepresentation(e.Scalar)
		_, err := codec.DecodeLogicalTime(family, v.Message().Interface().(*rtiv1.LogicalTimeValue))
		return err
	}
	return nil
}

func isDataCallback(wire *rtiv1.FederateEvent) bool {
	return wire.GetDiscover() != nil || wire.GetReflect() != nil || wire.GetReceive() != nil || wire.GetRemove() != nil
}
