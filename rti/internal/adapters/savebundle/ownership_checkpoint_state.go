package savebundle

import (
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

// ReadOwnershipCheckpointState uses the same decoder and legacy confirmation
// interpretation as live restore, without publishing any manager state.
func ReadOwnershipCheckpointState(data []byte) (*ownership.ManagerState, error) {
	wire, history, offers, sessions, callbacks, err := decodeOwnershipSnapshotComplete(data)
	if err != nil {
		return nil, err
	}
	state := &ownership.ManagerState{UnownedOffers: offers, CallbackSessions: sessions, PendingCallbacks: callbacks}
	legacyAcquirers := make(map[[2]uint64]bool)
	if history == nil {
		for _, entry := range wire.PendingAcquires {
			if entry != nil {
				legacyAcquirers[[2]uint64{entry.ObjectHandle, entry.AttributeHandle}] = true
			}
		}
	}
	for _, entry := range wire.Owners {
		if entry != nil {
			state.Owners = append(state.Owners, ownership.OwnerState{Object: core.ObjectHandle(entry.GetObjectHandle()), Attribute: core.AttributeHandle(entry.GetAttributeHandle()), Owner: core.FederateHandle(entry.GetOwnerFederate())})
		}
	}
	for i, entry := range wire.PendingDivests {
		if entry != nil {
			requested := entry.TwoPhase && legacyAcquirers[[2]uint64{entry.ObjectHandle, entry.AttributeHandle}]
			if history != nil {
				requested = history[i]
			}
			state.PendingDivests = append(state.PendingDivests, ownership.PendingDivestState{Object: core.ObjectHandle(entry.GetObjectHandle()), Attribute: core.AttributeHandle(entry.GetAttributeHandle()), Owner: core.FederateHandle(entry.GetOwnerFederate()), Tag: append([]byte(nil), entry.GetTag()...), TwoPhase: entry.GetTwoPhase(), ConfirmationRequested: requested})
		}
	}
	for _, entry := range wire.PendingAcquires {
		if entry != nil {
			state.PendingAcquires = append(state.PendingAcquires, ownership.PendingAcquireState{Object: core.ObjectHandle(entry.GetObjectHandle()), Attribute: core.AttributeHandle(entry.GetAttributeHandle()), Acquirer: core.FederateHandle(entry.GetAcquirerFederate()), Tag: append([]byte(nil), entry.GetTag()...)})
		}
	}
	return state, nil
}
