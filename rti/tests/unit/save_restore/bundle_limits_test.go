package save_restore_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

func TestWriteBundleRejectsUnreadableManifest(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		t.Run(map[bool]string{false: "version", true: "size"}[oversized], func(t *testing.T) {
			var dst bytes.Buffer
			manifest := savepoint.Manifest{Version: 99, Federation: "fed", Label: savedStateValue}
			if oversized {
				manifest.Version = 1
				manifest.ManagerSnapshots = map[string][]byte{"large": make([]byte, 13*1024*1024)}
			}
			if err := savepoint.WriteBundle(&dst, manifest, nil); err == nil {
				t.Fatalf("unreadable manifest accepted; oversized=%v", oversized)
			}
			if dst.Len() != 0 {
				t.Fatal("invalid image wrote a partial bundle")
			}
		})
	}
}

type shortBundleWriter struct{}

func (shortBundleWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }

func TestWriteBundleRejectsShortWrites(t *testing.T) {
	err := savepoint.WriteBundle(shortBundleWriter{}, savepoint.Manifest{Federation: "fed", Label: savedStateValue}, nil)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("write error=%v", err)
	}
}
