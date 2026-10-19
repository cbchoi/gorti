package time

import "github.com/cbchoi/gorti/rti/internal/hla/core"

type FederateState = FederateStateOf[core.LogicalTime]
type IntegerFederateState = FederateStateOf[int64]
type GrantContext = GrantContextOf[core.LogicalTime]
type IntegerGrantContext = GrantContextOf[int64]
type GrantDecision = GrantDecisionOf[core.LogicalTime]
type IntegerGrantDecision = GrantDecisionOf[int64]
type GrantStrategy = GrantStrategyOf[core.LogicalTime]
type IntegerGrantStrategy = GrantStrategyOf[int64]
type LBTSStrategy = LBTSStrategyOf[core.LogicalTime]
type IntegerLBTSStrategy = LBTSStrategyOf[int64]
type Manager = ManagerOf[core.LogicalTime]
type IntegerManager = ManagerOf[int64]
type ManagerState = ManagerStateOf[core.LogicalTime]
type IntegerManagerState = ManagerStateOf[int64]
type MetricsSnapshot = MetricsSnapshotOf[core.LogicalTime]
type IntegerMetricsSnapshot = MetricsSnapshotOf[int64]
type Options = OptionsOf[core.LogicalTime]
type IntegerOptions = OptionsOf[int64]
type RegulatingFederate = RegulatingFederateOf[core.LogicalTime]
type IntegerRegulatingFederate = RegulatingFederateOf[int64]
type RetractionHistory = RetractionHistoryOf[core.LogicalTime]
type IntegerRetractionHistory = RetractionHistoryOf[int64]
type RetractionMessageState = RetractionMessageStateOf[core.LogicalTime]
type IntegerRetractionMessageState = RetractionMessageStateOf[int64]
type RetractionProducerHistory = RetractionProducerHistoryOf[core.LogicalTime]
type IntegerRetractionProducerHistory = RetractionProducerHistoryOf[int64]

// Preserve the established HLA API name.
//
//revive:disable-next-line:exported
type TimeAdvanceGrant = TimeAdvanceGrantOf[core.LogicalTime]
type IntegerTimeAdvanceGrant = TimeAdvanceGrantOf[int64]

// Preserve the established HLA API name.
//
//revive:disable-next-line:exported
type TimeConstrainedEnabled = TimeConstrainedEnabledOf[core.LogicalTime]
type IntegerTimeConstrainedEnabled = TimeConstrainedEnabledOf[int64]

// Preserve the established HLA API name.
//
//revive:disable-next-line:exported
type TimeRegulationEnabled = TimeRegulationEnabledOf[core.LogicalTime]
type IntegerTimeRegulationEnabled = TimeRegulationEnabledOf[int64]
type bufferedTSOEvent = bufferedTSOEventOf[core.LogicalTime]
type federateState = federateStateOf[core.LogicalTime]
type grantDecision = grantDecisionOf[core.LogicalTime]
type nerState = nerStateOf[core.LogicalTime]
type nerStore = nerStoreOf[core.LogicalTime]
type stateStore = stateStoreOf[core.LogicalTime]
type timeAdvanceGrantedRecord = TimeAdvanceGrantedRecordOf[core.LogicalTime]

// Preserve the established HLA API name.
//
//revive:disable-next-line:exported
type TimeAdvanceGrantedRecord = TimeAdvanceGrantedRecordOf[core.LogicalTime]
type IntegerTimeAdvanceGrantedRecord = TimeAdvanceGrantedRecordOf[int64]

func minimumEnableTime(current, lookahead, peerTime core.LogicalTime, exclusive bool) (core.LogicalTime, bool) {
	return minimumEnableTimeOf[core.LogicalTime](current, lookahead, peerTime, exclusive)
}

func LBTS(set []RegulatingFederate) core.LogicalTime { return LBTSOf[core.LogicalTime](set) }

func New(opts Options) (*Manager, error) { return NewOf[core.LogicalTime](opts) }

func MergeRetractionHistories(histories ...*RetractionHistory) (*RetractionHistory, error) {
	return MergeRetractionHistoriesOf[core.LogicalTime](histories...)
}

func checkAdvanceTarget(currentTime, requested core.LogicalTime) error {
	return checkAdvanceTargetOf[core.LogicalTime](currentTime, requested)
}

func newStateStore() *stateStore { return newStateStoreOf[core.LogicalTime]() }

func DefaultLBTSStrategy() LBTSStrategy { return DefaultLBTSStrategyOf[core.LogicalTime]() }

func DefaultGrantStrategy() GrantStrategy { return DefaultGrantStrategyOf[core.LogicalTime]() }

func outgoingTSOBound(fs *federateState, ns *nerState) (base, lookahead core.LogicalTime, exclusive bool) {
	return outgoingTSOBoundOf[core.LogicalTime](fs, ns)
}

func decideGrant(mode AdvanceMode, currentTime, requested, lbts core.LogicalTime, solePending bool, nextTSO core.LogicalTime, hasTSO bool) grantDecision {
	return decideGrantOf[core.LogicalTime](mode, currentTime, requested, lbts, solePending, nextTSO, hasTSO)
}

func ValidateRetractionHistory(history *RetractionHistory) error {
	return ValidateRetractionHistoryOf[core.LogicalTime](history)
}

func addTimeBound(at, interval core.LogicalTime) core.LogicalTime {
	return addTimeBoundOf[core.LogicalTime](at, interval)
}

func extOf(m *Manager) *nerStore { return extOfOf[core.LogicalTime](m) }

func NewInteger(opts IntegerOptions) (*IntegerManager, error) { return NewOf[int64](opts) }
