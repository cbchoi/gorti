package server

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"slices"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"github.com/cbchoi/gorti/rti/internal/adapters/sdkstate"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

// validateSDKRestoreCallbackRecoveryOf is the production Options hook. The
// owning decoder provides the validated version and source-only ledger rows.
func validateSDKRestoreCallbackRecoveryOf[T core.LogicalTimeScalar](ctx context.Context, fed core.FederationName, manifest savepoint.ManifestOf[T]) error {
	return newSDKRestoreCallbackRecoveryOf[T](func(ctx context.Context, _ core.FederationName, image []byte) (uint32, []savepoint.CallbackRecoveryRequirement, error) {
		if err := ctx.Err(); err != nil {
			return 0, nil, err
		}
		version, rows, err := grpcadapter.CallbackCheckpointRecoveryVersion(image)
		if err != nil {
			return 0, nil, err
		}
		requirements := make([]savepoint.CallbackRecoveryRequirement, 0, len(rows))
		for _, row := range rows {
			identity, err := hex.DecodeString(row.Identity)
			if err != nil || len(identity) != 32 || hex.EncodeToString(identity) != row.Identity {
				return 0, nil, fmt.Errorf("invalid callback source invocation identity")
			}
			requirement := savepoint.CallbackRecoveryRequirement{Recipient: row.Recipient, InvocationIdentity: identity, Phase: row.Phase}
			if row.Outcome != nil {
				requirement.Outcome = &savepoint.CallbackRecoveryOutcome{Success: row.Outcome.Success, Exception: row.Outcome.Exception}
			}
			requirements = append(requirements, requirement)
		}
		return version, requirements, nil
	})(ctx, fed, manifest)
}

// newSDKRestoreCallbackRecoveryOf keeps the callback image codec in its owning
// adapter. The validated image version is explicit; entered outcomes require
// negotiated SDK capability before any restore state is prepared.
func newSDKRestoreCallbackRecoveryOf[T core.LogicalTimeScalar](resolve func(context.Context, core.FederationName, []byte) (uint32, []savepoint.CallbackRecoveryRequirement, error)) func(context.Context, core.FederationName, savepoint.ManifestOf[T]) error {
	return func(ctx context.Context, fed core.FederationName, manifest savepoint.ManifestOf[T]) error {
		version := uint32(1)
		var rows []savepoint.CallbackRecoveryRequirement
		if image, present := manifest.ManagerSnapshots["callback_pending"]; present {
			if resolve == nil {
				return fmt.Errorf("%w: callback recovery ledger resolver unavailable", savepoint.ErrSaveBundleIncompatible)
			}
			var err error
			version, rows, err = resolve(ctx, fed, bytes.Clone(image))
			if err != nil {
				return fmt.Errorf("%w: callback recovery source ledger: %w", savepoint.ErrSaveBundleIncompatible, err)
			}
		}
		if err := sdkstate.ReconcileManifestVersionOf(ctx, manifest, version, rows); err != nil {
			return fmt.Errorf("%w: SDK callback outcome preflight: %w", savepoint.ErrSaveBundleIncompatible, err)
		}
		return nil
	}
}

// extendSDKRestoreReferencesOf fully validates every private source image before
// ReserveRestoreHandles and merges SDK-only historical producers into the graph
// census. Allocation and publication are deliberately absent from this helper.
func extendSDKRestoreReferencesOf[T core.LogicalTimeScalar](ctx context.Context, manifest savepoint.ManifestOf[T], references []core.FederateHandle) ([]core.FederateHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(manifest.FederateClientStates) == 0 {
		return slices.Clone(references), nil
	}
	if manifest.FederationGeneration == nil || len(manifest.FederateClientStates) != len(manifest.Federates) {
		return nil, fmt.Errorf("%w: SDK source provenance or roster missing", savepoint.ErrSaveBundleIncompatible)
	}
	seen := make(map[core.FederateHandle]bool, len(references))
	for _, h := range references {
		seen[h] = true
	}
	total := 0
	for i, state := range manifest.FederateClientStates {
		total += len(state)
		if total > 8<<20 {
			return nil, fmt.Errorf("%w: SDK federation checkpoint size limit", savepoint.ErrSaveBundleIncompatible)
		}
		if len(state) == 0 {
			continue
		}
		if err := sdkstate.Validate(ctx, savepoint.ClientStateValidationRequest{
			Federation: manifest.Federation, Label: manifest.Label, SourceGeneration: *manifest.FederationGeneration,
			SourceHandle: manifest.Federates[i], LogicalTimeImplementationName: manifest.LogicalTimeImplementationName, State: state,
		}); err != nil {
			return nil, fmt.Errorf("%w: SDK source preflight: %w", savepoint.ErrSaveBundleIncompatible, err)
		}
		refs, err := sdkstate.References(state)
		if err != nil {
			return nil, fmt.Errorf("%w: SDK source references: %w", savepoint.ErrSaveBundleIncompatible, err)
		}
		for _, h := range refs {
			seen[h] = true
		}
	}
	merged := make([]core.FederateHandle, 0, len(seen))
	for h := range seen {
		merged = append(merged, h)
	}
	slices.Sort(merged)
	return merged, nil
}

// rebindSDKClientStatesOf uses the SAME complete allocation map as graph.Rebind,
// exactly once. Domain RebindClientState then validates this prepared candidate
// instead of rewriting it again with its participant-only projection.
func rebindSDKClientStatesOf[T core.LogicalTimeScalar](ctx context.Context, source, rebound savepoint.ManifestOf[T], generation uint64, mapping map[core.FederateHandle]core.FederateHandle) (savepoint.ManifestOf[T], error) {
	if len(source.FederateClientStates) == 0 {
		rebound.FederateClientStates = nil
		return rebound, nil
	}
	if source.FederationGeneration == nil || len(source.FederateClientStates) != len(source.Federates) || len(rebound.Federates) != len(source.Federates) {
		return savepoint.ManifestOf[T]{}, fmt.Errorf("%w: SDK source/target roster mismatch", savepoint.ErrSaveBundleIncompatible)
	}
	states := make([][]byte, len(source.FederateClientStates))
	for i, state := range source.FederateClientStates {
		if len(state) == 0 {
			states[i] = slices.Clone(state)
			continue
		}
		translated, err := sdkstate.Rebind(ctx, savepoint.ClientStateRebindRequest{
			Federation: source.Federation, Label: source.Label, SourceGeneration: *source.FederationGeneration,
			TargetGeneration: generation, SourceHandle: source.Federates[i], TargetHandle: rebound.Federates[i],
			LogicalTimeImplementationName: source.LogicalTimeImplementationName, FederateHandles: mapping, State: state,
		})
		if err != nil {
			return savepoint.ManifestOf[T]{}, fmt.Errorf("%w: SDK runtime rewrite: %w", savepoint.ErrSaveBundleIncompatible, err)
		}
		states[i] = translated
	}
	rebound.FederateClientStates = states
	return rebound, nil
}
