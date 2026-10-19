package savebundle

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func validateDeletionRecipientTimesOf[T, N core.LogicalTimeScalar](v *graphValidationOf[T], federates []timeFederateSnapshotOf[N]) error {
	byHandle := make(map[core.FederateHandle]timeFederateSnapshotOf[N], len(federates))
	for _, federate := range federates {
		byHandle[federate.Handle] = federate
	}
	for _, deletion := range v.deletions {
		for _, recipient := range deletion.Recipients {
			if recipient.Forgotten {
				continue
			}
			state, ok := byHandle[recipient.Recipient]
			if !ok || !recipient.ReceivedTSO || !state.HasRegulationState || !state.HasAdvanceState || !state.Constrained || T(state.CurrentTime) >= deletion.Timestamp {
				return fmt.Errorf("unforgotten deletion receipt conflicts with recipient time state")
			}
		}
	}
	return nil
}
