package eventlog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// WriterFactory builds a per-federation Writer on demand. Production wires
// a file-backed factory; tests typically pass a bytes.Buffer-backed
// factory so determinism + replay tests can compare byte streams without
// disk I/O. The factory MUST set opts.Sink before returning.
type WriterFactory func(opts WriterOptions) (*Writer, error)

// MultiplexOptions configures NewMultiplexWriter.
type MultiplexOptions struct {
	// Clock is forwarded into per-federation WriterOptions for header
	// CreatedAtNs stamping. MUST NOT be nil.
	Clock core.Clock

	// Mode is forwarded into per-federation WriterOptions.
	Mode core.Mode

	// Seed is forwarded into per-federation WriterOptions. Per-federation
	// seeds (one-per-federation seeding) are wired separately in cut-1;
	// MultiplexWriter applies the same seed to every per-federation writer.
	Seed uint64

	// Dir is the directory where the default file-backed factory writes
	// per-federation .log files. Required only when Factory is nil.
	Dir string

	// Factory builds per-federation Writers. Optional — when nil, the
	// multiplexer uses a default file-backed factory rooted at Dir.
	Factory WriterFactory
}

// MultiplexWriter implements core.EventLog over multiple per-federation
// Writers. The first Append for a federation lazily opens a Writer via
// the configured WriterFactory; subsequent calls reuse the same Writer.
//
// Per-federation seq counters are independent (each Writer tracks its
// own monotonic seq), which matches the on-disk file-per-federation
// layout: one .log file per federation, each starting at seq 1.
//
// Concurrency: a single mutex serializes Append/Sync/Close to keep the
// per-federation writer table consistent. Inside, each Writer is itself
// goroutine-unsafe (per W1B's contract), so the multiplexer's lock also
// guards Writer state.
type MultiplexWriter struct {
	mu      sync.Mutex
	opts    MultiplexOptions
	writers map[core.FederationName]*Writer
	closed  bool
}

// NewMultiplexWriter constructs a MultiplexWriter. Validates that Clock is
// non-nil and that either Factory or Dir is supplied.
func NewMultiplexWriter(opts MultiplexOptions) (*MultiplexWriter, error) {
	if opts.Clock == nil {
		return nil, errors.New("eventlog: MultiplexOptions.Clock is required (D-1: no time.Now)")
	}
	if opts.Factory == nil && opts.Dir == "" {
		return nil, errors.New("eventlog: MultiplexOptions: either Factory or Dir is required")
	}
	if opts.Factory == nil {
		opts.Factory = newFileFactory(opts.Dir)
	}
	return &MultiplexWriter{
		opts:    opts,
		writers: map[core.FederationName]*Writer{},
	}, nil
}

// Append implements core.EventLog. Routes to the per-federation Writer,
// lazily creating it on the first call for that federation.
func (m *MultiplexWriter) Append(ctx context.Context, fed core.FederationName, evt core.EventRecord) error {
	w, err := m.writerFor(fed)
	if err != nil {
		return err
	}
	return w.Append(ctx, fed, evt)
}

// Sync implements core.EventLog. Returns ErrFederationNotFound if no
// writer has been opened for fed (no Append observed yet).
func (m *MultiplexWriter) Sync(ctx context.Context, fed core.FederationName) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errMultiplexClosed
	}
	w, ok := m.writers[fed]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: federation %q has no open writer", core.ErrFederationNotFound, fed)
	}
	return w.Sync(ctx, fed)
}

// OpenReader implements core.EventLog. The path argument is interpreted
// as a federation name (the multiplexer's natural identifier), not a raw
// filesystem path. For the default file-backed factory, this opens the
// per-federation log file under Dir; for a custom factory, the
// multiplexer cannot recover the source bytes and returns an error.
func (m *MultiplexWriter) OpenReader(_ context.Context, path string) (core.EventLogReader, error) {
	if m.opts.Dir == "" {
		return nil, errors.New("eventlog: MultiplexWriter.OpenReader requires Dir (no file backing)")
	}
	logPath := filepath.Join(m.opts.Dir, path+".log")
	f, err := os.Open(logPath) //nolint:gosec // path is composed from caller-supplied federation name + Dir
	if err != nil {
		return nil, fmt.Errorf("eventlog: open %s: %w", logPath, err)
	}
	r, err := NewReader(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return r, nil
}

// Close flushes and releases every per-federation writer. Safe to call
// multiple times; subsequent Append/Sync return errMultiplexClosed.
func (m *MultiplexWriter) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	var firstErr error
	for fed, w := range m.writers {
		if err := w.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("eventlog: close federation %q: %w", fed, err)
		}
	}
	return firstErr
}

// writerFor returns the federation's Writer, creating it on first use.
func (m *MultiplexWriter) writerFor(fed core.FederationName) (*Writer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errMultiplexClosed
	}
	if w, ok := m.writers[fed]; ok {
		return w, nil
	}
	w, err := m.opts.Factory(WriterOptions{
		Federation: fed,
		Mode:       m.opts.Mode,
		Seed:       m.opts.Seed,
		Clock:      m.opts.Clock,
	})
	if err != nil {
		return nil, fmt.Errorf("eventlog: open federation %q: %w", fed, err)
	}
	m.writers[fed] = w
	return w, nil
}

// errMultiplexClosed is returned by Append/Sync after Close.
var errMultiplexClosed = errors.New("eventlog: MultiplexWriter is closed")

// newFileFactory returns a WriterFactory that creates one .log file per
// federation under dir. The file is opened with O_CREATE|O_TRUNC|O_WRONLY
// — production callers control the lifecycle (one rtid run per federation
// log file), so truncation is acceptable on open.
func newFileFactory(dir string) WriterFactory {
	return func(opts WriterOptions) (*Writer, error) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("eventlog: mkdir %s: %w", dir, err)
		}
		path := filepath.Join(dir, string(opts.Federation)+".log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644) //nolint:gosec // composed from caller-supplied federation name + Dir
		if err != nil {
			return nil, fmt.Errorf("eventlog: open %s: %w", path, err)
		}
		opts.Sink = f
		w, err := NewWriter(opts)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		return w, nil
	}
}

// Compile-time assertion that MultiplexWriter implements core.EventLog.
var _ core.EventLog = (*MultiplexWriter)(nil)
