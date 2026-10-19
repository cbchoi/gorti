package savebundle

import (
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type Time = TimeOf[core.LogicalTime]
type IntegerTime = TimeOf[int64]
type Object = ObjectOf[core.LogicalTime]
type IntegerObject = ObjectOf[int64]
type Retractions = RetractionsOf[core.LogicalTime]
type IntegerRetractions = RetractionsOf[int64]
type RestoreGraph = RestoreGraphOf[core.LogicalTime]
type IntegerRestoreGraph = RestoreGraphOf[int64]
type graphValidation = graphValidationOf[core.LogicalTime]
type objectSnapshotV1 = objectSnapshotV1Of[core.LogicalTime]
type timeSnapshot = timeSnapshotOf[timeSnapshotNumber]
type timeFederateSnapshot = timeFederateSnapshotOf[timeSnapshotNumber]
type timeQueuedSnapshot = timeQueuedSnapshotOf[timeSnapshotNumber]
type timeEventSnapshot = timeEventSnapshotOf[timeSnapshotNumber]
type retractionSnapshot = retractionSnapshotOf[core.LogicalTime, timeSnapshotNumber]
type SaveHistoryRecord = SaveHistoryRecordOf[core.LogicalTime]

func cloneTimeEvent(e core.OutboundEvent) (core.OutboundEvent, error) {
	return cloneTimeEventOf[core.LogicalTime, timeSnapshotNumber](e)
}
func encodeTimeEvent(e core.OutboundEvent) (timeEventSnapshot, error) {
	return encodeTimeEventOf[core.LogicalTime, timeSnapshotNumber](e)
}
func decodeTimeEvent(e timeEventSnapshot) (*object.CallbackEvent, error) {
	return decodeTimeEventOf[core.LogicalTime, timeSnapshotNumber](e)
}
func objectSnapshotVersion(e *object.RegistryState) uint32 { return objectSnapshotVersionOf(e) }
func decodeRetractionSnapshot(data []byte) (*timepkg.RetractionHistory, error) {
	return decodeRetractionSnapshotOf[core.LogicalTime, timeSnapshotNumber](data)
}
func marshalRetractionSnapshot(h *timepkg.RetractionHistory) ([]byte, error) {
	return marshalRetractionSnapshotOf[core.LogicalTime, timeSnapshotNumber](h)
}
func MergeSessionRetractions(saved, current []byte, mapping map[core.FederateHandle]core.FederateHandle) ([]byte, error) {
	return MergeSessionRetractionsOf[core.LogicalTime](saved, current, mapping)
}
func EncodeSaveHistoryRecord(r SaveHistoryRecord) ([]byte, error) {
	return EncodeSaveHistoryRecordOf(r)
}
func DecodeSaveHistoryRecord(data []byte) (SaveHistoryRecord, error) {
	return DecodeSaveHistoryRecordOf[core.LogicalTime](data)
}
