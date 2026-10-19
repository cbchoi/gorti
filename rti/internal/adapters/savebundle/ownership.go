package savebundle

import (
	"bytes"
	"encoding/json"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"google.golang.org/protobuf/proto"
)

type Ownership struct{ Manager *ownership.Manager }

// OwnershipCallbackScopes returns saved enabled sessions without restoring state.
// Federation is absent from the snapshot; callers supply it during rebinding.
func OwnershipCallbackScopes(data []byte) (map[core.FederateHandle]core.OwnershipCallbackScope, error) {
	_, _, _, sessions, _, err := decodeOwnershipSnapshotComplete(data)
	if err != nil {
		return nil, fmt.Errorf("ownership callback scopes: %w", err)
	}
	scopes := make(map[core.FederateHandle]core.OwnershipCallbackScope, len(sessions))
	for _, session := range sessions {
		if session.Enabled {
			scopes[session.Recipient] = core.OwnershipCallbackScope{
				Recipient:            session.Recipient,
				FederationGeneration: session.FederationGeneration,
				CallbackEpoch:        session.CallbackEpoch,
			}
		}
	}
	return scopes, nil
}

// Like MOM extensions, the zero tag makes old protobuf readers reject new state.
const ownershipSnapshotMagic = "\x00gorti.ownership\x00"

type ownershipSnapshotEnvelope struct {
	Version               uint32                            `json:"version"`
	LegacyProtoBytes      []byte                            `json:"legacyProtoBytes"`
	ConfirmationRequested []bool                            `json:"confirmationRequested"`
	UnownedOffers         []ownership.UnownedOfferState     `json:"unownedOffers,omitempty"`
	CallbackSessions      []ownership.CallbackSessionState  `json:"callbackSessions,omitempty"`
	PendingCallbacks      []ownership.TerminalCallbackState `json:"pendingCallbacks,omitempty"`
}

func marshalOwnershipSnapshot(wire *rtiv1.OwnershipManagerState, history []bool, pending ...[]ownership.UnownedOfferState) ([]byte, error) {
	var offers []ownership.UnownedOfferState
	if len(pending) != 0 {
		offers = pending[0]
	}
	base, err := (proto.MarshalOptions{Deterministic: true}).Marshal(wire)
	if err != nil || (history == nil && len(offers) == 0) {
		return base, err
	}
	version := uint32(1)
	if len(offers) != 0 {
		version = 2
		if history == nil {
			history = []bool{}
		}
	}
	extension, err := json.Marshal(ownershipSnapshotEnvelope{Version: version, LegacyProtoBytes: base, ConfirmationRequested: history, UnownedOffers: offers})
	return append([]byte(ownershipSnapshotMagic), extension...), err
}

func decodeOwnershipSnapshot(data []byte) (*rtiv1.OwnershipManagerState, []bool, error) {
	wire, history, offers, err := decodeOwnershipSnapshotState(data)
	if err == nil && len(offers) != 0 {
		return nil, nil, fmt.Errorf("ownership continuation requires the full-state snapshot decoder")
	}
	return wire, history, err
}

func decodeOwnershipSnapshotState(data []byte) (*rtiv1.OwnershipManagerState, []bool, []ownership.UnownedOfferState, error) {
	var history []bool
	var offers []ownership.UnownedOfferState
	extended := bytes.HasPrefix(data, []byte(ownershipSnapshotMagic))
	if extended {
		var envelope ownershipSnapshotEnvelope
		if err := decodeSnapshotJSON(data[len(ownershipSnapshotMagic):], &envelope); err != nil {
			return nil, nil, nil, err
		}
		if err := validateOwnershipExtension(envelope); err != nil {
			return nil, nil, nil, err
		}
		data, history = envelope.LegacyProtoBytes, envelope.ConfirmationRequested
		offers = envelope.UnownedOffers
	}
	wire := &rtiv1.OwnershipManagerState{}
	if err := proto.Unmarshal(data, wire); err != nil {
		return nil, nil, nil, err
	}
	if extended {
		if len(history) != len(wire.PendingDivests) {
			return nil, nil, nil, fmt.Errorf("ownership confirmation history count differs")
		}
		for i, requested := range history {
			if wire.PendingDivests[i] == nil || (requested && !wire.PendingDivests[i].TwoPhase) {
				return nil, nil, nil, fmt.Errorf("ownership confirmation without a two-phase divest")
			}
		}
	}
	if err := validateUnownedOffers(wire, offers); err != nil {
		return nil, nil, nil, err
	}
	return wire, history, offers, nil
}

func (s Ownership) Marshal(fed core.FederationName) ([]byte, error) {
	state := s.Manager.ExportState(fed)
	if state == nil {
		return nil, nil
	}
	wire := &rtiv1.OwnershipManagerState{}
	var history []bool
	for _, entry := range state.Owners {
		wire.Owners = append(wire.Owners, &rtiv1.OwnershipRecordEntry{ObjectHandle: uint64(entry.Object), AttributeHandle: uint64(entry.Attribute), OwnerFederate: uint64(entry.Owner)})
	}
	for _, entry := range state.PendingDivests {
		wire.PendingDivests = append(wire.PendingDivests, &rtiv1.PendingDivestEntry{ObjectHandle: uint64(entry.Object), AttributeHandle: uint64(entry.Attribute), OwnerFederate: uint64(entry.Owner), Tag: append([]byte(nil), entry.Tag...), TwoPhase: entry.TwoPhase})
		history = append(history, entry.ConfirmationRequested)
	}
	for _, entry := range state.PendingAcquires {
		wire.PendingAcquires = append(wire.PendingAcquires, &rtiv1.PendingAcquireEntry{ObjectHandle: uint64(entry.Object), AttributeHandle: uint64(entry.Attribute), AcquirerFederate: uint64(entry.Acquirer), Tag: append([]byte(nil), entry.Tag...)})
	}
	return marshalOwnershipSnapshotComplete(wire, history, state.UnownedOffers, state.CallbackSessions, state.PendingCallbacks)
}

func (s Ownership) Unmarshal(fed core.FederationName, data []byte) error {
	// Preserve the legacy direct-call contract; the manifest-aware path uses
	// PrepareRestore, where present empty bytes mean an empty saved state.
	if len(data) == 0 {
		return nil
	}
	commit, err := s.PrepareRestore(fed, data)
	if err != nil {
		return err
	}
	if commit != nil {
		commit()
	}
	return nil
}

// PrepareRestore validates the complete snapshot before any live mutation.
func (s Ownership) PrepareRestore(fed core.FederationName, data []byte) (func(), error) {
	state, err := ReadOwnershipCheckpointState(data)
	if err != nil {
		return nil, fmt.Errorf("ownership snapshot for %q: %w", fed, err)
	}
	return s.Manager.PrepareImportState(fed, state)
}

func marshalOwnershipSnapshotComplete(wire *rtiv1.OwnershipManagerState, history []bool, offers []ownership.UnownedOfferState, sessions []ownership.CallbackSessionState, callbacks []ownership.TerminalCallbackState) ([]byte, error) {
	if len(sessions) == 0 && len(callbacks) == 0 {
		return marshalOwnershipSnapshot(wire, history, offers)
	}
	if err := ownership.ValidateOwnershipCallbackState(&ownership.ManagerState{CallbackSessions: sessions, PendingCallbacks: callbacks}); err != nil {
		return nil, err
	}
	base, err := (proto.MarshalOptions{Deterministic: true}).Marshal(wire)
	if err != nil {
		return nil, err
	}
	if history == nil {
		history = []bool{}
	}
	version := uint32(3)
	for _, callback := range callbacks {
		if callback.RetainedProjection != nil {
			version = 4
			break
		}
	}
	data, err := json.Marshal(ownershipSnapshotEnvelope{Version: version, LegacyProtoBytes: base, ConfirmationRequested: history, UnownedOffers: offers, CallbackSessions: sessions, PendingCallbacks: callbacks})
	return append([]byte(ownershipSnapshotMagic), data...), err
}

func decodeOwnershipSnapshotComplete(data []byte) (*rtiv1.OwnershipManagerState, []bool, []ownership.UnownedOfferState, []ownership.CallbackSessionState, []ownership.TerminalCallbackState, error) {
	if !bytes.HasPrefix(data, []byte(ownershipSnapshotMagic)) {
		wire, history, offers, err := decodeOwnershipSnapshotState(data)
		return wire, history, offers, nil, nil, err
	}
	var envelope ownershipSnapshotEnvelope
	if err := decodeSnapshotJSON(data[len(ownershipSnapshotMagic):], &envelope); err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if envelope.Version != 3 && envelope.Version != 4 {
		wire, history, offers, err := decodeOwnershipSnapshotState(data)
		return wire, history, offers, nil, nil, err
	}
	if envelope.Version == 3 {
		for _, callback := range envelope.PendingCallbacks {
			if callback.RetainedProjection != nil {
				return nil, nil, nil, nil, nil, fmt.Errorf("retained ownership projection requires snapshot version 4")
			}
		}
	}
	if envelope.ConfirmationRequested == nil || len(envelope.CallbackSessions) == 0 {
		return nil, nil, nil, nil, nil, fmt.Errorf("incomplete ownership callback snapshot")
	}
	if err := ownership.ValidateOwnershipCallbackState(&ownership.ManagerState{CallbackSessions: envelope.CallbackSessions, PendingCallbacks: envelope.PendingCallbacks}); err != nil {
		return nil, nil, nil, nil, nil, err
	}
	wire, history, offers, err := decodeOwnershipCallbackBase(envelope)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	return wire, history, offers, envelope.CallbackSessions, envelope.PendingCallbacks, nil
}

func validateOwnershipExtension(envelope ownershipSnapshotEnvelope) error {
	if (envelope.Version != 1 && envelope.Version != 2) || envelope.ConfirmationRequested == nil ||
		(envelope.Version == 1 && (len(envelope.LegacyProtoBytes) == 0 || len(envelope.UnownedOffers) != 0)) ||
		(envelope.Version == 2 && len(envelope.UnownedOffers) == 0) || envelope.CallbackSessions != nil || envelope.PendingCallbacks != nil {
		return fmt.Errorf("unsupported or incomplete ownership snapshot extension")
	}
	return nil
}

func validateUnownedOffers(wire *rtiv1.OwnershipManagerState, offers []ownership.UnownedOfferState) error {
	seen := make(map[[2]uint64]bool, len(offers))
	for _, offer := range offers {
		key := [2]uint64{uint64(offer.Object), uint64(offer.Attribute)}
		if offer.Object == 0 || offer.Attribute == 0 || offer.Divester == ^core.FederateHandle(0) || seen[key] {
			return fmt.Errorf("invalid or duplicate unowned offer")
		}
		seen[key] = true
	}
	for _, owner := range wire.Owners {
		if owner != nil && seen[[2]uint64{owner.ObjectHandle, owner.AttributeHandle}] {
			return fmt.Errorf("unowned offer has an ownership record")
		}
	}
	for _, pending := range wire.PendingDivests {
		if pending != nil && seen[[2]uint64{pending.ObjectHandle, pending.AttributeHandle}] {
			return fmt.Errorf("unowned offer also has a negotiated divest")
		}
	}
	return nil
}

func decodeOwnershipCallbackBase(envelope ownershipSnapshotEnvelope) (*rtiv1.OwnershipManagerState, []bool, []ownership.UnownedOfferState, error) {
	legacy := envelope
	legacy.CallbackSessions = nil
	legacy.PendingCallbacks = nil
	legacy.Version = 1
	if len(legacy.UnownedOffers) > 0 {
		legacy.Version = 2
	}
	// A protobuf with no ownership records is a valid empty state. The historical
	// extension decoder requires a nonempty base in v1, so validate the empty base
	// through its plain-protobuf path instead of inventing ownership records.
	var wire *rtiv1.OwnershipManagerState
	var history []bool
	var offers []ownership.UnownedOfferState
	var err error
	if len(legacy.LegacyProtoBytes) == 0 && len(legacy.UnownedOffers) == 0 {
		wire = &rtiv1.OwnershipManagerState{}
		history = legacy.ConfirmationRequested
		if len(history) != 0 {
			return nil, nil, nil, fmt.Errorf("ownership confirmation history count differs")
		}
	} else {
		encoded, marshalErr := json.Marshal(legacy)
		if marshalErr != nil {
			return nil, nil, nil, marshalErr
		}
		wire, history, offers, err = decodeOwnershipSnapshotState(append([]byte(ownershipSnapshotMagic), encoded...))
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return wire, history, offers, nil
}
