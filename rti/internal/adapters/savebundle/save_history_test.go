package savebundle

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestSaveHistoryCodecStrictVersionedDecisions(t *testing.T) {
	for _, boundary := range []string{SaveHistoryFreshBoundary, SaveHistoryCommitBoundary} {
		record := SaveHistoryRecord{Version: 1, Sequence: 1, Federation: "fed", Boundary: boundary}
		if boundary == SaveHistoryCommitBoundary {
			at := core.LogicalTime(0)
			record.Time = &at
		}
		data, err := EncodeSaveHistoryRecord(record)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeSaveHistoryRecord(data)
		if err != nil || got.Boundary != boundary || (got.Time != nil) != (record.Time != nil) {
			t.Fatalf("round trip: %+v %v", got, err)
		}
	}
	valid := `{"version":1,"sequence":1,"federation":"fed","generation":null,"boundary":"reserved-success-fanout","name":"ok","time":0}`
	for name, data := range map[string]string{
		"version":         strings.Replace(valid, `"version":1`, `"version":2`, 1),
		"sequence":        strings.Replace(valid, `"sequence":1`, `"sequence":0`, 1),
		"boundary":        strings.Replace(valid, `reserved-success-fanout`, `sdk-callbacks-executed`, 1),
		"duplicate":       strings.Replace(valid, `"time":0`, `"time":0,"TIME":1`, 1),
		"unknown":         strings.Replace(valid, `"time":0`, `"time":0,"extra":true`, 1),
		"negative-time":   strings.Replace(valid, `"time":0`, `"time":-1`, 1),
		"overflow-time":   strings.Replace(valid, `"time":0`, `"time":1e999`, 1),
		"fresh-with-save": strings.Replace(valid, `reserved-success-fanout`, `fresh-execution`, 1),
		"trailing":        valid + `{}`,
		"truncated":       valid[:len(valid)-1],
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeSaveHistoryRecord([]byte(data)); err == nil {
				t.Fatal("corrupt history accepted")
			}
		})
	}
	if _, err := DecodeSaveHistoryRecord(bytes.ReplaceAll([]byte(valid), []byte("ok"), []byte{0xff})); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}
