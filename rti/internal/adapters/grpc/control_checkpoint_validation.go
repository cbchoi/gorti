package grpc

import (
	"fmt"
	"strings"

	wire "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Object-only callbacks require a separate saved recipient/class graph check;
// the name lookup cannot resolve an object instance to its caller-known class.
func validateCheckpointControl(event *rtiv1.FederateEvent, names core.FOMHandleNameLookup, kind logicaltime.Representation) error {
	if event == nil {
		return fmt.Errorf("callback checkpoint: nil control")
	}
	m := event.ProtoReflect()
	f := m.WhichOneof(m.Descriptor().Oneofs().ByName("event"))
	if f == nil {
		return fmt.Errorf("callback checkpoint: missing control body")
	}
	name := string(f.Name())
	switch name {
	case "discover", "reflect", callbackReceive, "remove":
		return nil // Validated by the data adapter.
	case callbackLocalValidationBarrier, callbackHalted:
		return fmt.Errorf("callback checkpoint: internal event is not a recoverable control")
	}
	b := m.Get(f).Message()
	if err := validateCheckpointControlScalars(b, name); err != nil {
		return err
	}
	if err := validateCheckpointControlLists(b, name, event); err != nil {
		return err
	}
	if err := validateCheckpointControlClasses(b, name, names); err != nil {
		return err
	}
	return validateCheckpointControlBody(event, b, name, kind)
}

func validateCheckpointControlScalars(b protoreflect.Message, name string) error {
	for _, n := range []protoreflect.Name{"label", "object_name"} {
		if field := b.Descriptor().Fields().ByName(n); field != nil && !(name == "reservation_failed" && n == "object_name") && strings.TrimSpace(b.Get(field).String()) == "" {
			return fmt.Errorf("callback checkpoint: empty %s", n)
		}
	}
	for _, n := range []protoreflect.Name{"object_handle", "message_retraction_handle", "divesting_federate", "owning_federate", "sender_federate"} {
		if field := b.Descriptor().Fields().ByName(n); field != nil && !checkpointControlHandle(b.Get(field).Uint()) {
			// Transportation confirmation selects either object or interaction.
			if name == callbackTransportationConfirmed && n == "object_handle" {
				continue
			}
			return fmt.Errorf("callback checkpoint: invalid %s", n)
		}
	}
	return nil
}

func validateCheckpointControlLists(b protoreflect.Message, name string, event *rtiv1.FederateEvent) error {
	for _, n := range []protoreflect.Name{"attribute_handles", "required_federates", "failed_to_sync"} {
		if field := b.Descriptor().Fields().ByName(n); field != nil {
			list := b.Get(field).List()
			if n == "attribute_handles" && list.Len() == 0 && !(name == callbackTransportationConfirmed && event.GetTransportationChangeConfirmed().InteractionClassHandle != 0) {
				return fmt.Errorf("callback checkpoint: empty attribute set")
			}
			seen := map[uint64]bool{}
			for i := 0; i < list.Len(); i++ {
				h := list.Get(i).Uint()
				if !checkpointControlHandle(h) || seen[h] {
					return fmt.Errorf("callback checkpoint: invalid or duplicate %s", n)
				}
				seen[h] = true
			}
		}
	}
	return nil
}

func validateCheckpointControlClasses(b protoreflect.Message, name string, names core.FOMHandleNameLookup) error {
	for _, n := range []protoreflect.Name{"object_class_handle", "interaction_class_handle"} {
		if field := b.Descriptor().Fields().ByName(n); field != nil {
			h := b.Get(field).Uint()
			if name == callbackTransportationConfirmed && h == 0 {
				continue
			}
			if names == nil || !checkpointControlHandle(h) {
				return fmt.Errorf("callback checkpoint: invalid model reference")
			}
			var known bool
			if n == "object_class_handle" {
				_, known = names.ObjectClassName(core.ObjectClassHandle(h))
			} else {
				_, known = names.InteractionClassName(core.InteractionClassHandle(h))
			}
			if !known {
				return fmt.Errorf("callback checkpoint: unknown %s", n)
			}
		}
	}
	return nil
}

func validateCheckpointControlBody(event *rtiv1.FederateEvent, b protoreflect.Message, name string, kind logicaltime.Representation) error {
	switch name {
	case "grant":
		v := event.GetGrant()
		_, err := wire.DecodeLogicalTimePair(kind, v.LogicalTime, v.LogicalTimeValue)
		return err
	case "time_regulation_enabled":
		v := event.GetTimeRegulationEnabled()
		_, err := wire.DecodeLogicalTimePair(kind, v.LogicalTime, v.LogicalTimeValue)
		return err
	case "time_constrained_enabled":
		v := event.GetTimeConstrainedEnabled()
		_, err := wire.DecodeLogicalTimePair(kind, v.LogicalTime, v.LogicalTimeValue)
		return err
	case "save_initiate":
		v := event.GetSaveInitiate()
		_, err := wire.DecodeOptionalLogicalTimePair(kind, v.SaveTime, v.SaveTimeValue)
		return err
	case "sync_registration_failed":
		r := event.GetSyncRegistrationFailed().Reason
		if r != rtiv1.SyncPointFailureReason_SYNC_POINT_FAILURE_REASON_LABEL_NOT_UNIQUE && r != rtiv1.SyncPointFailureReason_SYNC_POINT_FAILURE_REASON_SET_MEMBER_NOT_JOINED {
			return fmt.Errorf("callback checkpoint: invalid synchronization failure reason")
		}
	case "save_failed", "restore_failed":
		return validateCheckpointLifecycleFailure(b, name)
	default:
		return validateCheckpointControlTarget(event, b, name)
	}
	return nil
}

func validateCheckpointLifecycleFailure(b protoreflect.Message, name string) error {
	field := b.Descriptor().Fields().ByName("reason")
	if b.Has(field) {
		r := b.Get(field).Int()
		maxReason := int64(6)
		if name == "restore_failed" {
			maxReason = 5
		}
		if r < 1 || r > maxReason {
			return fmt.Errorf("callback checkpoint: invalid lifecycle failure reason")
		}
	}
	return nil
}

func validateCheckpointControlTarget(event *rtiv1.FederateEvent, b protoreflect.Message, name string) error {
	switch name {
	case "restore_initiate":
		// Zero is the explicitly documented legacy absence of a remap.
		if h := b.Get(b.Descriptor().Fields().ByName("federate_handle")).Uint(); h != 0 && !checkpointControlHandle(h) {
			return fmt.Errorf("callback checkpoint: invalid restore federate")
		}
	case "restore_completed":
		field := b.Descriptor().Fields().ByName("federate_handle")
		if b.Has(field) && !checkpointControlHandle(b.Get(field).Uint()) {
			return fmt.Errorf("callback checkpoint: invalid restored federate")
		}
	case "reservation_multi_succeeded":
		_, err := checkpointControlNames(event.GetReservationMultiSucceeded().ObjectNames)
		return err
	case "reservation_multi_failed":
		return validateCheckpointCollisions(event.GetReservationMultiFailed())
	case callbackTransportationConfirmed:
		return validateCheckpointTransportationTarget(event.GetTransportationChangeConfirmed())
	case "provide_update", "sync_announced", "sync_synchronized", "sync_registration_succeeded", "ownership_assumption", "ownership_acquired", "ownership_divest_confirmed", "ownership_release_requested", "ownership_unavailable", "ownership_acquisition_cancelled", "save_completed", "restore_request_succeeded", "restore_request_failed", "restore_begun", "reservation_succeeded", "reservation_failed", "start_registration", "stop_registration", "turn_interactions_on", "turn_interactions_off", "attributes_in_scope", "attributes_out_of_scope", "retraction_requested", "turn_updates_on", "turn_updates_off":
	default:
		return fmt.Errorf("callback checkpoint: unsupported control %s", name)
	}
	return nil
}

func validateCheckpointCollisions(v *rtiv1.MultipleObjectInstanceNameReservationFailed) error {
	requested := map[string]bool{}
	for _, n := range v.RequestedNames {
		requested[n] = true
	}
	if len(v.CollidingNames) == 0 {
		return fmt.Errorf("callback checkpoint: missing collision names")
	}
	for _, n := range v.CollidingNames {
		if !requested[n] {
			return fmt.Errorf("callback checkpoint: collision outside requested names")
		}
	}
	return nil
}

func validateCheckpointTransportationTarget(v *rtiv1.TransportationChange) error {
	object := v.ObjectHandle != 0
	interaction := v.InteractionClassHandle != 0
	if object == interaction || (interaction && len(v.AttributeHandles) != 0) || (object && len(v.AttributeHandles) == 0) {
		return fmt.Errorf("callback checkpoint: ambiguous transportation target")
	}
	if v.TransportType != rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE && v.TransportType != rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT {
		return fmt.Errorf("callback checkpoint: invalid transportation type")
	}
	return nil
}

func checkpointControlHandle(h uint64) bool { return h != 0 && h != ^uint64(0) }

func checkpointControlNames(values []string) (map[string]bool, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("callback checkpoint: empty name set")
	}
	seen := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || seen[value] {
			return nil, fmt.Errorf("callback checkpoint: empty or duplicate name")
		}
		seen[value] = true
	}
	return seen, nil
}

// Rewrite a detached copy so a missing roster reference leaves the input intact.
func rewriteCheckpointControlFederates(event *rtiv1.FederateEvent, rewrite func(core.FederateHandle) (core.FederateHandle, error)) error {
	if event == nil || rewrite == nil {
		return fmt.Errorf("callback checkpoint: missing control rewrite input")
	}
	cloned := proto.Clone(event).(*rtiv1.FederateEvent)
	m := cloned.ProtoReflect()
	f := m.WhichOneof(m.Descriptor().Oneofs().ByName("event"))
	if f == nil {
		return nil
	} // Completed markers have no body.
	fieldName := checkpointControlFederateField(f.Name())
	if fieldName == "" {
		return nil
	}
	b := m.Get(f).Message()
	field := b.Descriptor().Fields().ByName(fieldName)
	if err := rewriteCheckpointFederateField(b, field, rewrite); err != nil {
		return err
	}
	proto.Reset(event)
	proto.Merge(event, cloned)
	return nil
}

func checkpointControlFederateField(name protoreflect.Name) protoreflect.Name {
	switch name {
	case "sync_announced":
		return "required_federates"
	case "sync_synchronized":
		return "failed_to_sync"
	case "ownership_assumption":
		return "divesting_federate"
	case "ownership_acquired":
		return "owning_federate"
	case "restore_initiate", "restore_completed":
		return "federate_handle"
	case "retraction_requested":
		return "sender_federate"
	default:
		return ""
	}
}

func rewriteCheckpointFederateField(b protoreflect.Message, field protoreflect.FieldDescriptor, rewrite func(core.FederateHandle) (core.FederateHandle, error)) error {
	mapHandle := func(h uint64) (uint64, error) {
		if !checkpointControlHandle(h) {
			return 0, fmt.Errorf("callback checkpoint: invalid federate reference")
		}
		newHandle, err := rewrite(core.FederateHandle(h))
		if err != nil {
			return 0, err
		}
		if !checkpointControlHandle(uint64(newHandle)) {
			return 0, fmt.Errorf("callback checkpoint: invalid rebound federate")
		}
		return uint64(newHandle), nil
	}
	if field.IsList() {
		list := b.Mutable(field).List()
		seen := map[uint64]bool{}
		for i := 0; i < list.Len(); i++ {
			h, err := mapHandle(list.Get(i).Uint())
			if err != nil {
				return err
			}
			if seen[h] {
				return fmt.Errorf("callback checkpoint: federate rewrite collapses distinct references")
			}
			seen[h] = true
			list.Set(i, protoreflect.ValueOfUint64(h))
		}
	} else if b.Has(field) && b.Get(field).Uint() != 0 {
		h, err := mapHandle(b.Get(field).Uint())
		if err != nil {
			return err
		}
		b.Set(field, protoreflect.ValueOfUint64(h))
	}
	return nil
}

const (
	callbackLocalValidationBarrier  = "local_validation_barrier"
	callbackTransportationConfirmed = "transportation_change_confirmed"
	callbackReceive                 = "receive"
)

const callbackHalted = "halted"
