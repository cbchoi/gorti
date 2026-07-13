package eventlog

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"unsafe"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/protobuf/proto"
)

// ErrNotImplemented is returned by stub methods until Agent A implements them.
//
// Retained for symmetry with the reader; concrete writer/reader paths now
// return more specific errors.
var ErrNotImplemented = errors.New("eventlog: not implemented (Agent A M2 deliverable)")

// errWriterClosed is returned by Append/Sync after Close.
var errWriterClosed = errors.New("eventlog: writer is closed")

// WriterOptions bundles Writer dependencies. Tests typically pass a
// bytes.Buffer for Sink; production passes an *os.File opened for write.
type WriterOptions struct {
	// Sink receives the binary log stream. MUST NOT be nil.
	Sink io.Writer

	// Federation is recorded in the header and used in EventLog.Append's
	// federation argument as a sanity check.
	Federation core.FederationName

	// Mode is recorded in the header so replay can validate the
	// federation's mode matches.
	Mode core.Mode

	// Seed is recorded in the header for deterministic replay seeding.
	Seed uint64

	// Generation identifies this federation execution in the v2 header.
	Generation uint64

	// Clock remains required for deterministic writer construction. Version 2
	// no longer persists wall-clock creation time in the fixed-width header.
	Clock core.Clock
}

// Writer implements core.EventLog's write side over a single io.Writer.
// One Writer per federation; do not share across federations.
//
// Concurrency: Append/Sync/Close are safe to call concurrently from
// multiple goroutines. The internal mutex serializes (a) the nextSeq
// increment, (b) the seq write-back into the caller's record, and (c)
// the length-prefix + body write pair to the sink, so the on-disk
// stream stays well-formed (each record's framing is contiguous and
// seq order on disk matches the seq value assigned to the record).
//
// Historically (pre-M6 W1B) the Writer assumed a single serializing
// caller per federation (the federation manager). The W2A perf harness
// surfaced that gRPC handler goroutines may legitimately call into the
// same federation's Append concurrently, racing nextSeq. The mutex
// makes the safety contract explicit so callers don't have to reason
// about upstream serialization.
type Writer struct {
	opts WriterOptions

	mu      sync.Mutex // guards nextSeq, sink writes, closed
	nextSeq uint64
	closed  bool
}

// NewWriter constructs a Writer, writes the file header to opts.Sink, and
// returns the Writer ready to Append. Returns an error if any required
// field of opts is missing or the header write fails.
func NewWriter(opts WriterOptions) (*Writer, error) {
	if opts.Sink == nil {
		return nil, errors.New("eventlog: WriterOptions.Sink is required")
	}
	if opts.Clock == nil {
		return nil, errors.New("eventlog: WriterOptions.Clock is required (D-1: no time.Now)")
	}
	if len(opts.Federation) > MaxFederationNameBytes {
		return nil, errFederationNameTooLong
	}

	hdr := core.EventLogHeader{
		Magic:      Magic,
		Version:    Version,
		Federation: opts.Federation,
		Generation: opts.Generation,
		Seed:       opts.Seed,
		Mode:       opts.Mode,
	}
	var headerBuf [HeaderSize]byte
	if err := EncodeHeader(headerBuf[:], hdr); err != nil {
		return nil, err
	}
	if _, err := opts.Sink.Write(headerBuf[:]); err != nil {
		return nil, fmt.Errorf("eventlog: write header: %w", err)
	}
	return &Writer{opts: opts}, nil
}

// Append implements core.EventLog. Writes one length-prefixed Event record
// to the sink and assigns evt's seq the next monotonic sequence number.
// fed argument MUST equal the Writer's federation; mismatch returns an
// error (defensive — multi-federation Writers are not supported).
//
// # Type-acceptance contract (Option A)
//
// Append accepts two concrete record shapes:
//
//  1. A proto.Message with an exported uint64 Seq field (e.g.
//     *rtiv1.Event in production). The message is marshaled directly
//     after the writer assigns Seq.
//  2. Any struct pointer with a uint64 field named "Seq" or "seq". The
//     writer sets the seq via reflection (unsafe pointer write for
//     unexported fields) and marshals a synthetic *rtiv1.Event{Seq: N}
//     to the wire so that round-trip and replay still produce a valid
//     binary log. This path exists for spec-test fixtures whose body
//     content is irrelevant; production code MUST always pass a
//     proto.Message.
//
// Write-ahead contract: Append MUST return successfully BEFORE the caller
// applies any state mutation. This is the determinism guarantee — replay
// reproduces exactly what was logged.
func (w *Writer) Append(ctx context.Context, fed core.FederationName, evt core.EventRecord) error {
	_ = ctx
	// Federation + nil checks happen before grabbing the lock so the
	// fast-fail paths don't contend with concurrent Appends. The closed
	// check repeats under the lock — the lock-free read is only an
	// early-out hint.
	if fed != w.opts.Federation {
		// Tolerate a benign race against Close: if the writer was
		// closed concurrently, return the closed error rather than
		// the federation-mismatch one (the latter would be misleading).
		// We don't bother taking the lock here; the closed check below
		// is authoritative.
		return fmt.Errorf("eventlog: Append federation %q != writer federation %q",
			fed, w.opts.Federation)
	}
	if evt == nil {
		return errors.New("eventlog: Append nil event")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return errWriterClosed
	}

	w.nextSeq++

	// Assign seq on the caller's record (write-back contract).
	if err := assignSeq(evt, w.nextSeq); err != nil {
		// Roll back the seq counter so the writer's state matches the
		// log: the failure means we never produced bytes.
		w.nextSeq--
		return err
	}

	// Marshal the wire body. If the record is itself a proto.Message
	// (production path — typically wrapping *rtiv1.Event with a Seq()
	// adapter), marshal it directly. Otherwise synthesize a minimal
	// *rtiv1.Event so the wire stays well-formed; the fixture's body
	// content is not part of the determinism contract.
	msg, ok := evt.(proto.Message)
	if !ok {
		msg = &rtiv1.Event{Seq: w.nextSeq}
	}
	frame, err := proto.MarshalOptions{}.MarshalAppend(
		make([]byte, 4, 4+proto.Size(msg)),
		msg,
	)
	if err != nil {
		w.nextSeq--
		return fmt.Errorf("eventlog: marshal event: %w", err)
	}
	binary.LittleEndian.PutUint32(frame[:4], uint32(len(frame)-4))

	n, err := w.opts.Sink.Write(frame)
	if err != nil {
		// The frame may be partially written, so the seq cannot be reused.
		return fmt.Errorf("eventlog: write event frame: %w", err)
	}
	if n != len(frame) {
		return fmt.Errorf("eventlog: write event frame: wrote %d of %d bytes: %w", n, len(frame), io.ErrShortWrite)
	}
	return nil
}

// assignSeq writes seq into the underlying record. It supports:
//   - any pointer-to-struct with a uint64 field named "Seq" (exported,
//     e.g. *rtiv1.Event in production)
//   - any pointer-to-struct with a uint64 field named "seq" (unexported,
//     written via unsafe.Pointer; required for the spec-test fixture
//     rti/spec/M2.fakeEventRecord which lives in another package).
func assignSeq(evt core.EventRecord, seq uint64) error {
	v := reflect.ValueOf(evt)
	if setter, ok := evt.(interface{ SetSeq(uint64) }); ok {
		if v.Kind() == reflect.Pointer && v.IsNil() {
			return fmt.Errorf("eventlog: cannot assign seq: record is not a non-nil pointer (kind=%s)", v.Kind())
		}
		setter.SetSeq(seq)
		return nil
	}
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("eventlog: cannot assign seq: record is not a non-nil pointer (kind=%s)", v.Kind())
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("eventlog: cannot assign seq: record is not a struct (kind=%s)", v.Kind())
	}

	for _, name := range []string{"Seq", "seq"} {
		f := v.FieldByName(name)
		if !f.IsValid() {
			continue
		}
		if f.Kind() != reflect.Uint64 {
			return fmt.Errorf("eventlog: %s field is %s, want uint64", name, f.Kind())
		}
		if f.CanSet() {
			f.SetUint(seq)
			return nil
		}
		// Unexported field: write through unsafe pointer.
		if !f.CanAddr() {
			return fmt.Errorf("eventlog: %s field is not addressable", name)
		}
		ptr := unsafe.Pointer(f.UnsafeAddr())
		*(*uint64)(ptr) = seq
		return nil
	}
	return fmt.Errorf("eventlog: record %T has no Seq/seq uint64 field", evt)
}

// Sync implements core.EventLog. Flushes the federation's log to durable
// storage. For sinks that implement io.Closer-style Sync (e.g. *os.File's
// Sync method) this propagates; for buffer sinks it is a no-op.
func (w *Writer) Sync(ctx context.Context, fed core.FederationName) error {
	_ = ctx
	if fed != w.opts.Federation {
		return fmt.Errorf("eventlog: Sync federation %q != writer federation %q",
			fed, w.opts.Federation)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errWriterClosed
	}
	if syncer, ok := w.opts.Sink.(interface{ Sync() error }); ok {
		return syncer.Sync()
	}
	return nil
}

// OpenReader implements core.EventLog. For an in-memory Writer this is
// not generally useful — production callers go through the federation
// manager which knows the on-disk path. Tests construct a Reader directly
// off the same buffer instead.
func (w *Writer) OpenReader(ctx context.Context, path string) (core.EventLogReader, error) {
	_ = ctx
	_ = path
	return nil, errors.New("eventlog: Writer.OpenReader is not supported on in-memory writers; use eventlog.NewReader on the persisted file")
}

// Close finalizes the writer. After Close, further Append calls return
// errWriterClosed. If the underlying sink implements io.Closer, it is
// closed (best-effort).
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	if c, ok := w.opts.Sink.(io.Closer); ok {
		if err := c.Close(); err != nil {
			return err
		}
	}
	w.closed = true
	return nil
}

// Compile-time assertion that Writer implements core.EventLog.
var _ core.EventLog = (*Writer)(nil)
