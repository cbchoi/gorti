package storage

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

// History shares the bundle adapter's single-writer-process assumption. Immutable
// records avoid nonportable replacement renames. Synced payload + atomic rename
// covers process restart; filesystem/power-loss guarantees remain those of the
// existing bundle writer. A partial temporary entry is never a decision.
var historyPublishMu sync.Mutex

const maxSaveHistoryBytes = 1024 * 1024

func (s *FSStorageOf[T]) LoadSaveHistory(key savepoint.SaveHistoryKey) (savepoint.SaveHistoryOf[T], error) {
	historyPublishMu.Lock()
	defer historyPublishMu.Unlock()
	files, err := s.historyFiles(key)
	if err != nil || len(files) == 0 {
		return savepoint.SaveHistoryOf[T]{}, err
	}
	record, err := s.readHistoryRecord(key, files[len(files)-1])
	if err != nil {
		return savepoint.SaveHistoryOf[T]{}, err
	}
	return savepoint.SaveHistoryOf[T]{Known: true, HasSave: record.Boundary == savebundle.SaveHistoryCommitBoundary, Name: record.Name, Time: record.Time}, nil
}

func (s *FSStorageOf[T]) StoreSaveHistory(key savepoint.SaveHistoryKey, history savepoint.SaveHistoryOf[T]) (err error) {
	if err := history.Validate(); err != nil {
		return err
	}
	if !history.Known || key.Federation == "" || (!key.HasGeneration && key.Generation != 0) {
		return fmt.Errorf("save history: unknown history or invalid execution key")
	}
	historyPublishMu.Lock()
	defer historyPublishMu.Unlock()
	files, err := s.historyFiles(key)
	if err != nil {
		return err
	}
	sequence, err := s.nextHistorySequence(key, history, files)
	if err != nil {
		return err
	}
	record := savebundle.SaveHistoryRecordOf[T]{Version: 1, Sequence: sequence, Federation: key.Federation, Name: history.Name, Time: history.Time, Boundary: savebundle.SaveHistoryCommitBoundary}
	if integerHistory[T]() {
		record.Version = 2
		record.LogicalTimeImplementationName = "HLAinteger64Time"
	}
	if !history.HasSave {
		record.Boundary = savebundle.SaveHistoryFreshBoundary
	}
	if key.HasGeneration {
		generation := key.Generation
		record.Generation = &generation
	}
	data, err := savebundle.EncodeSaveHistoryRecordOf[T](record)
	if err != nil {
		return err
	}
	if len(data) > maxSaveHistoryBytes {
		return fmt.Errorf("save history: record exceeds reader limit")
	}
	writer, err := s.newWriter(filepath.Join(s.historyDir(key), fmt.Sprintf("%016x.json", sequence)))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, writer.Abort()) }()
	if _, err := writer.Write(data); err != nil {
		return err
	}
	return writer.Close()
}

func (s *FSStorageOf[T]) DiscardUncommittedSave(key savepoint.SaveHistoryKey, label string) error {
	historyPublishMu.Lock()
	defer historyPublishMu.Unlock()
	files, err := s.historyFiles(key)
	if err != nil {
		return err
	}
	for _, file := range files {
		record, err := s.readHistoryRecord(key, file)
		if err != nil {
			return err
		}
		if record.Boundary == savebundle.SaveHistoryCommitBoundary && record.Name == label {
			return fmt.Errorf("save history: refusing to discard committed save %q", label)
		}
	}
	path := s.bundlePath(key.Federation, label)
	if key.HasGeneration {
		path = s.bundlePathFor(savepoint.StorageKey{Federation: key.Federation, Generation: key.Generation, Label: label})
	}
	fsPublishMu.Lock()
	defer fsPublishMu.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *FSStorageOf[T]) historyDir(key savepoint.SaveHistoryKey) string {
	generation := "legacy"
	if key.HasGeneration {
		generation = fmt.Sprintf("%016x", key.Generation)
	}
	version := "v1"
	if integerHistory[T]() {
		version = "v2-integer64"
	}
	return filepath.Join(s.Dir, "history", version, hexComponent(string(key.Federation)), generation)
}

func (s *FSStorageOf[T]) historyFiles(key savepoint.SaveHistoryKey) ([]string, error) {
	entries, err := os.ReadDir(s.historyDir(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && strings.Contains(name, ".tmp-") {
			continue
		}
		if entry.IsDir() || len(name) != 21 || !strings.HasSuffix(name, ".json") {
			return nil, fmt.Errorf("save history: invalid journal entry %q", name)
		}
		sequence, err := strconv.ParseUint(strings.TrimSuffix(name, ".json"), 16, 64)
		if err != nil || sequence == 0 || name != fmt.Sprintf("%016x.json", sequence) {
			return nil, fmt.Errorf("save history: invalid journal sequence %q", name)
		}
		files = append(files, name)
	}
	slices.Sort(files)
	return files, nil
}

func (s *FSStorageOf[T]) readHistoryRecord(key savepoint.SaveHistoryKey, file string) (savebundle.SaveHistoryRecordOf[T], error) {
	reader, err := os.Open(filepath.Join(s.historyDir(key), file))
	if err != nil {
		return savebundle.SaveHistoryRecordOf[T]{}, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxSaveHistoryBytes+1))
	if err != nil {
		return savebundle.SaveHistoryRecordOf[T]{}, err
	}
	if len(data) > maxSaveHistoryBytes {
		return savebundle.SaveHistoryRecordOf[T]{}, fmt.Errorf("save history: record exceeds reader limit")
	}
	record, err := savebundle.DecodeSaveHistoryRecordOf[T](data)
	if err != nil {
		return savebundle.SaveHistoryRecordOf[T]{}, err
	}
	if record.Federation != key.Federation || (record.Generation != nil) != key.HasGeneration ||
		(key.HasGeneration && *record.Generation != key.Generation) || file != fmt.Sprintf("%016x.json", record.Sequence) {
		return savebundle.SaveHistoryRecordOf[T]{}, fmt.Errorf("save history: record identity mismatch")
	}
	return record, nil
}

var _ savepoint.SaveHistoryStoreOf[int64] = (*FSStorageOf[int64])(nil)
var _ savepoint.UncommittedSaveDiscarder = (*FSStorageOf[int64])(nil)

func integerHistory[T core.LogicalTimeScalar]() bool { return T(1)/T(2) == 0 }

func (s *FSStorageOf[T]) nextHistorySequence(key savepoint.SaveHistoryKey, history savepoint.SaveHistoryOf[T], files []string) (uint64, error) {
	sequence := uint64(1)
	if len(files) > 0 {
		previous, err := s.readHistoryRecord(key, files[len(files)-1])
		if err != nil {
			return 0, err
		}
		if !history.HasSave {
			return 0, fmt.Errorf("save history: cannot reset existing execution history")
		}
		if previous.Sequence == math.MaxUint64 {
			return 0, fmt.Errorf("save history: sequence exhausted")
		}
		sequence = previous.Sequence + 1
	}
	return sequence, nil
}
