package grpc

import (
	"fmt"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	"google.golang.org/protobuf/proto"
)

// CallbackRecoveryRow describes evidence for runtime-state reconciliation, not
// the receipt capability needed to report a callback.
type CallbackRecoveryRow struct {
	Recipient core.FederateHandle
	Identity  string
	Phase     string
	Outcome   *CallbackCheckpointOutcome
}

func CallbackCheckpointSyncCallbacks(data []byte) ([]syncpkg.RestoredCallback, error) {
	image, err := decodeCallbackCheckpoint(data)
	if err != nil {
		return nil, err
	}
	var rows []syncpkg.RestoredCallback
	handles := func(in []uint64) []core.FederateHandle {
		out := make([]core.FederateHandle, len(in))
		for i, h := range in {
			out[i] = core.FederateHandle(h)
		}
		return out
	}
	for _, row := range image.Deliveries {
		if row.Phase == callbackPhaseCompleted {
			continue
		}
		event := &rtiv1.FederateEvent{}
		if err = proto.Unmarshal(row.Event, event); err != nil {
			return nil, err
		}
		r := syncpkg.RestoredCallback{Recipient: row.Recipient, Sequence: event.Seq, OutcomeOnly: row.Phase == callbackPhaseOutcome}
		switch {
		case event.GetSyncAnnounced() != nil:
			v := event.GetSyncAnnounced()
			r.Kind = core.SyncCallbackAnnounced
			r.Label = v.Label
			r.Tag = v.Tag
			r.Required = handles(v.RequiredFederates)
		case event.GetSyncSynchronized() != nil:
			v := event.GetSyncSynchronized()
			r.Kind = core.SyncCallbackSynchronized
			r.Label = v.Label
			r.Failed = handles(v.FailedToSync)
		case event.GetSyncRegistrationSucceeded() != nil:
			r.Kind = core.SyncCallbackRegistrationSucceeded
			r.Label = event.GetSyncRegistrationSucceeded().Label
		case event.GetSyncRegistrationFailed() != nil:
			v := event.GetSyncRegistrationFailed()
			r.Kind = core.SyncCallbackRegistrationFailed
			r.Label = v.Label
			r.Reason, err = checkpointSyncFailureReason(v.Reason)
			if err != nil {
				return nil, err
			}
		default:
			continue
		}
		rows = append(rows, r)
	}
	return rows, nil
}

func (CallbackCheckpoint) ValidateOwnership(data []byte, state *ownership.ManagerState) error {
	image, err := decodeCallbackCheckpoint(data)
	if err != nil {
		return err
	}
	for _, row := range image.Deliveries {
		if row.Phase == callbackPhaseCompleted {
			continue
		}
		event := &rtiv1.FederateEvent{}
		if err = proto.Unmarshal(row.Event, event); err != nil {
			return err
		}
		if _, err = PrepareOwnershipCheckpointEvent(state, row.Recipient, event, row.Ownership, row.Phase == callbackPhaseOutcome); err != nil {
			return err
		}
	}
	return nil
}

func CallbackCheckpointRecoveryRows(data []byte) ([]CallbackRecoveryRow, error) {
	_, rows, err := CallbackCheckpointRecoveryVersion(data)
	return rows, err
}

func CallbackCheckpointRecoveryVersion(data []byte) (uint32, []CallbackRecoveryRow, error) {
	image, err := decodeCallbackCheckpoint(data)
	if err != nil {
		return 0, nil, err
	}
	if image.Version != 2 {
		return image.Version, nil, nil
	}
	rows := make([]CallbackRecoveryRow, 0, len(image.Deliveries))
	for _, row := range image.Deliveries {
		rows = append(rows, CallbackRecoveryRow{Recipient: row.Recipient, Identity: row.Identity, Phase: row.Phase, Outcome: row.Outcome})
	}
	return image.Version, rows, nil
}
func checkpointSyncFailureReason(reason rtiv1.SyncPointFailureReason) (syncpkg.RegistrationFailureReason, error) {
	switch reason {
	case rtiv1.SyncPointFailureReason_SYNC_POINT_FAILURE_REASON_LABEL_NOT_UNIQUE:
		return syncpkg.RegistrationFailureLabelNotUnique, nil
	case rtiv1.SyncPointFailureReason_SYNC_POINT_FAILURE_REASON_SET_MEMBER_NOT_JOINED:
		return syncpkg.RegistrationFailureSetMemberNotJoined, nil
	default:
		return 0, fmt.Errorf("callback checkpoint: invalid synchronization failure reason")
	}
}
