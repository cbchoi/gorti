package sdkstate

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Local records are immutable notification data, never server receipt obligations.
type localNotification struct {
	ID      string          `json:"local_id"`
	Order   uint64          `json:"order"`
	Scope   string          `json:"scope"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}
type localObjectAttribute struct {
	Object    uint64 `json:"object_handle"`
	Attribute uint64 `json:"attribute_handle"`
}
type localOwnedAttribute struct {
	localObjectAttribute
	Owner uint64 `json:"owner_federate"`
}
type localObjectAttributes struct {
	Object     uint64   `json:"object_handle"`
	Attributes []uint64 `json:"attribute_handles"`
}
type localAttributeConfirmation struct {
	localObjectAttributes
	Transport uint32 `json:"transport_type"`
}
type localAttributeReport struct {
	localObjectAttribute
	Transport uint32 `json:"transport_type"`
}
type localInteractionConfirmation struct {
	Class     uint64 `json:"interaction_class_handle"`
	Transport uint32 `json:"transport_type"`
}
type localInteractionReport struct {
	localInteractionConfirmation
	Federate uint64 `json:"federate_handle"`
}
type localSaveStatus struct {
	Federate uint64 `json:"federate_handle"`
	Status   uint32 `json:"status"`
}
type localSaveResponse struct {
	Statuses []localSaveStatus `json:"statuses"`
	State    *uint32           `json:"state,omitempty"`
	Label    *string           `json:"active_label,omitempty"`
}
type localRestoreStatus struct {
	Pre    uint64 `json:"pre_restore_handle"`
	Post   uint64 `json:"post_restore_handle"`
	Status uint32 `json:"status"`
}
type localRestoreResponse struct {
	Statuses []localRestoreStatus `json:"statuses"`
	State    *uint32              `json:"state,omitempty"`
	Label    *string              `json:"active_label,omitempty"`
}
type localExecution struct {
	Name       string  `json:"name"`
	Factory    string  `json:"logical_time_implementation_name"`
	Mode       *uint32 `json:"mode,omitempty"`
	Joined     *uint32 `json:"federates_joined,omitempty"`
	Generation *uint64 `json:"federation_generation,omitempty"`
}
type localExecutionList struct {
	Executions []localExecution `json:"executions"`
}

func localNotifications(state json.RawMessage) ([]localNotification, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(state, &fields); err != nil {
		return nil, false, err
	}
	raw, present := fields["pending_local_notifications"]
	if !present {
		return nil, false, nil
	}
	var records []localNotification
	if err := strictDecode(raw, &records); err != nil {
		return nil, true, err
	}
	return records, true, nil
}

func validateLocalNotifications(e Envelope) error {
	records, present, err := localNotifications(e.State)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	if e.Language == "go" {
		return errors.New("Go runtime reader does not support local notification fields")
	}
	capability, _ := recoveryState(e)
	if capability != checkpointServerV2 {
		return errors.New("local notifications require server-v2 runtime")
	}
	ids := map[string]bool{}
	var previous uint64
	for _, record := range records {
		if e.Language == checkpointPython {
			switch record.Kind {
			case checkpointReportFederationExecutions, "confirmAttributeTransportationTypeChange", "confirmInteractionTransportationTypeChange", "reportAttributeTransportationType", "reportInteractionTransportationType", "federationSaveStatusResponse", checkpointFederationRestoreStatusResponse:
			default:
				return fmt.Errorf("Python runtime reader does not support local notification kind %q", record.Kind)
			}
		}
		if !canonicalInvocationIdentity(record.ID) || ids[record.ID] || record.Order == 0 || record.Order <= previous {
			return errors.New("invalid local notification identity or FIFO order")
		}
		ids[record.ID], previous = true, record.Order
		if _, err := decodeLocalPayload(record, e); err != nil {
			return err
		}
	}
	return nil
}

func rejectLocalNulls(raw json.RawMessage) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	var visit func(any) error
	visit = func(value any) error {
		switch v := value.(type) {
		case nil:
			return errors.New("null local notification field")
		case map[string]any:
			for _, item := range v {
				if err := visit(item); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range v {
				if err := visit(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(value)
}

func decodeLocalPayload(record localNotification, e Envelope) (any, error) {
	if record.Scope != "member" && record.Scope != checkpointConnection {
		return nil, errors.New("invalid local notification scope")
	}
	if (record.Kind == checkpointReportFederationExecutions) != (record.Scope == checkpointConnection) {
		return nil, errors.New("local notification kind/scope mismatch")
	}
	if err := rejectLocalNulls(record.Payload); err != nil {
		return nil, err
	}
	payload, err := newLocalPayload(record.Kind)
	if err != nil {
		return nil, err
	}
	if err := strictDecode(record.Payload, payload); err != nil {
		return nil, err
	}
	err = validateLocalPayload(payload)
	if err != nil {
		return nil, err
	}
	if err := visitLocalFederates(payload, func(value *uint64) error {
		if !validFederate(*value, e.Generation) {
			return errors.New("local notification live federate reference belongs to another execution")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return payload, nil
}

// Historical post handles and execution-list generation metadata are not visited.
func visitLocalFederates(payload any, visit func(*uint64) error) error {
	switch p := payload.(type) {
	case *localOwnedAttribute:
		return visit(&p.Owner)
	case *localInteractionReport:
		return visit(&p.Federate)
	case *localSaveResponse:
		for i := range p.Statuses {
			if err := visit(&p.Statuses[i].Federate); err != nil {
				return err
			}
		}
	case *localRestoreResponse:
		for i := range p.Statuses {
			if err := visit(&p.Statuses[i].Pre); err != nil {
				return err
			}
		}
	}
	return nil
}

func localReferences(e Envelope) ([]core.FederateHandle, error) {
	records, _, err := localNotifications(e.State)
	if err != nil {
		return nil, err
	}
	var refs []core.FederateHandle
	for _, record := range records {
		payload, err := decodeLocalPayload(record, e)
		if err != nil {
			return nil, err
		}
		_ = visitLocalFederates(payload, func(value *uint64) error { refs = append(refs, core.FederateHandle(*value)); return nil })
	}
	return refs, nil
}

func rewriteLocalNotifications(e Envelope, state json.RawMessage, mapping map[core.FederateHandle]core.FederateHandle) (json.RawMessage, error) {
	records, present, err := localNotifications(state)
	if err != nil || !present {
		return state, err
	}
	for i, record := range records {
		payload, err := decodeLocalPayload(record, e)
		if err != nil {
			return nil, err
		}
		if err := visitLocalFederates(payload, func(value *uint64) error {
			target, ok := mapping[core.FederateHandle(*value)]
			if !ok {
				return errors.New("missing local notification federate mapping")
			}
			*value = uint64(target)
			return nil
		}); err != nil {
			return nil, err
		}
		records[i].Payload, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(state, &fields)
	fields["pending_local_notifications"], err = json.Marshal(records)
	if err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

func newLocalPayload(kind string) (any, error) {
	switch kind {
	case checkpointInformAttributeOwnership:
		return &localOwnedAttribute{}, nil
	case "attributeIsNotOwned", "attributeIsOwnedByRTI":
		return &localObjectAttribute{}, nil
	case "confirmAttributeOwnershipAcquisitionCancellation":
		return &localObjectAttributes{}, nil
	case "confirmAttributeTransportationTypeChange":
		return &localAttributeConfirmation{}, nil
	case "reportAttributeTransportationType":
		return &localAttributeReport{}, nil
	case "confirmInteractionTransportationTypeChange":
		return &localInteractionConfirmation{}, nil
	case "reportInteractionTransportationType":
		return &localInteractionReport{}, nil
	case "federationSaveStatusResponse":
		return &localSaveResponse{}, nil
	case checkpointFederationRestoreStatusResponse:
		return &localRestoreResponse{}, nil
	case checkpointReportFederationExecutions:
		return &localExecutionList{}, nil
	default:
		return nil, fmt.Errorf("unsupported local notification kind %q", kind)
	}
}

func validateLocalPayload(payload any) error {

	var err error
	switch p := payload.(type) {
	case *localObjectAttribute:
		err = validateLocalObjectAttribute(*p)
	case *localOwnedAttribute:
		err = validateLocalObjectAttribute(p.localObjectAttribute)
	case *localObjectAttributes:
		err = validateLocalObjectAttributes(*p)
	case *localAttributeConfirmation:
		err = validateLocalObjectAttributes(p.localObjectAttributes)
		if err == nil {
			err = validateLocalTransport(p.Transport)
		}
	case *localAttributeReport:
		err = validateLocalObjectAttribute(p.localObjectAttribute)
		if err == nil {
			err = validateLocalTransport(p.Transport)
		}
	case *localInteractionConfirmation:
		err = validateLocalInteraction(*p)
	case *localInteractionReport:
		err = validateLocalInteraction(p.localInteractionConfirmation)
	case *localSaveResponse:
		return validateLocalSaveResponse(p)
	case *localRestoreResponse:
		return validateLocalRestoreResponse(p)
	case *localExecutionList:
		return validateLocalExecutions(p)
	}
	return err
}

func validateLocalObjectAttribute(p localObjectAttribute) error {
	if p.Object == 0 || p.Attribute == 0 {
		return errors.New("invalid local object/attribute handle")
	}
	return nil
}

func validateLocalObjectAttributes(p localObjectAttributes) error {
	if p.Object == 0 {
		return errors.New("invalid local object handle")
	}
	return uniqueNonzero(p.Attributes)
}

func validateLocalTransport(value uint32) error {
	if value != 1 && value != 2 {
		return errors.New("invalid local transportation type")
	}
	return nil
}

func validateLocalInteraction(p localInteractionConfirmation) error {
	if p.Class == 0 {
		return errors.New("invalid local interaction class")
	}
	return validateLocalTransport(p.Transport)
}

func validateLocalSaveResponse(p *localSaveResponse) error {
	var err error
	if p.State != nil && (*p.State < 1 || *p.State > 4) {
		err = errors.New("invalid local save state")
	}
	seen := map[uint64]bool{}
	for _, row := range p.Statuses {
		if row.Status < 1 || row.Status > 4 || seen[row.Federate] {
			err = errors.New("invalid local save status vector")
		}
		seen[row.Federate] = true
	}
	return err
}

func validateLocalRestoreResponse(p *localRestoreResponse) error {
	var err error
	if p.State != nil && (*p.State < 1 || *p.State > 5) {
		err = errors.New("invalid local restore state")
	}
	seen := map[uint64]bool{}
	for _, row := range p.Statuses {
		if row.Status < 1 || row.Status > 6 || seen[row.Pre] || row.Post != 0 && row.Post&0xffffffff == 0 {
			err = errors.New("invalid local restore status vector")
		}
		seen[row.Pre] = true
	}
	return err
}

func validateLocalExecutions(p *localExecutionList) error {
	var err error
	seen := map[string]bool{}
	for _, row := range p.Executions {
		if row.Name == "" || row.Factory == "" || seen[row.Name] || row.Mode != nil && *row.Mode > 2 {
			err = errors.New("invalid local federation execution information")
		}
		seen[row.Name] = true
	}
	return err
}

const checkpointConnection = "connection"

const checkpointInformAttributeOwnership = "informAttributeOwnership"

const checkpointReportFederationExecutions = "reportFederationExecutions"

const checkpointServerV2 = "server-v2"

const checkpointFederationRestoreStatusResponse = "federationRestoreStatusResponse"
