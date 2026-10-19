package savepoint

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// RetractionHistoryPreview validates a complete saved ledger and previews its
// union with current generation history without publishing either state.
type RetractionHistoryPreview interface {
	PreviewNextRetractionHandle(core.FederationName, []byte, core.FederateHandle) (uint64, error)
}

func (m *ManagerOf[T]) restoreRetractionHints(fed core.FederationName, manifest ManifestOf[T]) (map[core.FederateHandle]uint64, error) {
	snapshot, configured := m.opts.ManagerSnapshots["retractions"]
	if !configured {
		return nil, nil
	}
	preview, ok := snapshot.(RetractionHistoryPreview)
	data, present := manifest.ManagerSnapshots["retractions"]
	if !ok || !present {
		return nil, core.ErrRetractionHistoryUnavailable
	}
	hints := make(map[core.FederateHandle]uint64, len(manifest.Federates))
	for _, saved := range manifest.Federates {
		next, err := preview.PreviewNextRetractionHandle(fed, data, saved)
		if err != nil {
			return nil, fmt.Errorf("restore retraction history for producer %d: %w", saved, err)
		}
		if next == 0 {
			return nil, core.ErrRetractionHistoryUnavailable
		}
		hints[saved] = next
	}
	return hints, nil
}
