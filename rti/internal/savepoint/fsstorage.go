package savepoint

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// FSStorage is a filesystem-backed Storage. Legacy bundles retain the flat
// "<fed>__<label>.bundle" layout. Generation-aware bundles use
// "v2/<hex-fed>/<gen16>/<hex-label>.bundle".
type FSStorage struct {
	Dir string
}

// Serializing the existence check and rename prevents two FSStorage instances
// in this process from replacing the same final path on platforms where rename
// permits replacement. The RTI still assumes one writer process per save dir.
var fsPublishMu gosync.Mutex

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

// Writer implements legacy Storage.Writer.
func (s *FSStorage) Writer(fed core.FederationName, label string) (io.WriteCloser, error) {
	return s.newWriter(s.bundlePath(fed, label))
}

// WriterFor opens an atomically published generation-aware bundle writer.
func (s *FSStorage) WriterFor(key StorageKey) (BundleWriter, error) {
	return s.newWriter(s.bundlePathFor(key))
}

func (s *FSStorage) newWriter(finalPath string) (*fsBundleWriter, error) {
	if _, err := os.Stat(finalPath); err == nil {
		return nil, ErrSaveBundleExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("savepoint: stat %s: %w", finalPath, err)
	}

	dir := filepath.Dir(finalPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("savepoint: mkdir %s: %w", dir, err)
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(finalPath)+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("savepoint: create temp bundle for %s: %w", finalPath, err)
	}
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return nil, fmt.Errorf("savepoint: chmod temp bundle for %s: %w", finalPath, err)
	}
	return &fsBundleWriter{
		file:      temp,
		tempPath:  temp.Name(),
		finalPath: finalPath,
	}, nil
}

// Reader implements legacy Storage.Reader.
func (s *FSStorage) Reader(fed core.FederationName, label string) (io.ReadCloser, error) {
	return openBundle(s.bundlePath(fed, label))
}

// ReaderFor opens a bundle from one exact federation generation.
func (s *FSStorage) ReaderFor(key StorageKey) (io.ReadCloser, error) {
	return openBundle(s.bundlePathFor(key))
}

func openBundle(path string) (io.ReadCloser, error) {
	f, err := os.Open(path) //nolint:gosec // path is rooted beneath the configured save dir
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrSaveBundleNotFound
		}
		return nil, fmt.Errorf("savepoint: open %s: %w", path, err)
	}
	return f, nil
}

// Exists implements legacy Storage.Exists.
func (s *FSStorage) Exists(fed core.FederationName, label string) bool {
	return bundleExists(s.bundlePath(fed, label))
}

// ExistsFor reports whether an exact generation-keyed bundle is published.
func (s *FSStorage) ExistsFor(key StorageKey) bool {
	return bundleExists(s.bundlePathFor(key))
}

func bundleExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// bundlePath composes the legacy on-disk path for (fed, label).
func (s *FSStorage) bundlePath(fed core.FederationName, label string) string {
	name := filenameEscape(string(fed)) + "__" + filenameEscape(label) + ".bundle"
	return filepath.Join(s.Dir, name)
}

// bundlePathFor composes the v2 generation-aware path. Hex encoding the raw
// UTF-8 bytes is injective and leaves no path separators or platform metacharacters.
func (s *FSStorage) bundlePathFor(key StorageKey) string {
	return filepath.Join(
		s.Dir,
		"v2",
		hexComponent(string(key.Federation)),
		fmt.Sprintf("%016x", key.Generation),
		hexComponent(key.Label)+".bundle",
	)
}

func hexComponent(value string) string {
	return hex.EncodeToString([]byte(value))
}

// filenameEscape retains the legacy layout while escaping '%' itself so the
// old percent encoding is injective (for example, "/" cannot collide with the
// literal string "%2F").
func filenameEscape(value string) string {
	const unsafe = `%/\\:*?"<>|`
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(unsafe, r) {
			fmt.Fprintf(&b, "%%%02X", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// fsBundleWriter keeps the final path absent until Close has successfully
// synced and closed the temporary file and atomically renamed it into place.
type fsBundleWriter struct {
	file      *os.File
	tempPath  string
	finalPath string
	writeErr  error
	closeErr  error
	done      bool
}

func (w *fsBundleWriter) Write(p []byte) (int, error) {
	if w.done || w.file == nil {
		return 0, os.ErrClosed
	}
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	n, err := w.file.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.writeErr = err
	}
	return n, err
}

func (w *fsBundleWriter) Close() error {
	if w.done {
		return w.closeErr
	}
	w.done = true
	if w.writeErr != nil {
		w.closeErr = w.writeErr
		w.discardTemp()
		return w.closeErr
	}
	if err := w.file.Sync(); err != nil {
		w.closeErr = fmt.Errorf("sync temp bundle: %w", err)
		w.discardTemp()
		return w.closeErr
	}
	if err := w.file.Close(); err != nil {
		w.closeErr = fmt.Errorf("close temp bundle: %w", err)
		w.file = nil
		_ = os.Remove(w.tempPath)
		w.tempPath = ""
		return w.closeErr
	}
	w.file = nil

	fsPublishMu.Lock()
	defer fsPublishMu.Unlock()
	if _, err := os.Stat(w.finalPath); err == nil {
		w.closeErr = ErrSaveBundleExists
	} else if !errors.Is(err, os.ErrNotExist) {
		w.closeErr = fmt.Errorf("stat final bundle: %w", err)
	} else if err := os.Rename(w.tempPath, w.finalPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			w.closeErr = ErrSaveBundleExists
		} else {
			w.closeErr = fmt.Errorf("rename temp bundle: %w", err)
		}
	}
	if w.closeErr != nil {
		_ = os.Remove(w.tempPath)
	} else {
		w.tempPath = ""
	}
	return w.closeErr
}

func (w *fsBundleWriter) Abort() error {
	if w.done {
		return nil
	}
	w.done = true
	w.discardTemp()
	return nil
}

func (w *fsBundleWriter) discardTemp() {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	if w.tempPath != "" {
		_ = os.Remove(w.tempPath)
		w.tempPath = ""
	}
}

var (
	_ Storage           = (*FSStorage)(nil)
	_ GenerationStorage = (*FSStorage)(nil)
	_ BundleWriter      = (*fsBundleWriter)(nil)
)
