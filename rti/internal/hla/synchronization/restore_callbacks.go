package sync

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// RestoredCallback identifies an unfinished obligation owned by transport replay.
// OutcomeOnly records known entry, never an inferred application outcome.
type RestoredCallback struct {
	Kind        core.SyncCallbackKind
	Recipient   core.FederateHandle
	Sequence    uint64
	Label       string
	OutcomeOnly bool
	Tag         []byte
	Required    []core.FederateHandle
	Failed      []core.FederateHandle
	Reason      RegistrationFailureReason
}

// ValidateRestoredCallbacks checks detached domain and transport images together.
func ValidateRestoredCallbacks(state *ManagerState, captured []RestoredCallback) error {
	if err := ValidateSnapshot(state); err != nil {
		return err
	}
	obligations := map[uint64]CallbackState{}
	if state != nil {
		for _, callback := range state.Obligations {
			obligations[callback.Sequence] = callback
		}
	}
	_, err := validateRestoredCallbacks(obligations, captured)
	return err
}

func validateRestoredCallbacks(obligations map[uint64]CallbackState, captured []RestoredCallback) (map[uint64]bool, error) {
	sequences := make(map[uint64]bool, len(captured))
	for _, recovered := range captured {
		if recovered.Sequence == 0 || sequences[recovered.Sequence] {
			return nil, fmt.Errorf("sync recovery: invalid or duplicate captured sequence")
		}
		callback, exists := obligations[recovered.Sequence]
		if !exists || callback.Recipient != recovered.Recipient || callback.Label != recovered.Label || callback.Kind.observedKind() != recovered.Kind {
			return nil, fmt.Errorf("sync recovery: captured obligation identity mismatch")
		}
		if callback.Entered != recovered.OutcomeOnly {
			return nil, fmt.Errorf("sync recovery: captured invocation phase mismatch")
		}
		if !bytes.Equal(callback.Tag, recovered.Tag) || !slices.Equal(callback.Required, recovered.Required) || !slices.Equal(callback.Failed, recovered.Failed) || callback.Reason != recovered.Reason {
			return nil, fmt.Errorf("sync recovery: captured immutable payload mismatch")
		}
		sequences[recovered.Sequence] = true
	}
	return sequences, nil
}

// ReconcileRestoredCallbacks runs after domain import and before transport
// publication/retry, while the root holds exclusive checkpoint admission.
func (m *Manager) ReconcileRestoredCallbacks(fed core.FederationName, captured []RestoredCallback) error {
	if len(captured) == 0 {
		return nil
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.RLock()
	next := cloneFederationState(m.fed[fed])
	m.mu.RUnlock()
	sequences, err := validateRestoredCallbacks(next.obligations, captured)
	if err != nil {
		return err
	}
	next.pending = slices.DeleteFunc(next.pending, func(callback CallbackState) bool {
		return sequences[callback.Sequence]
	})
	m.publish(fed, next)
	return nil
}
