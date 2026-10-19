package server

import (
	"bytes"
	"os"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/eventlog"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type fakeOutboundEvent struct{ seq uint64 }

func (e *fakeOutboundEvent) Seq() uint64 { return e.seq }

func openTestReader(t *testing.T, path string) (*eventlog.Reader, error) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return eventlog.NewReader(f)
}

func readFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path) //nolint:gosec // path constructed in test from t.TempDir
}

func equalBytes(a, b []byte) bool {
	return bytes.Equal(a, b)
}

func testRealClock() core.Clock {
	return core.NewRealClock()
}
