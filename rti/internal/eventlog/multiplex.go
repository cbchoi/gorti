package eventlog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/protobuf/proto"
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
// Concurrency: a single mutex serializes table mutations (lazy writer
// creation in writerFor and the close walk in Close) so the
// per-federation writer table stays consistent. Append itself releases
// the multiplexer mutex before delegating to the per-federation
// Writer, which has its own internal mutex (added in M6 W1B) covering
// nextSeq + sink writes. Sync also delegates without holding the
// multiplexer lock for the same reason.
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
//
// The Writer requires its EventRecord arg to be a non-nil pointer (so
// reflection-based seq assignment is addressable). The federation
// manager and object registry currently emit value-typed event records
// in some paths; ensurePointerRecord wraps any non-pointer record in an
// addressable copy so the Writer's reflection succeeds. This is a
// production-wiring concession local to the multiplexer — the W1B
// Writer's contract stays unchanged.
func (m *MultiplexWriter) Append(ctx context.Context, fed core.FederationName, evt core.EventRecord) error {
	w, err := m.writerFor(fed)
	if err != nil {
		return err
	}
	return w.Append(ctx, fed, ensurePointerRecord(evt))
}

// ensurePointerRecord normalizes the inbound record into a shape the
// W1B Writer's reflection-based assignSeq can write through. Three
// production-shape inputs are recognized:
//
//  1. *rtiv1.Event-shaped wrappers (proto.Message that ProtoReflects to
//     an Event) — these may have a private `pb *rtiv1.Event` field
//     which the Writer's reflection cannot reach. We unwrap and rewrap
//     in a protoRecord whose embedded *rtiv1.Event exposes Seq via
//     promotion.
//  2. Pointer-to-struct with a public/private Seq field — pass through;
//     the Writer's existing reflection handles it.
//  3. Value-typed structs (e.g. the federation manager's
//     federateJoinedEvent) — copy into an addressable pointer wrapper
//     so the Writer's UnsafeAddr write-back succeeds.
//
// Records that don't match any shape pass through unchanged (the Writer
// will return its own descriptive error).
func ensurePointerRecord(evt core.EventRecord) core.EventRecord {
	if evt == nil {
		return evt
	}
	if pb := extractProtoEvent(evt); pb != nil {
		return &protoRecord{Event: pb}
	}
	rv := reflect.ValueOf(evt)
	if rv.Kind() == reflect.Pointer {
		return evt
	}
	if rv.Kind() != reflect.Struct {
		return evt
	}
	p := reflect.New(rv.Type())
	p.Elem().Set(rv)
	wrapped, ok := p.Interface().(core.EventRecord)
	if !ok {
		return evt
	}
	return wrapped
}

// extractProtoEvent recovers the underlying *rtiv1.Event from common
// production wrappers. Returns nil when the record is not a proto event
// wrapper.
//
// The federation manager + object registry both wrap *rtiv1.Event
// inside a struct that satisfies core.EventRecord (Seq() method) and
// proto.Message (ProtoReflect delegates to the inner Event). The
// Writer's reflection-based assignSeq cannot reach into private
// fields; round-tripping through proto.Marshal recovers a fresh
// *rtiv1.Event we can wrap in a protoRecord whose embedded pointer
// exposes Seq via promotion.
func extractProtoEvent(evt core.EventRecord) *rtiv1.Event {
	msg, ok := evt.(proto.Message)
	if !ok {
		return nil
	}
	if pb, ok := msg.(*rtiv1.Event); ok {
		return pb
	}
	b, err := proto.Marshal(msg)
	if err != nil {
		return nil
	}
	pb := &rtiv1.Event{}
	if err := proto.Unmarshal(b, pb); err != nil {
		return nil
	}
	return pb
}

// (protoRecord is declared in replayer.go in this same package; the
// multiplexer reuses it so wire-shape stays consistent between
// passthrough replay and live append.)

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
	// Sort federation names before iterating so the "first error"
	// returned on multi-failure paths is deterministic. Without this
	// the caller would see whichever close failed first under Go's
	// randomized map iteration order — fine for correctness, bad for
	// any test that asserts on the returned error's federation name.
	// (M5-audit issue #3.)
	names := make([]core.FederationName, 0, len(m.writers))
	for fed := range m.writers {
		names = append(names, fed)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	var firstErr error
	for _, fed := range names {
		if err := m.writers[fed].Close(); err != nil && firstErr == nil {
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
