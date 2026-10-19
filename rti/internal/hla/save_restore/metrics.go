package savepoint

import "github.com/cbchoi/gorti/rti/internal/hla/core"

type MOMMetricsOf[T core.LogicalTimeScalar] struct {
	FederateState uint32
	NextSaveName  string
	NextSaveTime  *T
	// Unknown history must not be encoded as a known no-save-yet value.
	LastSaveHistoryKnown bool
	LastSaveExists       bool
	LastSaveName         string
	LastSaveTime         *T
}

// QueryMOMMetrics does not acquire operation/checkpoint admission again. The
// caller holds admission before entering this read-only state lock.
func (m *ManagerOf[T]) QueryMOMMetrics(fed core.FederationName, h core.FederateHandle) MOMMetricsOf[T] {
	key := historyKey(fed, nil)
	identityKnown := true
	if m.opts.Generation != nil {
		key.Generation, identityKnown = m.opts.Generation(fed)
		key.HasGeneration = true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := MOMMetricsOf[T]{FederateState: 1}
	if identityKnown {
		history := cloneSaveHistoryOf[T](m.history[key])
		result.LastSaveHistoryKnown = history.Known
		result.LastSaveExists = history.HasSave
		result.LastSaveName = history.Name
		result.LastSaveTime = history.Time
	}
	if active := m.saves[fed]; active != nil {
		result.NextSaveName = active.label
		if active.saveTime != nil {
			value := *active.saveTime
			result.NextSaveTime = &value
		}
		if _, member := active.required[h]; member && saveParticipantStarted(active, h) {
			result.FederateState = 3
		}
	}
	if active := m.restore[fed]; active != nil {
		if _, member := active.required[h]; member {
			result.FederateState = 5
		}
	}
	return result
}
