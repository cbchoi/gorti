package grpc

import (
	"bytes"
	"fmt"
	"slices"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"google.golang.org/protobuf/proto"
)

// OwnershipCheckpointBinding is saved beside the general callback's stable
// identity, body and phase. An ID is a domain relationship, not a capability.
type OwnershipCheckpointBinding struct {
	CallbackID uint64 `json:"callback_id"`
}

type ownershipCheckpointStateSource interface {
	ExportState(core.FederationName) *ownership.ManagerState
}

func CaptureOwnershipCheckpointBinding(state *ownership.ManagerState, recipient core.FederateHandle, event *rtiv1.FederateEvent, outcomeOnly bool, retained ...*OwnershipCheckpointBinding) (*OwnershipCheckpointBinding, error) {
	var binding *OwnershipCheckpointBinding
	if len(retained) > 1 {
		return nil, fmt.Errorf("ownership checkpoint: ambiguous retained binding")
	}
	if len(retained) == 1 && retained[0] != nil {
		binding = &OwnershipCheckpointBinding{CallbackID: retained[0].CallbackID}
		if recovery := event.GetCallbackRecovery(); recovery == nil || len(recovery.GetInvocationIdentity()) != 32 || recovery.GetCompleted() {
			return nil, fmt.Errorf("ownership checkpoint: retained binding requires recovered identity")
		}
	}
	if wire := event.GetOwnershipReceipt(); wire != nil {
		if binding != nil && binding.CallbackID != wire.GetCallbackId() {
			return nil, fmt.Errorf("ownership checkpoint: changed retained binding")
		}
		binding = &OwnershipCheckpointBinding{CallbackID: wire.GetCallbackId()}
		scopeFound := ownershipCheckpointScopeMatches(state, recipient, event, wire)
		if !scopeFound {
			return nil, fmt.Errorf("ownership checkpoint: source scope mismatch")
		}
	}
	if _, err := validateOwnershipCheckpointBinding(state, recipient, event, binding, outcomeOnly); err != nil {
		return nil, err
	}
	return binding, nil
}

// PrepareOwnershipCheckpointEvent only relates the portable body to saved
// ownership state. The parent must first validate the general identity/phase
// and a matched completed SDK outcome; ownership completion cannot infer it.
func PrepareOwnershipCheckpointEvent(state *ownership.ManagerState, recipient core.FederateHandle, event *rtiv1.FederateEvent, binding *OwnershipCheckpointBinding, outcomeOnly bool) (*rtiv1.FederateEvent, error) {
	if event == nil || len(event.GetCallbackReceipt()) != 0 || event.GetOwnershipReceipt() != nil || hasUnknownCallbackFields(event.ProtoReflect()) {
		return nil, fmt.Errorf("ownership checkpoint: missing body or persisted capability")
	}
	completed, err := validateOwnershipCheckpointBinding(state, recipient, event, binding, outcomeOnly)
	if err != nil {
		return nil, err
	}
	replay := proto.Clone(event).(*rtiv1.FederateEvent)
	if binding != nil && !completed {
		// Zero scope/attempt deliberately has no live authorization. Prepare must
		// resolve the current scope after the restore transition commits.
		replay.OwnershipReceipt = &rtiv1.OwnershipCallbackReceipt{CallbackId: binding.CallbackID}
	}
	return replay, nil
}

func validateOwnershipCheckpointBinding(state *ownership.ManagerState, recipient core.FederateHandle, event *rtiv1.FederateEvent, binding *OwnershipCheckpointBinding, outcomeOnly bool) (bool, error) {
	if recipient == 0 || event == nil {
		return false, fmt.Errorf("ownership checkpoint: missing recipient or body")
	}
	if err := ownership.ValidateOwnershipCallbackState(state); err != nil {
		return false, err
	}
	kind, object, attrs, tag, owner := ownershipCheckpointBody(event)
	session := ownershipCheckpointSession(state, recipient)
	if binding == nil {
		if kind != 0 && session != nil && session.Enabled {
			return false, fmt.Errorf("ownership checkpoint: strict terminal callback missing binding")
		}
		return false, nil
	}
	if err := validateOwnershipCheckpointTerminal(kind, object, attrs, owner, recipient, session, binding); err != nil {
		return false, err
	}
	completed := binding.CallbackID <= session.CompletedThrough || slices.Contains(session.CompletedIDs, binding.CallbackID)
	if completed {
		if !outcomeOnly {
			return false, fmt.Errorf("ownership checkpoint: completed terminal cannot replay body")
		}
		return true, nil
	}
	return false, validateOwnershipCheckpointObligation(state, recipient, binding, kind, object, attrs, tag, owner)
}

func ownershipCheckpointBody(event *rtiv1.FederateEvent) (ownership.CallbackKind, uint64, []uint64, []byte, uint64) {
	switch {
	case event.GetOwnershipAcquired() != nil:
		body := event.GetOwnershipAcquired()
		return ownership.CallbackAcquisitionNotification, body.GetObjectHandle(), body.GetAttributeHandles(), body.GetTag(), body.GetOwningFederate()
	case event.GetOwnershipUnavailable() != nil:
		body := event.GetOwnershipUnavailable()
		return ownership.CallbackUnavailable, body.GetObjectHandle(), body.GetAttributeHandles(), nil, 0
	case event.GetOwnershipAcquisitionCancelled() != nil:
		body := event.GetOwnershipAcquisitionCancelled()
		return ownership.CallbackAcquisitionCancellation, body.GetObjectHandle(), body.GetAttributeHandles(), nil, 0
	default:
		return 0, 0, nil, nil, 0
	}
}
func ownershipCheckpointScopeMatches(state *ownership.ManagerState, recipient core.FederateHandle, event *rtiv1.FederateEvent, wire *rtiv1.OwnershipCallbackReceipt) bool {
	var scopeFound bool
	// A replay row is captured before the stream assigns current authority.
	// Its stable general identity survives another save without old scope.
	if recovery := event.GetCallbackRecovery(); recovery != nil && len(recovery.GetInvocationIdentity()) == 32 && !recovery.GetCompleted() && wire.GetFederationGeneration() == 0 && wire.GetCallbackEpoch() == 0 && wire.GetDeliveryAttempt() == 0 {
		scopeFound = true
	}
	if state != nil {
		for _, session := range state.CallbackSessions {
			if session.Recipient == recipient && session.Enabled && wire.GetFederationGeneration() == session.FederationGeneration && wire.GetCallbackEpoch() == session.CallbackEpoch {
				scopeFound = true
			}
		}
	}
	return scopeFound
}

func ownershipCheckpointSession(state *ownership.ManagerState, recipient core.FederateHandle) *ownership.CallbackSessionState {
	var session *ownership.CallbackSessionState
	if state != nil {
		for i := range state.CallbackSessions {
			if state.CallbackSessions[i].Recipient == recipient {
				session = &state.CallbackSessions[i]
				break
			}
		}
	}
	return session
}

func validateOwnershipCheckpointTerminal(kind ownership.CallbackKind, object uint64, attrs []uint64, owner uint64, recipient core.FederateHandle, session *ownership.CallbackSessionState, binding *OwnershipCheckpointBinding) error {
	if kind == 0 || object == 0 || len(attrs) == 0 || session == nil || !session.Enabled || binding.CallbackID == 0 || binding.CallbackID > session.LastCallbackID {
		return fmt.Errorf("ownership checkpoint: invalid terminal relationship")
	}
	if kind == ownership.CallbackAcquisitionNotification && owner != uint64(recipient) {
		return fmt.Errorf("ownership checkpoint: acquired owner/recipient mismatch")
	}
	for i, attr := range attrs {
		if attr == 0 || (i > 0 && attrs[i-1] >= attr) {
			return fmt.Errorf("ownership checkpoint: invalid terminal attributes")
		}
	}
	return nil
}

func validateOwnershipCheckpointObligation(state *ownership.ManagerState, recipient core.FederateHandle, binding *OwnershipCheckpointBinding, kind ownership.CallbackKind, object uint64, attrs []uint64, tag []byte, owner uint64) error {
	for _, pending := range state.PendingCallbacks {
		if pending.Recipient != recipient || pending.CallbackID != binding.CallbackID {
			continue
		}
		wantAttrs := pending.Attributes
		if pending.RetainedProjection != nil {
			wantAttrs = pending.RetainedProjection.Attributes
		}
		actual := make([]core.AttributeHandle, len(attrs))
		for i, attr := range attrs {
			actual[i] = core.AttributeHandle(attr)
		}
		if pending.Kind != kind || uint64(pending.Object) != object || !slices.Equal(actual, wantAttrs) || !bytes.Equal(tag, pending.Tag) || uint64(pending.Federate) != owner {
			return fmt.Errorf("ownership checkpoint: terminal body differs from retained obligation")
		}
		return nil
	}
	return fmt.Errorf("ownership checkpoint: missing terminal obligation")
}
