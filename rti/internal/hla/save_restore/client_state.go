package savepoint

import (
	"context"
	"errors"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const MaxClientStateBytes = 1 << 20
const maxFederationClientStateBytes = 8 << 20

var ErrClientStateTooLarge = errors.New("savepoint: SDK runtime checkpoint exceeds the configured format limit")

// FederateSaveCompleteWithClientState commits runtime state in the same transaction
// as the participant's completion. A rejected response cannot replace prior state.
func (m *ManagerOf[T]) FederateSaveCompleteWithClientState(ctx context.Context, fed core.FederationName, handle core.FederateHandle, state []byte) error {
	return m.recordFederateSave(ctx, fed, handle, true, state)
}

func validateClientStateAdmissionOf[T core.LogicalTimeScalar](save *activeSaveOf[T], state []byte) error {
	if len(state) > MaxClientStateBytes {
		return ErrClientStateTooLarge
	}
	total := len(state)
	for _, value := range save.clientStates {
		total += len(value)
	}
	if total > maxFederationClientStateBytes {
		return ErrClientStateTooLarge
	}
	return nil
}

func clientStatesForOf[T core.LogicalTimeScalar](save *activeSaveOf[T], handles []core.FederateHandle) [][]byte {
	if save == nil || len(save.clientStates) == 0 {
		return nil
	}
	states := make([][]byte, len(handles))
	for i, handle := range handles {
		states[i] = append([]byte(nil), save.clientStates[handle]...)
	}
	return states
}

func validateClientStatesOf[T core.LogicalTimeScalar](manifest ManifestOf[T]) error {
	if len(manifest.FederateClientStates) == 0 {
		return nil
	}
	if len(manifest.FederateClientStates) != len(manifest.Federates) {
		return fmt.Errorf("%w: runtime checkpoint roster mismatch", ErrSaveBundleIncompatible)
	}
	total := 0
	for _, state := range manifest.FederateClientStates {
		total += len(state)
		if len(state) > MaxClientStateBytes || total > maxFederationClientStateBytes {
			return fmt.Errorf("%w: runtime checkpoint limit", ErrSaveBundleIncompatible)
		}
	}
	return nil
}
