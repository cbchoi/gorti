package savebundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type federateRewrite = func(core.FederateHandle) (core.FederateHandle, error)

// Only the named domain FederateHandle type is rewritten. Object, region,
// class and retraction IDs remain unchanged, even when numerically identical.
func rewriteTypedFederates(v reflect.Value, rewrite federateRewrite) error {
	if !v.IsValid() {
		return nil
	}
	if v.Type() == reflect.TypeOf(core.FederateHandle(0)) {
		return rewriteFederateValue(v, rewrite)
	}
	if v.Type() == reflect.TypeOf(core.MOMAttributePoint{}) {
		return rewriteMOMPoint(v, rewrite)
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return rewriteTypedFederates(v.Elem(), rewrite)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if err := rewriteTypedFederates(v.Field(i), rewrite); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return nil
		}
		for i := 0; i < v.Len(); i++ {
			if err := rewriteTypedFederates(v.Index(i), rewrite); err != nil {
				return err
			}
		}
	case reflect.Map:
		return rewriteFederateMap(v, rewrite)
	}
	return nil
}

func rewriteJSONSnapshot(data []byte, wire any, rewrite federateRewrite, version func() bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if err := decodeSnapshotJSON(data, wire); err != nil {
		return nil, err
	}
	if !version() {
		return nil, fmt.Errorf("unsupported snapshot version")
	}
	if err := rewriteTypedFederates(reflect.ValueOf(wire), rewrite); err != nil {
		return nil, err
	}
	return json.Marshal(wire)
}

func (ObjectOf[T]) RewriteFederates(data []byte, rewrite federateRewrite) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var v objectSnapshotV1Of[T]
	if err := decodeSnapshotJSON(data, &v); err != nil {
		return nil, err
	}
	if err := validateObjectSnapshotVersionOf[T](v); err != nil {
		return nil, err
	}
	if err := rewriteTypedFederates(reflect.ValueOf(&v), rewrite); err != nil {
		return nil, err
	}
	for i := range v.State.Deletions {
		recipients := v.State.Deletions[i].Recipients
		slices.SortFunc(recipients, func(a, b object.DeletionRecipientState) int {
			if a.Recipient < b.Recipient {
				return -1
			}
			if a.Recipient > b.Recipient {
				return 1
			}
			return 0
		})
		for j, recipient := range recipients {
			if recipient.Recipient == 0 || recipient.Recipient == ^core.FederateHandle(0) || j > 0 && recipient.Recipient == recipients[j-1].Recipient {
				return nil, fmt.Errorf("invalid rewritten deletion recipient")
			}
		}
	}
	return json.Marshal(v)
}
func (Declaration) RewriteFederates(data []byte, rewrite federateRewrite) ([]byte, error) {
	var v declarationSnapshot
	return rewriteJSONSnapshot(data, &v, rewrite, func() bool { return v.Version >= 1 && v.Version <= declarationSnapshotVersion })
}
func (TimeOf[T]) RewriteFederates(data []byte, rewrite federateRewrite) ([]byte, error) {
	if integerScalar[T]() {
		return rewriteTimeOf[integerSnapshotNumber](data, rewrite)
	}
	return rewriteTimeOf[timeSnapshotNumber](data, rewrite)
}

func rewriteTimeOf[N core.LogicalTimeScalar](data []byte, rewrite federateRewrite) ([]byte, error) {
	var v timeSnapshotOf[N]
	return rewriteJSONSnapshot(data, &v, rewrite, func() bool { return validateTimeSnapshotVersionOf[N](v) == nil })
}
func (RetractionsOf[T]) RewriteFederates(data []byte, rewrite federateRewrite) ([]byte, error) {
	history, err := readRetractionsOf[T](data)
	if err != nil {
		return nil, err
	}
	if err = rewriteTypedFederates(reflect.ValueOf(history), rewrite); err != nil {
		return nil, err
	}
	for i := range history.Producers {
		for j := range history.Producers[i].Messages {
			slices.Sort(history.Producers[i].Messages[j].Delivered)
		}
	}
	return writeRetractionsOf[T](history)
}
func (FOM) RewriteFederates(data []byte, _ federateRewrite) ([]byte, error) {
	var state core.FOMRepositoryState
	if err := decodeSnapshotJSON(data, &state); err != nil {
		return nil, err
	}
	if state.Version != 1 {
		return nil, fmt.Errorf("unsupported FOM snapshot version")
	}
	return bytes.Clone(data), nil
}

// Protobuf snapshot schemas use uint64 for all handle kinds. The explicitly
// enumerated fields below are the federate references of these closed DTOs.
func rewriteProtoFederates(message protoreflect.Message, fields map[protoreflect.Name]bool, rewrite federateRewrite) error {
	if len(message.GetUnknown()) != 0 {
		return fmt.Errorf("unknown protobuf snapshot fields in %s", message.Descriptor().FullName())
	}
	var err error
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.IsMap() {
			err = fmt.Errorf("unexpected protobuf snapshot map %s", field.FullName())
			return false
		}
		one := func(v protoreflect.Value) (protoreflect.Value, error) {
			if field.Kind() == protoreflect.MessageKind {
				return v, rewriteProtoFederates(v.Message(), fields, rewrite)
			}
			if !fields[field.Name()] || v.Uint() == 0 {
				return v, nil
			}
			h, e := rewrite(core.FederateHandle(v.Uint()))
			return protoreflect.ValueOfUint64(uint64(h)), e
		}
		if field.Kind() != protoreflect.MessageKind && !fields[field.Name()] {
			return true
		}
		if field.IsList() {
			list := value.List()
			for i := 0; i < list.Len(); i++ {
				var next protoreflect.Value
				next, err = one(list.Get(i))
				if err != nil {
					return false
				}
				list.Set(i, next)
			}
		} else {
			var next protoreflect.Value
			next, err = one(value)
			if err != nil {
				return false
			}
			message.Set(field, next)
		}
		return true
	})
	return err
}
func rewriteProtoSnapshot(data []byte, message proto.Message, fields map[protoreflect.Name]bool, rewrite federateRewrite) ([]byte, error) {
	if err := proto.Unmarshal(data, message); err != nil {
		return nil, err
	}
	if err := rewriteProtoFederates(message.ProtoReflect(), fields, rewrite); err != nil {
		return nil, err
	}
	return (proto.MarshalOptions{Deterministic: true}).Marshal(message)
}
func (DDM) RewriteFederates(data []byte, rewrite federateRewrite) ([]byte, error) {
	if _, err := decodeDDMState("", data); err != nil {
		return nil, err
	}
	rewritten, err := rewriteProtoSnapshot(data, &rtiv1.DDMManagerState{}, map[protoreflect.Name]bool{"owner_federate": true, "subscriber_federate": true, "associating_federate": true, "federate_handle": true}, rewrite)
	if err != nil {
		return nil, err
	}
	if _, err := decodeDDMState("", rewritten); err != nil {
		return nil, err
	}
	return rewritten, nil
}
func (Ownership) RewriteFederates(data []byte, rewrite federateRewrite) ([]byte, error) {
	wire, history, offers, sessions, callbacks, err := decodeOwnershipSnapshotComplete(data)
	if err != nil {
		return nil, err
	}
	if err = rewriteProtoFederates(wire.ProtoReflect(), map[protoreflect.Name]bool{"owner_federate": true, "acquirer_federate": true}, rewrite); err != nil {
		return nil, err
	}
	if err = rewriteTypedFederates(reflect.ValueOf(offers), rewrite); err != nil {
		return nil, err
	}
	if err = rewriteTypedFederates(reflect.ValueOf(sessions), rewrite); err != nil {
		return nil, err
	}
	if err = rewriteTypedFederates(reflect.ValueOf(callbacks), rewrite); err != nil {
		return nil, err
	}
	for i := range sessions {
		sessions[i].FederationGeneration = uint64(sessions[i].Recipient) >> 32
	}
	return marshalOwnershipSnapshotComplete(wire, history, offers, sessions, callbacks)
}
func (Synchronization) RewriteFederates(data []byte, rewrite federateRewrite) ([]byte, error) {
	if isStrictSynchronization(data) {
		return (StrictSynchronization{}).RewriteFederates(data, rewrite)
	}
	return rewriteProtoSnapshot(data, &rtiv1.SyncManagerState{}, map[protoreflect.Name]bool{"required": true, "achieved": true, "failed": true}, rewrite)
}

func decodeMOMGraph(data []byte) (*rtiv1.MOMManagerState, *momSnapshotRuntime, error) {
	representation := logicaltime.HLAfloat64Time
	var runtime *momSnapshotRuntime
	if bytes.HasPrefix(data, []byte(momSnapshotMagic)) {
		var envelope momSnapshotEnvelope
		if err := decodeSnapshotJSON(data[len(momSnapshotMagic):], &envelope); err != nil {
			return nil, nil, err
		}
		if err := validateMOMEnvelope(envelope); err != nil {
			return nil, nil, fmt.Errorf("incomplete MOM snapshot extension")
		}
		if envelope.Version == 2 {
			representation = logicaltime.HLAinteger64Time
		}
		data, runtime = envelope.LegacyProtoBytes, envelope.Runtime
	}
	wire := &rtiv1.MOMManagerState{}
	if err := validateMOMTimeWire(data, wire.ProtoReflect().Descriptor(), representation); err != nil {
		return nil, nil, err
	}
	if err := proto.Unmarshal(data, wire); err != nil {
		return nil, nil, err
	}
	for _, record := range wire.Federates {
		if _, _, err := decodeMOMTimeRecord(record, representation); err != nil {
			return nil, nil, err
		}
	}
	return wire, runtime, nil
}
func (MOM) RewriteFederates(data []byte, rewrite federateRewrite) ([]byte, error) {
	wire, runtime, err := decodeMOMGraph(data)
	if err != nil {
		return nil, err
	}
	if err = rewriteProtoFederates(wire.ProtoReflect(), map[protoreflect.Name]bool{"handle": true, "federate_handles": true}, rewrite); err != nil {
		return nil, err
	}
	base, err := (proto.MarshalOptions{Deterministic: true}).Marshal(wire)
	if err != nil || runtime == nil {
		return base, err
	}
	if err = rewriteTypedFederates(reflect.ValueOf(runtime), rewrite); err != nil {
		return nil, err
	}
	envelope := momSnapshotEnvelope{Version: 1, LegacyProtoBytes: base, Runtime: runtime}
	if bytes.HasPrefix(data, []byte(momSnapshotMagic)) {
		var original momSnapshotEnvelope
		if err := decodeSnapshotJSON(data[len(momSnapshotMagic):], &original); err != nil {
			return nil, err
		}
		envelope.Version = original.Version
		envelope.LogicalTimeImplementationName = original.LogicalTimeImplementationName
	}
	extension, err := json.Marshal(envelope)
	return append([]byte(momSnapshotMagic), extension...), err
}

func (g RestoreGraphOf[T]) References(manifest core.CheckpointManifestOf[T]) ([]core.FederateHandle, error) {
	if err := validateManifestScalar(manifest); err != nil {
		return nil, err
	}
	seen := map[core.FederateHandle]bool{}
	collect := func(h core.FederateHandle) (core.FederateHandle, error) {
		if h == 0 || h == ^core.FederateHandle(0) {
			return h, nil
		}
		if manifest.FederationGeneration == nil || uint64(h)>>32 != *manifest.FederationGeneration || uint64(h)&0xffffffff == 0 {
			return 0, fmt.Errorf("saved federate %d belongs to another execution", h)
		}
		seen[h] = true
		return h, nil
	}
	for _, h := range manifest.Federates {
		if _, err := collect(h); err != nil {
			return nil, err
		}
	}
	for key, data := range manifest.ManagerSnapshots {
		adapter, ok := g.snapshotAdapter(key).(core.FederateSnapshotRebinder)
		if !ok {
			return nil, fmt.Errorf("snapshot %q has no checked identity rewriting contract", key)
		}
		if _, err := adapter.RewriteFederates(data, collect); err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", key, err)
		}
	}
	handles := make([]core.FederateHandle, 0, len(seen))
	for h := range seen {
		handles = append(handles, h)
	}
	slices.Sort(handles)
	return handles, nil
}

func (g RestoreGraphOf[T]) Rebind(manifest core.CheckpointManifestOf[T], generation uint64, mapping map[core.FederateHandle]core.FederateHandle) (core.CheckpointManifestOf[T], error) {
	if err := validateManifestScalar(manifest); err != nil {
		return core.CheckpointManifestOf[T]{}, err
	}
	rewrite := func(h core.FederateHandle) (core.FederateHandle, error) {
		if h == 0 || h == ^core.FederateHandle(0) {
			return h, nil
		}
		target, ok := mapping[h]
		if !ok || target == 0 || uint64(target)>>32 != generation {
			return 0, fmt.Errorf("missing current-execution identity for saved federate %d", h)
		}
		return target, nil
	}
	out := manifest
	out.FederationGeneration = &generation
	out.Federates = make([]core.FederateHandle, len(manifest.Federates))
	out.ManagerSnapshots = make(map[string][]byte, len(manifest.ManagerSnapshots))
	for i, h := range manifest.Federates {
		target, err := rewrite(h)
		if err != nil {
			return core.CheckpointManifestOf[T]{}, err
		}
		out.Federates[i] = target
	}
	for key, data := range manifest.ManagerSnapshots {
		adapter, ok := g.snapshotAdapter(key).(core.FederateSnapshotRebinder)
		if !ok {
			return core.CheckpointManifestOf[T]{}, fmt.Errorf("snapshot %q has no checked identity rewriting contract", key)
		}
		var err error
		out.ManagerSnapshots[key], err = adapter.RewriteFederates(data, rewrite)
		if err != nil {
			return core.CheckpointManifestOf[T]{}, fmt.Errorf("snapshot %s: %w", key, err)
		}
	}
	return out, nil
}

func (g RestoreGraphOf[T]) snapshotAdapter(key string) any {
	if adapter, ok := g.Snapshots[key]; ok {
		return adapter
	}
	switch key {
	case checkpointObject:
		return ObjectOf[T]{}
	case "declaration":
		return Declaration{}
	case "time":
		return TimeOf[T]{}
	case "retractions":
		return RetractionsOf[T]{}
	case "ddm":
		return DDM{}
	case "ownership":
		return Ownership{}
	case "sync":
		return Synchronization{}
	case "mom":
		return MOM{}
	case "fom":
		return FOM{}
	}
	return nil
}

func rewriteMOMPoint(v reflect.Value, rewrite federateRewrite) error {
	point := v.Interface().(core.MOMAttributePoint)
	if point.Subject == 0 || point.Subject == ^core.FederateHandle(0) || point.Dimension == 0 || point.Coordinate != uint64(point.Subject)&0xffffffff {
		return fmt.Errorf("invalid saved MOM attribute point")
	}
	subject, err := rewrite(point.Subject)
	if err != nil {
		return err
	}
	if subject == 0 || subject == ^core.FederateHandle(0) {
		return fmt.Errorf("invalid rewritten MOM point subject")
	}
	point.Subject = subject
	point.Coordinate = uint64(subject) & 0xffffffff
	v.Set(reflect.ValueOf(point))
	return nil
}

func rewriteFederateMap(v reflect.Value, rewrite federateRewrite) error {
	for _, key := range v.MapKeys() {
		value := reflect.New(v.Type().Elem()).Elem()
		value.Set(v.MapIndex(key))
		if err := rewriteTypedFederates(value, rewrite); err != nil {
			return err
		}
		newKey := reflect.New(v.Type().Key()).Elem()
		newKey.Set(key)
		if err := rewriteTypedFederates(newKey, rewrite); err != nil {
			return err
		}
		if !reflect.DeepEqual(key.Interface(), newKey.Interface()) {
			if v.MapIndex(newKey).IsValid() {
				return fmt.Errorf("colliding rewritten map key")
			}
			v.SetMapIndex(key, reflect.Value{})
		}
		v.SetMapIndex(newKey, value)
	}
	return nil
}

func rewriteFederateValue(v reflect.Value, rewrite federateRewrite) error {
	if v.Uint() == 0 {
		return nil
	}
	h, err := rewrite(core.FederateHandle(v.Uint()))
	if err != nil {
		return err
	}
	v.SetUint(uint64(h))
	return nil
}

const checkpointObject = "object"
