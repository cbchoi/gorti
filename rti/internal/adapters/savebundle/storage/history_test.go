package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

func historyStore(t *testing.T, dir string) *FSStorage {
	t.Helper()
	s, err := NewFSStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFSHistoryRestartIdentityAndAbsentLegacy(t *testing.T) {
	dir := t.TempDir()
	store := historyStore(t, dir)
	keys := []savepoint.SaveHistoryKey{
		{Federation: "fed"},
		{Federation: "fed", HasGeneration: true},
		{Federation: "fed", HasGeneration: true, Generation: 1},
		{Federation: "fed", HasGeneration: true, Generation: 2},
		{Federation: "../other", HasGeneration: true, Generation: 1},
	}
	for i, key := range keys {
		got, err := store.LoadSaveHistory(key)
		if err != nil || got.Known {
			t.Fatalf("absent history invented: %+v %v", got, err)
		}
		if err := store.StoreSaveHistory(key, savepoint.SaveHistory{Known: true}); err != nil {
			t.Fatal(err)
		}
		at := core.LogicalTime(i)
		history := savepoint.SaveHistory{Known: true, HasSave: true, Name: fmt.Sprintf("checkpoint-%d", i), Time: &at}
		if err := store.StoreSaveHistory(key, history); err != nil {
			t.Fatal(err)
		}
		if err := store.StoreSaveHistory(key, savepoint.SaveHistory{Known: true}); err == nil {
			t.Fatal("history erased by initialization")
		}
		got, err = historyStore(t, dir).LoadSaveHistory(key)
		if err != nil || !reflect.DeepEqual(got, history) {
			t.Fatalf("restart: %+v %v", got, err)
		}
	}
	legacy := savepoint.SaveHistoryKey{Federation: "legacy"}
	w, err := store.Writer("legacy", "snapshot-is-not-completion")
	if err != nil {
		t.Fatal(err)
	}
	if err := savepoint.WriteBundle(w, savepoint.Manifest{Federation: "legacy", Label: "snapshot-is-not-completion"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := store.LoadSaveHistory(legacy); err != nil || got.Known {
		t.Fatalf("legacy snapshot became history: %+v %v", got, err)
	}
}

func TestFSHistoryRejectsCorruptionAndIgnoresUnpublishedTemp(t *testing.T) {
	for _, mode := range []string{"truncated", "wrong-generation", "duplicate", "unknown-version", "wrong-sequence", "oversized", "temporary"} {
		t.Run(mode, func(t *testing.T) {
			store := historyStore(t, t.TempDir())
			key := savepoint.SaveHistoryKey{Federation: "fed", HasGeneration: true, Generation: 9}
			if err := store.StoreSaveHistory(key, savepoint.SaveHistory{Known: true, HasSave: true, Name: testPrevious}); err != nil {
				t.Fatal(err)
			}
			generation := uint64(9)
			record := savebundle.SaveHistoryRecord{Version: 1, Sequence: 2, Federation: "fed", Generation: &generation, Boundary: savebundle.SaveHistoryCommitBoundary, Name: "next"}
			data, err := savebundle.EncodeSaveHistoryRecord(record)
			if err != nil {
				t.Fatal(err)
			}
			name := "0000000000000002.json"
			switch mode {
			case "truncated":
				data = data[:len(data)-1]
			case "wrong-generation":
				generation = 10
				data, err = savebundle.EncodeSaveHistoryRecord(record)
			case "duplicate":
				data = []byte(`{"version":1,"version":1}`)
			case "unknown-version":
				data = []byte(`{"version":2}`)
			case "wrong-sequence":
				name = "0000000000000003.json"
			case "oversized":
				data = make([]byte, maxSaveHistoryBytes+1)
			case "temporary":
				name = ".0000000000000002.json.tmp-interrupted"
				data = []byte("partial")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(store.historyDir(key), name), data, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := store.LoadSaveHistory(key)
			if mode == "temporary" {
				if err != nil || got.Name != testPrevious {
					t.Fatalf("temp adopted: %+v %v", got, err)
				}
				return
			}
			if err == nil || got.Known {
				t.Fatalf("corrupt latest record silently fell back: %+v %v", got, err)
			}
			if err := store.StoreSaveHistory(key, savepoint.SaveHistory{Known: true, HasSave: true, Name: "retry"}); err == nil {
				t.Fatal("corrupt history overwritten")
			}
		})
	}
}

type historyTestOutbox struct{ sent []core.OutboundEvent }

func (o *historyTestOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, e core.OutboundEvent) error {
	o.sent = append(o.sent, e)
	return nil
}
func (o *historyTestOutbox) Reserve(_ context.Context, _ core.FederationName, ds []core.OutboxDelivery) (core.OutboxReservation, error) {
	return &historyTestReservation{out: o, ds: ds}, nil
}

type historyTestReservation struct {
	out  *historyTestOutbox
	ds   []core.OutboxDelivery
	done bool
}

func (r *historyTestReservation) Commit() error {
	if !r.done {
		r.done = true
		for _, d := range r.ds {
			r.out.sent = append(r.out.sent, d.Event)
		}
	}
	return nil
}
func (r *historyTestReservation) Release() { r.done = true }

type historyFaultStore struct {
	*FSStorage
	fail  bool
	crash string
}

func (s *historyFaultStore) StoreSaveHistory(key savepoint.SaveHistoryKey, h savepoint.SaveHistory) error {
	if s.fail {
		return errors.New("injected history write failure")
	}
	if h.Name == testCrash && s.crash == "before" {
		os.Exit(73)
	}
	err := s.FSStorage.StoreSaveHistory(key, h)
	if err == nil && h.Name == testCrash && s.crash == "after" {
		os.Exit(73)
	}
	return err
}

func newFSHistoryManager(t *testing.T, store *FSStorage, history savepoint.SaveHistoryStore) *savepoint.Manager {
	t.Helper()
	m, err := savepoint.New(savepoint.Options{
		Outbox: &historyTestOutbox{}, BundleStore: store, HistoryStore: history,
		Members:    func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
		Generation: func(core.FederationName) (uint64, bool) { return 8, true },
		ScheduledTime: func(core.FederationName, core.LogicalTime, []core.FederateHandle) (func() (bool, error), func(), error) {
			return func() (bool, error) { return true, nil }, func() {}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func fsHistorySave(t *testing.T, m *savepoint.Manager, label string, at *core.LogicalTime) error {
	t.Helper()
	ctx := context.Background()
	if err := m.RequestFederationSave(ctx, "fed", label, at); err != nil {
		return err
	}
	if at != nil {
		if err := m.ProgressScheduledSave(ctx, "fed"); err != nil {
			return err
		}
	}
	if err := m.FederateSaveBegun(ctx, "fed", 1); err != nil {
		return err
	}
	return m.FederateSaveComplete(ctx, "fed", 1)
}

func TestFSHistoryManagerRestartTimedImmediateAndEmptyLabel(t *testing.T) {
	dir := t.TempDir()
	store := historyStore(t, dir)
	m := newFSHistoryManager(t, store, store)
	if err := m.InitializeSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"timed", "immediate", ""} {
		var at *core.LogicalTime
		if label != "immediate" {
			value := core.LogicalTime(0)
			at = &value
		}
		if err := fsHistorySave(t, m, label, at); err != nil {
			t.Fatal(err)
		}
		store = historyStore(t, dir)
		m = newFSHistoryManager(t, store, store)
		if err := m.RecoverSaveHistory("fed"); err != nil {
			t.Fatal(err)
		}
		got := m.QueryMOMMetrics("fed", 1)
		if !got.LastSaveHistoryKnown || !got.LastSaveExists || got.LastSaveName != label || (got.LastSaveTime != nil) != (at != nil) {
			t.Fatalf("restart: %+v", got)
		}
		if at != nil && *got.LastSaveTime != *at {
			t.Fatalf("restart time: %v", *got.LastSaveTime)
		}
	}
}

func TestFSHistoryPersistenceFailureAllowsSameLabelRetry(t *testing.T) {
	store := historyStore(t, t.TempDir())
	fault := &historyFaultStore{FSStorage: store}
	m := newFSHistoryManager(t, store, fault)
	if err := fsHistorySave(t, m, testPrevious, nil); err != nil {
		t.Fatal(err)
	}
	fault.fail = true
	if err := fsHistorySave(t, m, "retry", nil); err == nil {
		t.Fatal("history failure hidden")
	}
	if got := m.QueryMOMMetrics("fed", 1); got.LastSaveName != testPrevious {
		t.Fatalf("failed write changed history: %+v", got)
	}
	if store.ExistsFor(savepoint.StorageKey{Federation: "fed", Generation: 8, Label: "retry"}) {
		t.Fatal("uncommitted candidate blocks same-label retry")
	}
	fault.fail = false
	if err := fsHistorySave(t, m, "retry", nil); err != nil {
		t.Fatal(err)
	}
	key := savepoint.SaveHistoryKey{Federation: "fed", Generation: 8, HasGeneration: true}
	if err := store.DiscardUncommittedSave(key, testPrevious); err == nil {
		t.Fatal("discarded a prior committed save")
	}
	if !store.ExistsFor(savepoint.StorageKey{Federation: "fed", Generation: 8, Label: testPrevious}) {
		t.Fatal("prior bundle lost")
	}
}

// This helper deliberately exits without defers to exercise actual process
// interruption on both sides of the durable decision, using the real FS adapter.
func TestFSHistoryCrashHelper(t *testing.T) {
	dir := os.Getenv("GORTI_HISTORY_CRASH_DIR")
	if dir == "" {
		return
	}
	store := historyStore(t, dir)
	fault := &historyFaultStore{FSStorage: store, crash: os.Getenv("GORTI_HISTORY_CRASH_WINDOW")}
	m := newFSHistoryManager(t, store, fault)
	if err := m.RecoverSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	if err := fsHistorySave(t, m, testCrash, nil); err != nil {
		t.Fatal(err)
	}
	t.Fatal("did not enter crash window")
}

func TestFSHistoryActualProcessCrashWindows(t *testing.T) {
	for _, window := range []string{"before", "after"} {
		t.Run(window, func(t *testing.T) {
			dir := t.TempDir()
			store := historyStore(t, dir)
			m := newFSHistoryManager(t, store, store)
			if err := fsHistorySave(t, m, testPrevious, nil); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestFSHistoryCrashHelper$")
			cmd.Env = append(os.Environ(), "GORTI_HISTORY_CRASH_DIR="+dir, "GORTI_HISTORY_CRASH_WINDOW="+window)
			output, err := cmd.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 73 {
				t.Fatalf("child: %v %s", err, output)
			}
			store = historyStore(t, dir)
			m = newFSHistoryManager(t, store, store)
			if err := m.RecoverSaveHistory("fed"); err != nil {
				t.Fatal(err)
			}
			want := testPrevious
			if window == "after" {
				want = testCrash
			}
			if got := m.QueryMOMMetrics("fed", 1); !got.LastSaveHistoryKnown || got.LastSaveName != want {
				t.Fatalf("restart after crash: %+v", got)
			}
			if !store.ExistsFor(savepoint.StorageKey{Federation: "fed", Generation: 8, Label: testCrash}) {
				t.Fatal("crash did not occur after snapshot persistence")
			}
		})
	}
}

func TestFSHistoryConcurrentReaderWriterInstances(t *testing.T) {
	dir := t.TempDir()
	key := savepoint.SaveHistoryKey{Federation: "fed", HasGeneration: true, Generation: 3}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := &FSStorage{Dir: dir}
			for j := 0; j < 15; j++ {
				if err := s.StoreSaveHistory(key, savepoint.SaveHistory{Known: true, HasSave: true, Name: fmt.Sprintf("%d-%d", i, j)}); err != nil {
					t.Error(err)
					return
				}
				if h, err := s.LoadSaveHistory(key); err != nil || !h.Known || !h.HasSave {
					t.Errorf("concurrent read: %+v %v", h, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

const testCrash = "crash"

const testPrevious = "previous"
