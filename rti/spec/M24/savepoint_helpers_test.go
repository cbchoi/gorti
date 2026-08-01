// Test helpers for M24 W3 savepoint abort tests.

package m24spec

import (
	"errors"
	"io"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/savepoint"
)

// nullStorage is the smallest savepoint.Storage impl needed for the
// abort-not-in-progress tests; the abort path doesn't actually
// touch storage.
type nullStorage struct{}

func (nullStorage) Writer(_ core.FederationName, _ string) (io.WriteCloser, error) {
	return nil, errors.New("nullStorage: write not supported")
}
func (nullStorage) Reader(_ core.FederationName, _ string) (io.ReadCloser, error) {
	return nil, errors.New("nullStorage: read not supported")
}
func (nullStorage) Exists(_ core.FederationName, _ string) bool { return false }

func newSavepointMgr(t *testing.T) *savepoint.Manager {
	t.Helper()
	mgr, err := savepoint.New(savepoint.Options{
		Outbox:      nopOutbox{},
		BundleStore: nullStorage{},
	})
	if err != nil {
		t.Fatalf("savepoint.New: %v", err)
	}
	return mgr
}
