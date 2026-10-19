package storage

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

func TestCommittedCheckpointLabelResolutionBothFamilies(t *testing.T) {
	for _, mode := range []string{"single", "uncommitted", "ambiguous", testCurrent, "corrupt", "identity", "factory", "time"} {
		t.Run("Float64/"+mode, func(t *testing.T) { checkpointLabelContract[core.LogicalTime](t, mode) })
		t.Run("Integer64/"+mode, func(t *testing.T) { checkpointLabelContract[int64](t, mode) })
	}
}

func checkpointLabelContract[T core.LogicalTimeScalar](t *testing.T, mode string) {
	t.Helper()
	s, err := NewFSStorageOf[T](t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(gen uint64, commit bool) {
		key := savepoint.StorageKey{Federation: "fed", Generation: gen, Label: "saved"}
		value := T(10)
		manifestGen := gen
		implementation := core.ScalarRepresentation[T]().String()
		if mode == "identity" {
			manifestGen++
		}
		if mode == "factory" {
			implementation = "unsupportedTimeFactory"
		}
		manifest := savepoint.ManifestOf[T]{Version: 2, Federation: key.Federation, FederationGeneration: &manifestGen, Label: key.Label, LogicalTimeImplementationName: implementation, SaveTime: &value}
		w, err := s.WriterFor(key)
		if err != nil {
			t.Fatal(err)
		}
		if err = savepoint.WriteBundleOf[T](w, manifest, nil); err != nil {
			t.Fatal(err)
		}
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
		if commit {
			if mode == "time" {
				value++
			}
			if err = s.StoreSaveHistory(savepoint.SaveHistoryKey{Federation: "fed", Generation: gen, HasGeneration: true}, savepoint.SaveHistoryOf[T]{Known: true, HasSave: true, Name: "saved", Time: &value}); err != nil {
				t.Fatal(err)
			}
		}
		if mode == "corrupt" {
			if err = os.WriteFile(s.bundlePathFor(key), []byte{1, 2, 3}, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(9, mode != "uncommitted")
	if mode == "ambiguous" || mode == testCurrent {
		write(11, true)
	}
	current := uint64(10)
	if mode == testCurrent {
		current = 9
	}
	restarted, err := NewFSStorageOf[T](s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restarted.ResolveCommittedCheckpointLabel(context.Background(), "fed", "saved", current)
	switch mode {
	case "single", testCurrent:
		if err != nil || got != 9 {
			t.Fatalf("exact old generation resolution: %d %v", got, err)
		}
	case "uncommitted":
		if !errors.Is(err, savepoint.ErrSaveBundleNotFound) {
			t.Fatalf("uncommitted bundle selected: %d %v", got, err)
		}
	default:
		if !errors.Is(err, savepoint.ErrSaveBundleIncompatible) {
			t.Fatalf("invalid source accepted: %d %v", got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := restarted.ResolveCommittedCheckpointLabel(ctx, "fed", "saved", current); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled resolver: %v", err)
	}
}

const testCurrent = "current"
