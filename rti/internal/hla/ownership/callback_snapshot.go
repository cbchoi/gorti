package ownership

import (
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type CallbackSessionState struct {
	Recipient            core.FederateHandle
	FederationGeneration uint64
	CallbackEpoch        uint64
	Enabled              bool
	LastCallbackID       uint64
	CompletedThrough     uint64
	CompletedIDs         []uint64
}

type TerminalCallbackState struct {
	Recipient          core.FederateHandle
	CallbackID         uint64
	Kind               CallbackKind
	Object             core.ObjectHandle
	Attributes         []core.AttributeHandle
	Tag                []byte
	Federate           core.FederateHandle
	RetainedProjection *CallbackProjection `json:"retainedProjection,omitempty"`
}

func exportCallbackStateLocked(st *federationState, out *ManagerState) {
	for recipient, session := range st.callbackSessions {
		if !session.enabled && len(session.pending) == 0 {
			continue
		}
		state := CallbackSessionState{Recipient: recipient, FederationGeneration: session.scope.FederationGeneration, CallbackEpoch: session.scope.CallbackEpoch, Enabled: session.enabled, LastCallbackID: session.lastID, CompletedThrough: session.completedThrough}
		for id := range session.completed {
			state.CompletedIDs = append(state.CompletedIDs, id)
		}
		slices.Sort(state.CompletedIDs)
		out.CallbackSessions = append(out.CallbackSessions, state)
		for id, entry := range session.pending {
			event := entry.event
			out.PendingCallbacks = append(out.PendingCallbacks, TerminalCallbackState{Recipient: recipient, CallbackID: id, Kind: event.Kind, Object: event.Object, Attributes: slices.Clone(event.Attributes), Tag: slices.Clone(event.Tag), Federate: event.Federate, RetainedProjection: cloneCallbackProjection(event.RetainedProjection)})
		}
	}
	slices.SortFunc(out.CallbackSessions, func(a, b CallbackSessionState) int { return compareUint64(uint64(a.Recipient), uint64(b.Recipient)) })
	slices.SortFunc(out.PendingCallbacks, func(a, b TerminalCallbackState) int {
		if a.Recipient != b.Recipient {
			return compareUint64(uint64(a.Recipient), uint64(b.Recipient))
		}
		return compareUint64(a.CallbackID, b.CallbackID)
	})
}

// The parent savebundle decoder calls this before publishing restored state.
// Legacy snapshots with neither new collection remain valid.
func ValidateOwnershipCallbackState(state *ManagerState) error {
	if state == nil {
		return nil
	}
	if uint64(len(state.CallbackSessions)) > MaxCallbackStateCount || uint64(len(state.PendingCallbacks)) > MaxCallbackStateCount {
		return fmt.Errorf("ownership callback snapshot count exceeds limit")
	}
	sessions := make(map[core.FederateHandle]CallbackSessionState, len(state.CallbackSessions))
	ids := make(map[core.FederateHandle]map[uint64]bool, len(state.CallbackSessions))
	if err := validateCallbackSessions(state.CallbackSessions, sessions, ids); err != nil {
		return err
	}
	for _, callback := range state.PendingCallbacks {
		session, exists := sessions[callback.Recipient]
		seen := ids[callback.Recipient]
		if err := validatePendingCallback(callback, session, exists, seen); err != nil {
			return err
		}
		seen[callback.CallbackID] = true
	}
	for recipient, session := range sessions {
		if uint64(len(ids[recipient])) != session.LastCallbackID-session.CompletedThrough {
			return fmt.Errorf("ownership callback identity window has holes for %d", recipient)
		}
	}
	return nil
}

func validateCallbackSessions(entries []CallbackSessionState, sessions map[core.FederateHandle]CallbackSessionState, ids map[core.FederateHandle]map[uint64]bool) error {
	var outstanding uint64
	for _, session := range entries {
		if session.Recipient == 0 || session.CompletedThrough > session.LastCallbackID || session.LastCallbackID-session.CompletedThrough > MaxCallbackStateCount || uint64(len(session.CompletedIDs)) > MaxCallbackStateCount || (session.Enabled && session.FederationGeneration == 0) {
			return fmt.Errorf("invalid ownership callback session for %d", session.Recipient)
		}
		outstanding += session.LastCallbackID - session.CompletedThrough
		if outstanding > MaxCallbackStateCount {
			return fmt.Errorf("ownership callback snapshot window exceeds limit")
		}
		if _, duplicate := sessions[session.Recipient]; duplicate {
			return fmt.Errorf("duplicate ownership callback recipient %d", session.Recipient)
		}
		sessions[session.Recipient] = session
		seen := make(map[uint64]bool, len(session.CompletedIDs))
		for _, id := range session.CompletedIDs {
			if id <= session.CompletedThrough || id > session.LastCallbackID || seen[id] {
				return fmt.Errorf("invalid completed ownership callback %d", id)
			}
			seen[id] = true
		}
		ids[session.Recipient] = seen
	}
	return nil
}

func validatePendingCallback(callback TerminalCallbackState, session CallbackSessionState, exists bool, seen map[uint64]bool) error {
	if !exists || callback.Object == 0 || callback.CallbackID <= session.CompletedThrough || callback.CallbackID > session.LastCallbackID || seen[callback.CallbackID] || len(callback.Attributes) == 0 || uint64(len(callback.Attributes)) > MaxCallbackStateCount {
		return fmt.Errorf("invalid pending ownership callback %d", callback.CallbackID)
	}
	if err := validateTerminalCallbackKind(callback, session); err != nil {
		return err
	}
	return validateCallbackAttributes(callback)
}

func validateTerminalCallbackKind(callback TerminalCallbackState, session CallbackSessionState) error {
	if callback.Kind != CallbackAcquisitionNotification && callback.Kind != CallbackUnavailable && callback.Kind != CallbackAcquisitionCancellation {
		return fmt.Errorf("invalid terminal ownership callback kind %d", callback.Kind)
	}
	if callback.Kind == CallbackAcquisitionNotification && callback.Federate != callback.Recipient {
		return fmt.Errorf("ownership callback acquirer does not match recipient")
	}
	if callback.Kind != CallbackAcquisitionNotification && callback.Federate != 0 {
		return fmt.Errorf("invalid terminal ownership callback federate")
	}
	if callback.Kind == CallbackAcquisitionCancellation && !session.Enabled {
		return fmt.Errorf("legacy recipient cannot have new cancellation callback")
	}
	return nil
}

func validateCallbackAttributes(callback TerminalCallbackState) error {
	attrs := make(map[core.AttributeHandle]bool, len(callback.Attributes))
	if p := callback.RetainedProjection; p != nil {
		if p.RegisteredClass == 0 || p.KnownClass == 0 || len(p.Attributes) != len(callback.Attributes) {
			return fmt.Errorf("invalid retained ownership callback projection")
		}
		seen := make(map[core.AttributeHandle]bool, len(p.Attributes))
		for _, attr := range p.Attributes {
			if attr == 0 || seen[attr] {
				return fmt.Errorf("invalid retained ownership callback attribute")
			}
			seen[attr] = true
		}
	}
	for _, attr := range callback.Attributes {
		if attr == 0 || attrs[attr] {
			return fmt.Errorf("invalid ownership callback attribute %d", attr)
		}
		attrs[attr] = true
	}
	return nil
}

func importCallbackStateLocked(st *federationState, fed core.FederationName, state *ManagerState) {
	for _, value := range state.CallbackSessions {
		session := st.callbackSessionLocked(fed, value.Recipient)
		session.scope.FederationGeneration, session.scope.CallbackEpoch = value.FederationGeneration, value.CallbackEpoch
		session.enabled, session.restoreRequired = value.Enabled, value.Enabled
		session.lastID, session.completedThrough = value.LastCallbackID, value.CompletedThrough
		for _, id := range value.CompletedIDs {
			session.completed[id] = struct{}{}
		}
	}
	for _, value := range state.PendingCallbacks {
		session := st.callbackSessions[value.Recipient]
		event := newCallback(value.Kind, value.Object, value.Attributes, value.Tag, value.Federate)
		event.RetainedProjection = cloneCallbackProjection(value.RetainedProjection)
		event.CallbackID, event.CallbackScope, event.RequireInvocationReceipt = value.CallbackID, session.scope, session.enabled
		*event.identity = callbackIdentity{scope: session.scope, id: value.CallbackID, strict: session.enabled}
		session.pending[value.CallbackID] = &terminalCallback{event: event}
	}
}

func cloneCallbackProjection(p *CallbackProjection) *CallbackProjection {
	if p == nil {
		return nil
	}
	cloned := *p
	cloned.Attributes = slices.Clone(p.Attributes)
	return &cloned
}
