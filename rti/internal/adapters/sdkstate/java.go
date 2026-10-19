package sdkstate

import (
	"encoding/json"
	"errors"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/protobuf/reflect/protoreflect"
	"slices"
)

type javaObject struct {
	Object uint64 `json:"object_handle"`
	Class  uint64 `json:"class_handle"`
	Name   string `json:"name"`
}
type javaDeletion struct {
	javaObject
	Sender     uint64          `json:"sender_federate"`
	Retraction uint64          `json:"retraction_handle"`
	Time       json.RawMessage `json:"logical_time_value"`
	Expires    bool            `json:"expires_at_grant"`
}
type javaDimension struct {
	Dimension uint64 `json:"dimension_handle"`
	Lower     uint64 `json:"lower"`
	Upper     uint64 `json:"upper"`
}
type javaRegion struct {
	Region     uint64          `json:"region_handle"`
	Dimensions []javaDimension `json:"dimensions"`
}
type javaState struct {
	Federation       string              `json:"federation"`
	Label            string              `json:"label"`
	Objects          []javaObject        `json:"known_objects"`
	Removals         []runtimeRemoval    `json:"pending_removals"`
	Deleted          []javaDeletion      `json:"deleted_objects"`
	Cleared          uint64              `json:"cleared_retraction"`
	Last             uint64              `json:"last_issued_retraction"`
	Regions          []javaRegion        `json:"ddm_regions"`
	Synchronizations []string            `json:"synchronizations"`
	Policy           uint32              `json:"time_policy_flags"`
	Time             json.RawMessage     `json:"logical_time_value"`
	Callbacks        []json.RawMessage   `json:"pending_callbacks"`
	Recovery         string              `json:"callback_recovery,omitempty"`
	Outcomes         []pendingOutcome    `json:"pending_outcomes,omitempty"`
	Locals           []localNotification `json:"pending_local_notifications,omitempty"`
}

func validateJavaState(e Envelope) error {
	var s javaState
	if err := strictDecode(e.State, &s); err != nil {
		return err
	}
	if err := requireFields(e.State, "federation", "label", "known_objects", "pending_removals", "deleted_objects", "cleared_retraction", "last_issued_retraction", "ddm_regions", "synchronizations", "time_policy_flags", "logical_time_value", "pending_callbacks"); err != nil {
		return err
	}
	if err := validateRecoveryState(e, s.Recovery, s.Outcomes); err != nil {
		return err
	}
	if err := validateJavaProtocol(s); err != nil {
		return err
	}
	if err := validateTime(s.Time, e.Scalar); err != nil {
		return err
	}
	if err := validateJavaObjects(s, e.Scalar); err != nil {
		return err
	}
	if err := validateJavaDeletions(s.Deleted, e); err != nil {
		return err
	}
	if err := validateJavaRegions(s.Regions); err != nil {
		return err
	}
	labels := map[string]bool{}
	for _, label := range s.Synchronizations {
		if labels[label] {
			return errors.New("duplicate Java synchronization")
		}
		labels[label] = true
	}
	return validateCallbacks(s.Callbacks, e, false)
}
func javaReferences(e Envelope) ([]core.FederateHandle, error) {
	var s javaState
	_ = json.Unmarshal(e.State, &s)
	seen := map[core.FederateHandle]bool{core.FederateHandle(e.Self): true}
	for _, d := range s.Deleted {
		seen[core.FederateHandle(d.Sender)] = true
	}
	for _, raw := range s.Callbacks {
		wire, err := parseCallback(raw, e, false)
		if err != nil {
			return nil, err
		}
		_ = walkMessage(wire.ProtoReflect(), func(_ protoreflect.Message, f protoreflect.FieldDescriptor, v protoreflect.Value) error {
			if isFederateField(f.Name()) {
				seen[core.FederateHandle(v.Uint())] = true
			}
			return nil
		})
	}
	localRefs, err := localReferences(e)
	if err != nil {
		return nil, err
	}
	for _, h := range localRefs {
		seen[h] = true
	}
	result := make([]core.FederateHandle, 0, len(seen))
	for h := range seen {
		result = append(result, h)
	}
	slices.Sort(result)
	return result, nil
}
func rewriteJavaState(e Envelope, mapping map[core.FederateHandle]core.FederateHandle) (json.RawMessage, error) {
	var s javaState
	_ = json.Unmarshal(e.State, &s)
	for i := range s.Deleted {
		s.Deleted[i].Sender = uint64(mapping[core.FederateHandle(s.Deleted[i].Sender)])
	}
	for i, raw := range s.Callbacks {
		wire, err := parseCallback(raw, e, false)
		if err != nil {
			return nil, err
		}
		if err := rewriteMessage(wire.ProtoReflect(), mapping); err != nil {
			return nil, err
		}
		s.Callbacks[i], err = marshalCallback(wire)
		if err != nil {
			return nil, err
		}
	}
	// Preserve presence of the required v2 empty outcome array, as well as
	// legacy absence. Struct omitempty would erase negotiated capability.
	var state map[string]json.RawMessage
	_ = json.Unmarshal(e.State, &state)
	state["deleted_objects"], _ = json.Marshal(s.Deleted)
	state["pending_callbacks"], _ = json.Marshal(s.Callbacks)
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return rewriteLocalNotifications(e, encoded, mapping)
}

func validateJavaObjects(s javaState, scalar string) error {
	objects := map[uint64]bool{}
	names := map[string]bool{}
	for _, o := range s.Objects {
		if o.Object == 0 || o.Class == 0 || objects[o.Object] || names[o.Name] {
			return errors.New("invalid Java known object")
		}
		objects[o.Object] = true
		names[o.Name] = true
	}
	removed := map[uint64]bool{}
	for _, r := range s.Removals {
		if !objects[r.Object] || removed[r.Object] {
			return errors.New("invalid Java pending removal")
		}
		removed[r.Object] = true
		if err := validateTime(r.Time, scalar); err != nil {
			return err
		}
	}
	return nil
}

func validateJavaDeletions(deleted []javaDeletion, e Envelope) error {

	designators := map[[2]uint64]bool{}
	for _, d := range deleted {
		key := [2]uint64{d.Sender, d.Retraction}
		if !validFederate(d.Sender, e.Generation) || d.Retraction == 0 || d.Object == 0 || d.Class == 0 || designators[key] {
			return errors.New("invalid Java deletion designator")
		}
		designators[key] = true
		if err := validateTime(d.Time, e.Scalar); err != nil {
			return err
		}
	}
	return nil
}

func validateJavaRegions(saved []javaRegion) error {

	regions := map[uint64]bool{}
	for _, r := range saved {
		if r.Region == 0 || regions[r.Region] || len(r.Dimensions) == 0 {
			return errors.New("invalid Java DDM region")
		}
		regions[r.Region] = true
		dimensions := map[uint64]bool{}
		for _, d := range r.Dimensions {
			if d.Dimension == 0 || dimensions[d.Dimension] || d.Lower >= d.Upper {
				return errors.New("invalid Java DDM dimension")
			}
			dimensions[d.Dimension] = true
		}
	}
	return nil
}

func validateJavaProtocol(s javaState) error {
	if s.Federation == "" || s.Label == "" || s.Policy > 31 || s.Policy&1 != 0 && s.Policy&4 != 0 || s.Policy&2 != 0 && s.Policy&8 != 0 || s.Cleared > s.Last {
		return errors.New("invalid Java checkpoint protocol state")
	}
	return nil
}
