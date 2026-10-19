package sdkstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

type pendingOutcome struct {
	Identity  string `json:"identity"`
	Success   bool   `json:"success"`
	Exception string `json:"exception"`
}

func canonicalInvocationIdentity(identity string) bool {
	decoded, err := hex.DecodeString(identity)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == identity
}

func recoveryState(e Envelope) (string, []pendingOutcome) {
	var fields struct {
		Recovery string           `json:"callback_recovery"`
		Outcomes []pendingOutcome `json:"pending_outcomes"`
	}
	_ = json.Unmarshal(e.State, &fields)
	return fields.Recovery, fields.Outcomes
}

func stableCallbackIdentity(wire *rtiv1.FederateEvent) string {
	if metadata := wire.GetCallbackRecovery(); metadata != nil {
		return hex.EncodeToString(metadata.InvocationIdentity)
	}
	identity := sha256.Sum256(wire.CallbackReceipt)
	return hex.EncodeToString(identity[:])
}

func queuedRecoveryIdentities(e Envelope) ([]string, error) {
	capability, _ := recoveryState(e)
	if capability != checkpointServerV2 {
		return nil, nil
	}
	var projection struct {
		Callbacks []json.RawMessage `json:"pending_callbacks"`
		Data      []pendingData     `json:"pending_data"`
		Controls  []pendingData     `json:"pending_controls"`
	}
	if err := json.Unmarshal(e.State, &projection); err != nil {
		return nil, err
	}
	var identities []string
	for _, queued := range append(projection.Data, projection.Controls...) {
		identities = append(identities, queued.Identity)
	}
	for _, raw := range projection.Callbacks {
		wire, err := parseCallback(raw, e, e.Language == checkpointPython)
		if err != nil {
			return nil, err
		}
		identities = append(identities, stableCallbackIdentity(wire))
	}
	seen := map[string]bool{}
	for _, identity := range identities {
		if seen[identity] {
			return nil, errors.New("duplicate SDK queued invocation identity")
		}
		seen[identity] = true
	}
	return identities, nil
}

func validateRecoveryState(e Envelope, recovery string, outcomes []pendingOutcome) error {
	if recovery != checkpointServerV2 {
		if recovery != "server" && !(e.Language == checkpointJava && recovery == "") {
			return errors.New("unsupported SDK callback recovery authority")
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(e.State, &fields)
		if _, present := fields["pending_outcomes"]; present {
			return errors.New("legacy SDK state cannot carry v2 outcomes")
		}
		if _, present := fields["pending_controls"]; present {
			return errors.New("legacy SDK state cannot carry v2 controls")
		}
		if _, present := fields["pending_local_notifications"]; present {
			return errors.New("legacy SDK state cannot carry local notifications")
		}
		return nil
	}
	if err := requireFields(e.State, "pending_outcomes"); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, outcome := range outcomes {
		if !canonicalInvocationIdentity(outcome.Identity) || seen[outcome.Identity] {
			return errors.New("invalid or duplicate SDK completed outcome identity")
		}
		seen[outcome.Identity] = true
		if outcome.Success != (outcome.Exception == "") {
			return errors.New("SDK completed outcome has inconsistent success and exception")
		}
	}
	return validateLocalNotifications(e)
}

// ReconcileManifestVersionOf is a read-only SOURCE preflight. It must run before
// handle reservation or any manager restore preparation. Identities correlate
// known results only; they are never accepted as invocation authorization.
func ReconcileManifestVersionOf[T core.LogicalTimeScalar](ctx context.Context, manifest savepoint.ManifestOf[T], version uint32, requirements []savepoint.CallbackRecoveryRequirement) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if version != 1 && version != 2 {
		return errors.New("unsupported server callback ledger version")
	}
	if len(manifest.FederateClientStates) != 0 && len(manifest.FederateClientStates) != len(manifest.Federates) {
		return errors.New("SDK callback recovery roster mismatch")
	}
	state, err := collectRecoveryState(ctx, manifest)
	if err != nil {
		return err
	}
	seen := map[recoveryKey]bool{}
	for _, row := range requirements {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !state.participants[row.Recipient] {
			return errors.New("callback ledger recipient is not a saved participant")
		}
		if err := state.reconcileRow(version, row, seen); err != nil {
			return err
		}
	}
	if len(state.outcomes) != 0 {
		return errors.New("SDK completed outcome has no server outcome or completed tombstone")
	}
	if len(state.queued) != 0 {
		return errors.New("SDK queued callback has no authoritative server ledger row")
	}
	return nil
}

type recoveryKey struct {
	recipient core.FederateHandle
	identity  string
}

type manifestRecoveryState struct {
	participants map[core.FederateHandle]bool
	capabilities map[core.FederateHandle]string
	outcomes     map[recoveryKey]pendingOutcome
	queued       map[recoveryKey]bool
}

func (s manifestRecoveryState) reconcileRow(version uint32, row savepoint.CallbackRecoveryRequirement, seen map[recoveryKey]bool) error {
	if version == 1 {
		if row.Phase != checkpointPending || len(row.InvocationIdentity) != 0 || row.Outcome != nil {
			return errors.New("legacy callback ledger cannot carry entered outcomes")
		}
		return nil
	}
	if len(row.InvocationIdentity) != 32 {
		return errors.New("callback ledger invocation identity must be 32 bytes")
	}
	k := recoveryKey{row.Recipient, hex.EncodeToString(row.InvocationIdentity)}
	if seen[k] {
		return errors.New("duplicate callback ledger invocation identity")
	}
	seen[k] = true
	result, known := s.outcomes[k]
	if err := validateRecoveryPhase(row, known, s.capabilities[row.Recipient]); err != nil {
		return err
	}
	if known && row.Outcome != nil && (result.Success != row.Outcome.Success || result.Exception != row.Outcome.Exception) {
		return errors.New("SDK outcome conflicts with authoritative server result")
	}
	delete(s.outcomes, k)
	delete(s.queued, k)
	return nil
}

func validateRecoveryPhase(row savepoint.CallbackRecoveryRequirement, known bool, capability string) error {
	switch row.Phase {
	case checkpointPending:
		if known || row.Outcome != nil {
			return errors.New("finished SDK callback cannot be restored as pending body")
		}
	case "outcome":
		if capability != checkpointServerV2 {
			return errors.New("outcome-only callback requires participant server-v2 runtime capability")
		}
		if !known {
			return errors.New("entered callback lacks a known finished SDK outcome")
		}
	case checkpointCompleted:
		// Acknowledged SDK results may already be cleared. A completed
		// tombstone is authoritative even when it retains only identity.
	default:
		return fmt.Errorf("unknown callback ledger phase %q", row.Phase)
	}
	return nil
}

func collectRecoveryState[T core.LogicalTimeScalar](ctx context.Context, manifest savepoint.ManifestOf[T]) (manifestRecoveryState, error) {
	participants := map[core.FederateHandle]bool{}
	capabilities := map[core.FederateHandle]string{}
	outcomes := map[recoveryKey]pendingOutcome{}
	queued := map[recoveryKey]bool{}
	total := 0
	for i, recipient := range manifest.Federates {
		if recipient == 0 || participants[recipient] {
			return manifestRecoveryState{}, errors.New("invalid SDK callback recovery participant")
		}
		participants[recipient] = true
		var state []byte
		if len(manifest.FederateClientStates) != 0 {
			state = manifest.FederateClientStates[i]
		}
		total += len(state)
		if total > 8<<20 {
			return manifestRecoveryState{}, errors.New("SDK callback recovery aggregate state limit")
		}
		if len(state) == 0 {
			continue
		}
		if err := collectParticipantRecovery(ctx, manifest, recipient, state, capabilities, outcomes, queued); err != nil {
			return manifestRecoveryState{}, err
		}
	}
	return manifestRecoveryState{participants, capabilities, outcomes, queued}, nil
}

func collectParticipantRecovery[T core.LogicalTimeScalar](ctx context.Context, manifest savepoint.ManifestOf[T], recipient core.FederateHandle, state []byte, capabilities map[core.FederateHandle]string, outcomes map[recoveryKey]pendingOutcome, queued map[recoveryKey]bool) error {
	if manifest.FederationGeneration == nil {
		return errors.New("SDK callback recovery source generation absent")
	}
	if err := Validate(ctx, savepoint.ClientStateValidationRequest{Federation: manifest.Federation, Label: manifest.Label, SourceGeneration: *manifest.FederationGeneration, SourceHandle: recipient, LogicalTimeImplementationName: manifest.LogicalTimeImplementationName, State: state}); err != nil {
		return err
	}
	e, err := Decode(state)
	if err != nil {
		return err
	}
	capability, saved := recoveryState(e)
	capabilities[recipient] = capability
	for _, result := range saved {
		outcomes[recoveryKey{recipient, result.Identity}] = result
	}
	identities, err := queuedRecoveryIdentities(e)
	if err != nil {
		return err
	}
	for _, identity := range identities {
		queued[recoveryKey{recipient, identity}] = true
	}
	return nil
}

const checkpointCompleted = "completed"

const checkpointPending = "pending"
