package savebundle

import (
	"encoding/json"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

// Declaration persists declaration tables independently of the RPC schema.
type Declaration struct{ Manager *declaration.Manager }

const declarationSnapshotVersion = 4

type declarationSnapshot struct {
	Version                      uint32                                            `json:"version"`
	ObjectPublications           []declarationObjectRecord                         `json:"object_publications,omitempty"`
	ObjectPublicationHistory     []declaration.ObjectPublicationHistoryState       `json:"object_publication_history"`
	ObjectSubscriptions          []declarationObjectRecord                         `json:"object_subscriptions,omitempty"`
	InteractionPublications      []declarationInteractionRecord                    `json:"interaction_publications,omitempty"`
	InteractionSubscriptions     []declarationInteractionRecord                    `json:"interaction_subscriptions,omitempty"`
	ObjectOptions                []declaration.ObjectSubscriptionOptionsState      `json:"object_options,omitempty"`
	InteractionOptions           []declaration.InteractionSubscriptionOptionsState `json:"interaction_options,omitempty"`
	AdvisorySwitches             []declaration.AdvisorySwitchState                 `json:"advisory_switches,omitempty"`
	AdvisoryDefaults             *[4]bool                                          `json:"advisory_defaults,omitempty"`
	RegionalObjectInterests      []declarationObjectRecord                         `json:"regional_object_interests,omitempty"`
	RegionalInteractionInterests []declarationInteractionRecord                    `json:"regional_interaction_interests,omitempty"`
	UpdateRateObjects            []declaration.UpdateRateObjectState               `json:"update_rate_objects,omitempty"`
	RegionalUpdateRates          []core.RegionalUpdateRateSubscription             `json:"regional_update_rates,omitempty"`
	PendingAdvisories            []declaration.PendingAdvisoryState                `json:"pending_advisories,omitempty"`
}

type declarationObjectRecord struct {
	Class     core.ObjectClassHandle `json:"class"`
	Attribute core.AttributeHandle   `json:"attribute"`
	Federates []core.FederateHandle  `json:"federates"`
}

type declarationInteractionRecord struct {
	Class     core.InteractionClassHandle `json:"class"`
	Federates []core.FederateHandle       `json:"federates"`
}

func (s Declaration) Marshal(fed core.FederationName) ([]byte, error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("declaration snapshot for %q: manager is nil", fed)
	}
	state := s.Manager.ExportState(fed)
	history := state.ObjectPublicationHistory
	if history == nil {
		history = []declaration.ObjectPublicationHistoryState{}
	}
	objects := func(entries []declaration.ObjectDeclarationState) []declarationObjectRecord {
		var records []declarationObjectRecord
		for _, entry := range entries {
			records = append(records, declarationObjectRecord{Class: entry.Class, Attribute: entry.Attribute, Federates: entry.Federates})
		}
		return records
	}
	interactions := func(entries []declaration.InteractionDeclarationState) []declarationInteractionRecord {
		var records []declarationInteractionRecord
		for _, entry := range entries {
			records = append(records, declarationInteractionRecord{Class: entry.Class, Federates: entry.Federates})
		}
		return records
	}
	return json.Marshal(declarationSnapshot{
		Version:                      declarationSnapshotVersion,
		ObjectPublications:           objects(state.ObjectPublications),
		ObjectPublicationHistory:     history,
		ObjectSubscriptions:          objects(state.ObjectSubscriptions),
		InteractionPublications:      interactions(state.InteractionPublications),
		InteractionSubscriptions:     interactions(state.InteractionSubscriptions),
		ObjectOptions:                state.ObjectOptions,
		InteractionOptions:           state.InteractionOptions,
		AdvisorySwitches:             state.AdvisorySwitches,
		AdvisoryDefaults:             state.AdvisoryDefaults,
		RegionalObjectInterests:      objects(state.RegionalObjectInterests),
		RegionalInteractionInterests: interactions(state.RegionalInteractionInterests),
		UpdateRateObjects:            state.UpdateRateObjects,
		RegionalUpdateRates:          state.RegionalUpdateRates,
		PendingAdvisories:            state.PendingAdvisories,
	})
}

func (s Declaration) Unmarshal(fed core.FederationName, data []byte) error {
	// Legacy direct callers treat absent bytes as a no-op. Manifest-aware
	// restoration uses PrepareRestore, where present empty bytes clear state.
	if len(data) == 0 {
		return nil
	}
	commit, err := s.PrepareRestore(fed, data)
	if err != nil {
		return err
	}
	commit()
	return nil
}

// PrepareRestore decodes and validates without changing live state. The caller
// must quiesce services and validate federation membership/FOM provenance.
func (s Declaration) PrepareRestore(fed core.FederationName, data []byte) (func(), error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("declaration snapshot for %q: manager is nil", fed)
	}
	state, err := decodeDeclarationState(fed, data)
	if err != nil {
		return nil, err
	}
	return s.Manager.PrepareRestore(fed, state)
}

func (s Declaration) PrepareRestoreWithFOM(fed core.FederationName, data []byte, model core.FOMHandle) (func(), error) {
	if s.Manager == nil {
		return nil, fmt.Errorf("declaration snapshot for %q: manager is nil", fed)
	}
	state, err := decodeDeclarationState(fed, data)
	if err != nil {
		return nil, err
	}
	return s.Manager.PrepareRestoreWithFOM(fed, state, model)
}

func decodeDeclarationState(fed core.FederationName, data []byte) (*declaration.ManagerState, error) {
	var snapshot declarationSnapshot
	if len(data) != 0 {
		if err := decodeSnapshotJSON(data, &snapshot); err != nil {
			return nil, fmt.Errorf("declaration snapshot for %q: %w", fed, err)
		}
		if snapshot.Version < 1 || snapshot.Version > declarationSnapshotVersion {
			return nil, fmt.Errorf("declaration snapshot for %q: unsupported version %d", fed, snapshot.Version)
		}
		if snapshot.Version >= 4 && snapshot.ObjectPublicationHistory == nil {
			return nil, fmt.Errorf("declaration snapshot for %q: missing publication history", fed)
		}
	}
	objects := func(records []declarationObjectRecord) []declaration.ObjectDeclarationState {
		var entries []declaration.ObjectDeclarationState
		for _, record := range records {
			entries = append(entries, declaration.ObjectDeclarationState{Class: record.Class, Attribute: record.Attribute, Federates: record.Federates})
		}
		return entries
	}
	interactions := func(records []declarationInteractionRecord) []declaration.InteractionDeclarationState {
		var entries []declaration.InteractionDeclarationState
		for _, record := range records {
			entries = append(entries, declaration.InteractionDeclarationState{Class: record.Class, Federates: record.Federates})
		}
		return entries
	}
	return &declaration.ManagerState{
		ObjectPublications:           objects(snapshot.ObjectPublications),
		ObjectPublicationHistory:     snapshot.ObjectPublicationHistory,
		ObjectSubscriptions:          objects(snapshot.ObjectSubscriptions),
		InteractionPublications:      interactions(snapshot.InteractionPublications),
		InteractionSubscriptions:     interactions(snapshot.InteractionSubscriptions),
		ObjectOptions:                snapshot.ObjectOptions,
		InteractionOptions:           snapshot.InteractionOptions,
		AdvisorySwitches:             snapshot.AdvisorySwitches,
		AdvisoryDefaults:             snapshot.AdvisoryDefaults,
		RegionalObjectInterests:      objects(snapshot.RegionalObjectInterests),
		RegionalInteractionInterests: interactions(snapshot.RegionalInteractionInterests),
		UpdateRateObjects:            snapshot.UpdateRateObjects,
		RegionalUpdateRates:          snapshot.RegionalUpdateRates,
		PendingAdvisories:            snapshot.PendingAdvisories,
	}, nil
}
