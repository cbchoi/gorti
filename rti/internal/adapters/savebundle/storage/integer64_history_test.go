package storage

import (
	"math"
	"reflect"
	"testing"

	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

func TestInteger64HistoryRestartPrecisionAndFloatIsolation(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFSStorageOf[int64](dir)
	if err != nil {
		t.Fatal(err)
	}
	key := savepoint.SaveHistoryKey{Federation: "fed", HasGeneration: true, Generation: 9}
	if err := store.StoreSaveHistory(key, savepoint.SaveHistoryOf[int64]{Known: true}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []int64{1<<53 + 1, 1<<53 + 3, math.MaxInt64} {
		history := savepoint.SaveHistoryOf[int64]{Known: true, HasSave: true, Name: "saved", Time: &value}
		if err := store.StoreSaveHistory(key, history); err != nil {
			t.Fatal(err)
		}
		restarted, err := NewFSStorageOf[int64](dir)
		if err != nil {
			t.Fatal(err)
		}
		got, err := restarted.LoadSaveHistory(key)
		if err != nil || !reflect.DeepEqual(got, history) {
			t.Fatalf("restart changed int64: %+v %v", got, err)
		}
	}
	legacy, err := NewFSStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := legacy.LoadSaveHistory(key); err != nil || got.Known {
		t.Fatalf("integer record loaded as float: %+v %v", got, err)
	}
	if err := legacy.StoreSaveHistory(key, savepoint.SaveHistory{Known: true}); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadSaveHistory(key)
	if err != nil || got.Time == nil || *got.Time != math.MaxInt64 {
		t.Fatalf("float initialization overwrote integer history: %+v %v", got, err)
	}
}
