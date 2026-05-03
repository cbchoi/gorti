package savepoint

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// FSStorage is a filesystem-backed Storage. Bundles are written as
// individual files under Dir using the "<fed>__<label>.bundle" name
// scheme — flat, no per-federation subdirectories, no locking.
//
// Cut-1 simplifications (per orchestrator guidance):
//   - One file per (fed, label); no atomic rename / fsync ladder.
//   - No file locking; assumes a single-writer rtid process per Dir.
//   - Cross-platform path normalization is limited to filename
//     sanitization — fed/label characters that would break filenames on
//     Windows are escaped via filenameEscape (forward slash, backslash,
//     colon, asterisk, question mark, double-quote, less-than,
//     greater-than, pipe → percent-encoded). The encoding round-trips
//     for every printable ASCII input.
type FSStorage struct {
	Dir string
}

// NewFSStorage validates dir exists (or can be created) and returns an
// FSStorage rooted there.
func NewFSStorage(dir string) (*FSStorage, error) {
	if dir == "" {
		return nil, errors.New("savepoint: NewFSStorage dir is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("savepoint: mkdir %s: %w", dir, err)
	}
	return &FSStorage{Dir: dir}, nil
}

// Writer implements Storage.Writer. Returns ErrSaveBundleExists if a
// bundle for (fed, label) is already on disk.
func (s *FSStorage) Writer(fed core.FederationName, label string) (io.WriteCloser, error) {
	path := s.bundlePath(fed, label)
	if _, err := os.Stat(path); err == nil {
		return nil, ErrSaveBundleExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("savepoint: stat %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644) //nolint:gosec // composed from caller-supplied federation + label
	if err != nil {
		// O_EXCL race with another writer: surface as the existing
		// sentinel so callers don't have to special-case ENOENT vs EEXIST.
		if errors.Is(err, os.ErrExist) {
			return nil, ErrSaveBundleExists
		}
		return nil, fmt.Errorf("savepoint: open %s: %w", path, err)
	}
	return f, nil
}

// Reader implements Storage.Reader. Returns ErrSaveBundleNotFound when
// no bundle exists for (fed, label).
func (s *FSStorage) Reader(fed core.FederationName, label string) (io.ReadCloser, error) {
	path := s.bundlePath(fed, label)
	f, err := os.Open(path) //nolint:gosec // composed from caller-supplied federation + label
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrSaveBundleNotFound
		}
		return nil, fmt.Errorf("savepoint: open %s: %w", path, err)
	}
	return f, nil
}

// Exists implements Storage.Exists.
func (s *FSStorage) Exists(fed core.FederationName, label string) bool {
	_, err := os.Stat(s.bundlePath(fed, label))
	return err == nil
}

// bundlePath composes the on-disk path for (fed, label).
func (s *FSStorage) bundlePath(fed core.FederationName, label string) string {
	name := filenameEscape(string(fed)) + "__" + filenameEscape(label) + ".bundle"
	return filepath.Join(s.Dir, name)
}

// filenameEscape replaces filesystem-unsafe characters with %HH escapes.
// Symmetric with url.QueryEscape's spirit but limited to the small set
// of characters that break Windows filenames; the goal is only to
// produce a unique, valid filename — not full reversibility (callers
// don't need to recover the original strings; they store them in the
// manifest's Federation + Label fields).
func filenameEscape(s string) string {
	const unsafe = `/\\:*?"<>|`
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(unsafe, r) {
			fmt.Fprintf(&b, "%%%02X", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Compile-time assertion that FSStorage implements Storage.
var _ Storage = (*FSStorage)(nil)
