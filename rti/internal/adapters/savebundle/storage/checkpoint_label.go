package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

func (s *FSStorageOf[T]) ResolveCommittedCheckpointLabel(ctx context.Context, fed core.FederationName, label string, current uint64) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	historyPublishMu.Lock()
	defer historyPublishMu.Unlock()
	candidates, err := s.checkpointCandidates(ctx, fed, label)
	if err != nil {
		return 0, err
	}
	selected, err := selectCheckpointGeneration(candidates, current)
	if err != nil {
		return 0, err
	}
	if err := s.validateCheckpointBundle(fed, label, selected, candidates[selected]); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return selected, nil
}

func sameCheckpointTime[T core.LogicalTimeScalar](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

var _ core.CommittedCheckpointLabelResolver = (*FSStorageOf[int64])(nil)
var _ core.CommittedCheckpointLabelResolver = (*FSStorageOf[core.LogicalTime])(nil)

func (s *FSStorageOf[T]) checkpointCandidates(ctx context.Context, fed core.FederationName, label string) (map[uint64]*T, error) {
	root := filepath.Dir(s.historyDir(savepoint.SaveHistoryKey{Federation: fed, HasGeneration: true}))
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, savepoint.ErrSaveBundleNotFound
	}
	if err != nil {
		return nil, err
	}
	candidates := make(map[uint64]*T)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Name() == "legacy" {
			continue
		}
		generation, err := strconv.ParseUint(entry.Name(), 16, 64)
		if err != nil || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || entry.Name() != fmt.Sprintf("%016x", generation) {
			return nil, fmt.Errorf("%w: malformed checkpoint execution directory", savepoint.ErrSaveBundleIncompatible)
		}
		if err := s.collectCheckpointDecisions(fed, label, generation, candidates); err != nil {
			return nil, err
		}
	}
	return candidates, nil
}

func selectCheckpointGeneration[T core.LogicalTimeScalar](candidates map[uint64]*T, current uint64) (uint64, error) {

	selected := current
	if _, exists := candidates[current]; !exists {
		if len(candidates) == 0 {
			return 0, savepoint.ErrSaveBundleNotFound
		}
		if len(candidates) != 1 {
			return 0, fmt.Errorf("%w: checkpoint label is ambiguous across executions", savepoint.ErrSaveBundleIncompatible)
		}
		for generation := range candidates {
			selected = generation
		}
	}
	return selected, nil
}

func (s *FSStorageOf[T]) collectCheckpointDecisions(fed core.FederationName, label string, generation uint64, candidates map[uint64]*T) error {
	key := savepoint.SaveHistoryKey{Federation: fed, HasGeneration: true, Generation: generation}
	files, err := s.historyFiles(key)
	if err != nil {
		return fmt.Errorf("%w: checkpoint history: %v", savepoint.ErrSaveBundleIncompatible, err)
	}
	for _, file := range files {
		info, err := os.Lstat(filepath.Join(s.historyDir(key), file))
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: invalid checkpoint history entry", savepoint.ErrSaveBundleIncompatible)
		}
		record, err := s.readHistoryRecord(key, file)
		if err != nil {
			return fmt.Errorf("%w: checkpoint history: %v", savepoint.ErrSaveBundleIncompatible, err)
		}
		if record.Boundary == savebundle.SaveHistoryCommitBoundary && record.Name == label {
			// The current bundle writer never replaces a committed label.
			// Different times therefore cannot describe one published bundle.
			if previous, exists := candidates[generation]; exists && !sameCheckpointTime(previous, record.Time) {
				return fmt.Errorf("%w: contradictory checkpoint decisions", savepoint.ErrSaveBundleIncompatible)
			}
			candidates[generation] = record.Time
		}
	}
	return nil
}

func (s *FSStorageOf[T]) validateCheckpointBundle(fed core.FederationName, label string, selected uint64, at *T) error {
	reader, err := s.ReaderFor(savepoint.StorageKey{Federation: fed, Generation: selected, Label: label})
	if err != nil {
		return fmt.Errorf("%w: committed checkpoint bundle unavailable: %v", savepoint.ErrSaveBundleIncompatible, err)
	}
	manifest, _, err := savepoint.ReadBundleOf[T](reader)
	closeErr := reader.Close()
	if err != nil || closeErr != nil {
		return fmt.Errorf("%w: committed checkpoint bundle corrupt: %v", savepoint.ErrSaveBundleIncompatible, errors.Join(err, closeErr))
	}
	if manifest.Federation != fed || manifest.Label != label || manifest.FederationGeneration == nil || *manifest.FederationGeneration != selected || !sameCheckpointTime(manifest.SaveTime, at) {
		return fmt.Errorf("%w: checkpoint decision/bundle identity mismatch", savepoint.ErrSaveBundleIncompatible)
	}
	implementation := manifest.LogicalTimeImplementationName
	if implementation == "" && !integerHistory[T]() {
		implementation = core.ScalarRepresentation[T]().String()
	}
	if implementation != core.ScalarRepresentation[T]().String() {
		return fmt.Errorf("%w: checkpoint time factory mismatch", savepoint.ErrSaveBundleIncompatible)
	}
	return nil
}
