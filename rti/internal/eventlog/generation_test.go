package eventlog

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

func TestMultiplexWriter_DistinctSameNameGenerations(t *testing.T) {
	dir := t.TempDir()
	fed := core.FederationName("same/name")
	generation := uint64(7)
	mode := core.ModeBestEffort
	seed := uint64(17)
	mux, err := NewMultiplexWriter(MultiplexOptions{
		Clock: core.NewFakeClock(time.Unix(0, 0)),
		Dir:   dir,
		Metadata: func(got core.FederationName) (uint64, core.Mode, uint64, bool) {
			return generation, mode, seed, got == fed
		},
	})
	if err != nil {
		t.Fatalf("NewMultiplexWriter: %v", err)
	}
	defer func() { _ = mux.Close() }()

	if err := mux.Append(context.Background(), fed, &writerEvent{}); err != nil {
		t.Fatalf("Append generation 7: %v", err)
	}
	if err := mux.CloseFederation(fed); err != nil {
		t.Fatalf("CloseFederation generation 7: %v", err)
	}

	generation = 8
	mode = core.ModeVerbose
	seed = 18
	if err := mux.Append(context.Background(), fed, &writerEvent{}); err != nil {
		t.Fatalf("Append generation 8: %v", err)
	}
	if err := mux.CloseFederation(fed); err != nil {
		t.Fatalf("CloseFederation generation 8: %v", err)
	}

	fedDir := hex.EncodeToString([]byte(fed))
	path7 := filepath.Join(dir, fedDir, "0000000000000007.log")
	path8 := filepath.Join(dir, fedDir, "0000000000000008.log")
	for _, path := range []string{path7, path8} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Stat(%s): %v", path, err)
		}
	}

	rdr, err := mux.OpenReader(context.Background(), string(fed))
	if err != nil {
		t.Fatalf("OpenReader current generation: %v", err)
	}
	defer func() { _ = rdr.Close() }()
	hdr := rdr.Header()
	if hdr.Generation != 8 || hdr.Mode != core.ModeVerbose || hdr.Seed != 18 {
		t.Errorf("current header = generation %d, mode %d, seed %d; want 8, %d, 18",
			hdr.Generation, hdr.Mode, hdr.Seed, core.ModeVerbose)
	}
}

func TestMultiplexWriter_ExistingGenerationIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	fed := core.FederationName("stable")
	metadata := func(got core.FederationName) (uint64, core.Mode, uint64, bool) {
		return 41, core.ModeVerbose, 123, got == fed
	}
	mux, err := NewMultiplexWriter(MultiplexOptions{
		Clock:    core.NewFakeClock(time.Unix(0, 0)),
		Dir:      dir,
		Metadata: metadata,
	})
	if err != nil {
		t.Fatalf("NewMultiplexWriter: %v", err)
	}
	defer func() { _ = mux.Close() }()

	if err := mux.Append(context.Background(), fed, &writerEvent{}); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	if err := mux.CloseFederation(fed); err != nil {
		t.Fatalf("CloseFederation: %v", err)
	}
	path := filepath.Join(dir, hex.EncodeToString([]byte(fed)), "0000000000000029.log")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile before reopen: %v", err)
	}

	err = mux.Append(context.Background(), fed, &writerEvent{})
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("second Append error = %v, want os.ErrExist", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after reopen: %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("existing generation log changed after exclusive reopen failed")
	}
}
