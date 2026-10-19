package mom

import "github.com/cbchoi/gorti/rti/internal/hla/core"

// DataCallbackReport accounts for an entry, or a successful completion when
// Entry is nil. The caller has already serialized and deduplicated receipts.
type DataCallbackReport struct {
	Entry *DataCallbackEntry
}

// TryRecordDataCallbackReports declines without mutation when detailed MOM
// reporting is enabled. Its flag check and ordered accounting share the same
// lock, so a reporting change cannot disappear between selection and commit.
func (m *Manager) TryRecordDataCallbackReports(fed core.FederationName, h core.FederateHandle, reports []DataCallbackReport) (bool, []error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.fed[fed]
	if f == nil || f.federates[h] == nil || m.traffic == nil {
		return false, nil
	}
	fs := f.federates[h]
	if fs.serviceReportingEnabled || fs.exceptionReportingEnabled {
		return false, nil
	}
	// This is also the lock order used by ExportState.
	m.traffic.mu.Lock()
	defer m.traffic.mu.Unlock()
	var failures []error
	for _, report := range reports {
		if report.Entry == nil {
			m.callbackPendingLocked(fed, h, -1)
		} else if err := m.recordDataCallbackEntryLocked(fed, h, *report.Entry); err != nil {
			m.dataCallbackEntryUnavailableLocked(fed, h, *report.Entry, err)
			failures = append(failures, err)
		}
	}
	return true, failures
}
