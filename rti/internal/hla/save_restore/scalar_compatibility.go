package savepoint

import (
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"io"
)

type Manager = ManagerOf[core.LogicalTime]
type Options = OptionsOf[core.LogicalTime]
type Manifest = ManifestOf[core.LogicalTime]
type SaveHistory = SaveHistoryOf[core.LogicalTime]
type SaveHistoryStore = SaveHistoryStoreOf[core.LogicalTime]
type MOMMetrics = MOMMetricsOf[core.LogicalTime]
type InitiateSaveEvent = InitiateSaveEventOf[core.LogicalTime]
type activeSave = activeSaveOf[core.LogicalTime]
type activeRestore = activeRestoreOf[core.LogicalTime]

func New(opts Options) (*Manager, error) { return NewOf(opts) }
func WriteBundle(w io.Writer, manifest Manifest, eventLog []byte) error {
	return WriteBundleOf(w, manifest, eventLog)
}
func ReadBundle(r io.Reader) (Manifest, []byte, error) { return ReadBundleOf[core.LogicalTime](r) }
func validateClientStates(manifest Manifest) error     { return validateClientStatesOf(manifest) }
func cloneSaveHistory(history SaveHistory) SaveHistory { return cloneSaveHistoryOf(history) }
