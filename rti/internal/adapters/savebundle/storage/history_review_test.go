package storage

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

func TestHistoryReviewRejectsMissingDecisionFields(t *testing.T) {
	for _, mode := range []string{"missing-name", "null-name", "missing-time", "missing-generation"} {
		t.Run(mode, func(t *testing.T) {
			store := historyStore(t, t.TempDir())
			key := savepoint.SaveHistoryKey{Federation: "review"}
			at := core.LogicalTime(17)
			if err := store.StoreSaveHistory(key, savepoint.SaveHistory{Known: true, HasSave: true, Name: "checkpoint", Time: &at}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(store.historyDir(key), "0000000000000001.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "missing-name":
				delete(fields, "name")
			case "null-name":
				fields["name"] = json.RawMessage("null")
			case "missing-time":
				delete(fields, "time")
			case "missing-generation":
				delete(fields, "generation")
			}
			data, err = json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := historyStore(t, store.Dir).LoadSaveHistory(key)
			if err == nil || got.Known {
				t.Errorf("damaged decision silently accepted as authoritative: %+v error=%v; record=%s", got, err, data)
			}
			if err := store.StoreSaveHistory(key, savepoint.SaveHistory{Known: true, HasSave: true, Name: "next"}); err == nil {
				t.Error("new decision appended over corrupt predecessor")
			}
		})
	}
}

func TestHistoryReviewCommittedLabelReusePreservesOriginalBundleAndHistory(t *testing.T) {
	store := historyStore(t, t.TempDir())
	m := newFSHistoryManager(t, store, store)
	at := core.LogicalTime(19)
	if err := fsHistorySave(t, m, "same", &at); err != nil {
		t.Fatal(err)
	}
	key := savepoint.StorageKey{Federation: "fed", Generation: 8, Label: "same"}
	read := func() []byte {
		r, err := store.ReaderFor(key)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	before := read()
	// Immutable bundle labels reject a second attempt, including changing a
	// timed checkpoint into an immediate one. This must not overwrite history.
	if err := fsHistorySave(t, m, "same", nil); err != nil {
		t.Fatal(err)
	}
	if m.QuerySaveState("fed", "same") != savepoint.StateNotSaved {
		t.Fatal("duplicate bundle reported as newly saved")
	}
	if !bytes.Equal(before, read()) {
		t.Fatal("same-label attempt overwrote committed bundle")
	}
	for _, manager := range []*savepoint.Manager{m, newFSHistoryManager(t, store, store)} {
		if err := manager.RecoverSaveHistory("fed"); err != nil {
			t.Fatal(err)
		}
		got := manager.QueryMOMMetrics("fed", 1)
		if !got.LastSaveExists || got.LastSaveName != "same" || got.LastSaveTime == nil || *got.LastSaveTime != at {
			t.Fatal("duplicate label rewrote earlier timed history", got)
		}
	}
}

func TestHistoryReviewCodecRequiresTypedV1Fields(t *testing.T) {
	base := map[string]json.RawMessage{
		"version": json.RawMessage("1"), "sequence": json.RawMessage("1"),
		"federation": json.RawMessage(`"review"`), "generation": json.RawMessage("null"),
		"boundary": json.RawMessage(`"reserved-success-fanout"`), "name": json.RawMessage(`"checkpoint"`),
		"time": json.RawMessage("17"),
	}
	for _, field := range []string{"version", "sequence", "federation", "generation", "boundary", "name", "time"} {
		for _, mode := range []string{"missing", "null", "wrong-type"} {
			if mode == "null" && (field == "generation" || field == "time") {
				continue
			}
			t.Run(field+"/"+mode, func(t *testing.T) {
				fields := make(map[string]json.RawMessage, len(base))
				for k, v := range base {
					fields[k] = v
				}
				switch mode {
				case "missing":
					delete(fields, field)
				case "null":
					fields[field] = json.RawMessage("null")
				case "wrong-type":
					fields[field] = json.RawMessage("true")
				}
				data, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				if got, err := savebundle.DecodeSaveHistoryRecord(data); err == nil {
					t.Fatalf("invalid v1 record accepted: %+v %s", got, data)
				}
			})
		}
	}
	for _, boundary := range []string{savebundle.SaveHistoryFreshBoundary, savebundle.SaveHistoryCommitBoundary} {
		record := savebundle.SaveHistoryRecord{Version: 1, Sequence: 1, Federation: "review", Boundary: boundary}
		data, err := savebundle.EncodeSaveHistoryRecord(record)
		if err != nil {
			t.Fatal(err)
		}
		got, err := savebundle.DecodeSaveHistoryRecord(data)
		if err != nil || got.Name != "" || got.Time != nil || got.Generation != nil {
			t.Fatalf("valid empty label/explicit nulls rejected: %+v %v", got, err)
		}
	}
	zeroGeneration, zeroTime := uint64(0), core.LogicalTime(0)
	data, err := savebundle.EncodeSaveHistoryRecord(savebundle.SaveHistoryRecord{Version: 1, Sequence: 1, Federation: "review", Boundary: savebundle.SaveHistoryCommitBoundary, Generation: &zeroGeneration, Time: &zeroTime})
	if err != nil {
		t.Fatal(err)
	}
	got, err := savebundle.DecodeSaveHistoryRecord(data)
	if err != nil || got.Generation == nil || *got.Generation != 0 || got.Time == nil || *got.Time != 0 {
		t.Fatalf("explicit zero identities lost: %+v %v", got, err)
	}
}

func TestHistoryReviewCodecRejectsDuplicateUnknownAndMalformedFields(t *testing.T) {
	for name, data := range map[string]string{
		"duplicate":             `{"version":1,"sequence":1,"federation":"review","generation":null,"boundary":"reserved-success-fanout","name":"first","name":"second","time":null}`,
		"case duplicate":        `{"version":1,"sequence":1,"federation":"review","generation":null,"boundary":"reserved-success-fanout","name":"first","NAME":"second","time":null}`,
		"escaped duplicate":     `{"version":1,"sequence":1,"federation":"review","generation":null,"boundary":"reserved-success-fanout","name":"first","\u006eame":"second","time":null}`,
		"unknown":               `{"version":1,"sequence":1,"federation":"review","generation":null,"boundary":"reserved-success-fanout","name":"first","extra":1,"time":null}`,
		"fractional generation": `{"version":1,"sequence":1,"federation":"review","generation":0.5,"boundary":"reserved-success-fanout","name":"first","time":null}`,
		"overflow generation":   `{"version":1,"sequence":1,"federation":"review","generation":18446744073709551616,"boundary":"reserved-success-fanout","name":"first","time":null}`,
		"overflow time":         `{"version":1,"sequence":1,"federation":"review","generation":null,"boundary":"reserved-success-fanout","name":"first","time":1e999}`,
		"null record":           `null`,
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := savebundle.DecodeSaveHistoryRecord([]byte(data)); err == nil {
				t.Fatalf("malformed record accepted: %+v", got)
			}
		})
	}
}
