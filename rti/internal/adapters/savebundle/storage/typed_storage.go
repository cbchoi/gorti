package storage

import (
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

// Bundle IO remains byte-oriented. Only the completion history has a scalar
// representation; integer histories have a separate versioned directory.
type FSStorageOf[T core.LogicalTimeScalar] struct{ *FSStorage }

func NewFSStorageOf[T core.LogicalTimeScalar](dir string) (*FSStorageOf[T], error) {
	storage, err := NewFSStorage(dir)
	if err != nil {
		return nil, err
	}
	return &FSStorageOf[T]{FSStorage: storage}, nil
}

func (s *FSStorage) LoadSaveHistory(key savepoint.SaveHistoryKey) (savepoint.SaveHistory, error) {
	return (&FSStorageOf[core.LogicalTime]{s}).LoadSaveHistory(key)
}
func (s *FSStorage) StoreSaveHistory(key savepoint.SaveHistoryKey, h savepoint.SaveHistory) error {
	return (&FSStorageOf[core.LogicalTime]{s}).StoreSaveHistory(key, h)
}
func (s *FSStorage) DiscardUncommittedSave(key savepoint.SaveHistoryKey, label string) error {
	return (&FSStorageOf[core.LogicalTime]{s}).DiscardUncommittedSave(key, label)
}
func (s *FSStorage) historyDir(key savepoint.SaveHistoryKey) string {
	return (&FSStorageOf[core.LogicalTime]{s}).historyDir(key)
}

var _ savepoint.SaveHistoryStore = (*FSStorage)(nil)
var _ savepoint.UncommittedSaveDiscarder = (*FSStorage)(nil)
