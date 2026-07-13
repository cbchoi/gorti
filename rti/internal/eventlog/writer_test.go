package eventlog

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/protobuf/proto"
)

// (No exported-field record type: Go prohibits a method and a field
// sharing the same name on a struct, and core.EventRecord requires a
// Seq() method. Production wraps *rtiv1.Event in an adapter; tests
// exercise the unsafe-pointer path through unexportedSeqRecord, which
// matches the spec fixture rti/spec/M2.fakeEventRecord verbatim.)

func newWriterForTest(t *testing.T, fed core.FederationName, sink *bytes.Buffer) *Writer {
	t.Helper()
	w, err := NewWriter(WriterOptions{
		Sink:       sink,
		Federation: fed,
		Mode:       core.ModeVerbose,
		Seed:       1,
		Clock:      core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	return w
}

// TestNewWriter_WritesHeader: NewWriter writes exactly HeaderSize bytes
// and the magic occupies bytes 0-7.
func TestNewWriter_WritesHeader(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "demo", &buf)
	defer w.Close()

	if buf.Len() != HeaderSize {
		t.Errorf("after NewWriter, sink length = %d, want %d", buf.Len(), HeaderSize)
	}
	if !bytes.Equal(buf.Bytes()[:8], Magic[:]) {
		t.Errorf("magic = %x, want %x", buf.Bytes()[:8], Magic[:])
	}
}

// TestNewWriter_RejectsMissingSink: NewWriter rejects a nil sink.
func TestNewWriter_RejectsMissingSink(t *testing.T) {
	_, err := NewWriter(WriterOptions{
		Sink:       nil,
		Federation: "demo",
		Mode:       core.ModeVerbose,
		Clock:      core.NewFakeClock(time.Unix(0, 0)),
	})
	if err == nil {
		t.Errorf("NewWriter with nil sink returned nil error")
	}
}

// TestNewWriter_RejectsMissingClock: NewWriter rejects a nil clock (D-1).
func TestNewWriter_RejectsMissingClock(t *testing.T) {
	var buf bytes.Buffer
	_, err := NewWriter(WriterOptions{
		Sink:       &buf,
		Federation: "demo",
		Mode:       core.ModeVerbose,
		Clock:      nil,
	})
	if err == nil {
		t.Errorf("NewWriter with nil clock returned nil error")
	}
}

// TestNewWriter_RejectsLongFederation: NewWriter rejects federation
// names exceeding MaxFederationNameBytes.
func TestNewWriter_RejectsLongFederation(t *testing.T) {
	var buf bytes.Buffer
	long := bytes.Repeat([]byte{'a'}, MaxFederationNameBytes+1)
	_, err := NewWriter(WriterOptions{
		Sink:       &buf,
		Federation: core.FederationName(long),
		Mode:       core.ModeVerbose,
		Clock:      core.NewFakeClock(time.Unix(0, 0)),
	})
	if err == nil {
		t.Errorf("NewWriter on long federation name returned nil error")
	}
}

// unexportedSeqRecord exercises the unsafe-pointer path so that the spec
// fixture (rti/spec/M2.fakeEventRecord, with lowercase `seq`) is supported.
type unexportedSeqRecord struct {
	seq uint64
	tag string //nolint:unused // mirrors spec fixture layout
}

func (e *unexportedSeqRecord) Seq() uint64 { return e.seq }

// TestWriter_AppendAssignsMonotonicSeq_UnexportedField: Append assigns
// seq via reflection+unsafe to a record with an unexported `seq` field.
func TestWriter_AppendAssignsMonotonicSeq_UnexportedField(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "seq", &buf)
	defer w.Close()

	for i := 0; i < 4; i++ {
		evt := &unexportedSeqRecord{}
		if err := w.Append(context.Background(), "seq", evt); err != nil {
			t.Fatalf("Append[%d]: %v", i, err)
		}
		if got := evt.Seq(); got != uint64(i+1) {
			t.Errorf("Append[%d] assigned seq = %d, want %d", i, got, i+1)
		}
	}
}

// TestWriter_RejectsMismatchedFederation: Append with fed != writer's
// federation returns an error and does not advance seq.
func TestWriter_RejectsMismatchedFederation(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "alpha", &buf)
	defer w.Close()

	headerEnd := buf.Len()
	if err := w.Append(context.Background(), "beta", &unexportedSeqRecord{}); err == nil {
		t.Errorf("Append with mismatched federation returned nil error")
	}
	// Append must not have written anything to the sink.
	if buf.Len() != headerEnd {
		t.Errorf("Append with mismatched federation wrote %d bytes past header", buf.Len()-headerEnd)
	}
}

// TestWriter_AppendAfterClose: Append after Close returns an error.
func TestWriter_AppendAfterClose(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "demo", &buf)
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := w.Append(context.Background(), "demo", &unexportedSeqRecord{}); err == nil {
		t.Errorf("Append after Close returned nil error")
	}
}

// TestWriter_HeaderFirst: bytes 0-HeaderSize are the header, even
// after Append calls.
func TestWriter_HeaderFirst(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "demo", &buf)
	defer w.Close()

	for i := 0; i < 3; i++ {
		if err := w.Append(context.Background(), "demo", &unexportedSeqRecord{}); err != nil {
			t.Fatalf("Append[%d]: %v", i, err)
		}
	}
	hdr, err := DecodeHeader(buf.Bytes()[:HeaderSize])
	if err != nil {
		t.Fatalf("DecodeHeader: %v", err)
	}
	if hdr.Federation != "demo" {
		t.Errorf("hdr.Federation = %q, want %q", hdr.Federation, "demo")
	}
	if hdr.Mode != core.ModeVerbose {
		t.Errorf("hdr.Mode = %d, want %d", hdr.Mode, core.ModeVerbose)
	}
}

// TestWriter_AppendNilRecord: Append with a nil EventRecord errors out.
func TestWriter_AppendNilRecord(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "demo", &buf)
	defer w.Close()
	if err := w.Append(context.Background(), "demo", nil); err == nil {
		t.Errorf("Append(nil) returned nil error")
	}
}

// recordWithoutSeqField has a Seq() method but no Seq/seq struct field;
// the writer cannot assign and must return an error.
type recordWithoutSeqField struct{ value uint64 }

func (e *recordWithoutSeqField) Seq() uint64 { return e.value }

// TestWriter_AppendRejectsRecordMissingSeqField: a record whose struct
// has no Seq/seq field is rejected (assignSeq cannot find a target).
func TestWriter_AppendRejectsRecordMissingSeqField(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "demo", &buf)
	defer w.Close()
	headerEnd := buf.Len()
	if err := w.Append(context.Background(), "demo", &recordWithoutSeqField{}); err == nil {
		t.Errorf("Append on record missing seq field returned nil error")
	}
	// No bytes written past header.
	if buf.Len() != headerEnd {
		t.Errorf("failed Append wrote %d bytes past header", buf.Len()-headerEnd)
	}
}

// nonStructSeqRecord is a non-struct pointer (assignSeq must reject it).
type nonStructSeqRecord uint64

func (e *nonStructSeqRecord) Seq() uint64 { return uint64(*e) }

// TestWriter_AppendRejectsNonStructPointer: a record whose underlying
// type is not a struct can't have a seq field — Append must reject.
func TestWriter_AppendRejectsNonStructPointer(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "demo", &buf)
	defer w.Close()
	var v nonStructSeqRecord
	if err := w.Append(context.Background(), "demo", &v); err == nil {
		t.Errorf("Append on non-struct pointer returned nil error")
	}
}

// wrongTypeSeqRecord has a `seq` field of the wrong type (int32 instead
// of uint64). assignSeq must reject it. The Seq() method is required to
// satisfy core.EventRecord; it has a different name from the field so
// Go's name resolution is unambiguous.
type wrongTypeSeqRecord struct {
	seq int32
}

func (e *wrongTypeSeqRecord) Seq() uint64 { return uint64(e.seq) }

// TestWriter_AppendRejectsWrongSeqFieldType: a seq field whose type is
// not uint64 must be rejected (not silently truncated).
func TestWriter_AppendRejectsWrongSeqFieldType(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "demo", &buf)
	defer w.Close()
	if err := w.Append(context.Background(), "demo", &wrongTypeSeqRecord{}); err == nil {
		t.Errorf("Append on wrong-typed seq field returned nil error")
	}
}

// TestWriter_Sync_NoOpOnBuffer: Sync on a bytes.Buffer sink is a no-op
// (the buffer doesn't implement Sync()).
func TestWriter_Sync_NoOpOnBuffer(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "demo", &buf)
	defer w.Close()
	if err := w.Sync(context.Background(), "demo"); err != nil {
		t.Errorf("Sync on buffer sink: %v", err)
	}
}

// TestWriter_Sync_RejectsMismatchedFederation: Sync with the wrong fed
// returns an error.
func TestWriter_Sync_RejectsMismatchedFederation(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "alpha", &buf)
	defer w.Close()
	if err := w.Sync(context.Background(), "beta"); err == nil {
		t.Errorf("Sync with mismatched federation returned nil error")
	}
}

// TestWriter_Sync_AfterClose: Sync after Close returns errWriterClosed.
func TestWriter_Sync_AfterClose(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "demo", &buf)
	_ = w.Close()
	if err := w.Sync(context.Background(), "demo"); err == nil {
		t.Errorf("Sync after Close returned nil error")
	}
}

// syncableSink is a bytes.Buffer wrapper that implements Sync(); used to
// exercise the syncer-delegation branch in Writer.Sync.
type syncableSink struct {
	bytes.Buffer
	syncErr   error
	syncCount int
}

func (s *syncableSink) Sync() error {
	s.syncCount++
	return s.syncErr
}

// TestWriter_Sync_DelegatesToSink: when the sink implements Sync(),
// Writer.Sync calls into it and propagates errors.
func TestWriter_Sync_DelegatesToSink(t *testing.T) {
	sink := &syncableSink{}
	w, err := NewWriter(WriterOptions{
		Sink: sink, Federation: "demo", Mode: core.ModeVerbose,
		Clock: core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()
	if err := w.Sync(context.Background(), "demo"); err != nil {
		t.Errorf("Sync: %v", err)
	}
	if sink.syncCount != 1 {
		t.Errorf("sink.Sync() called %d times, want 1", sink.syncCount)
	}
	// Inject an error and confirm propagation.
	sink.syncErr = errors.New("disk full")
	if err := w.Sync(context.Background(), "demo"); err == nil {
		t.Errorf("Sync did not propagate sink error")
	}
}

// TestWriter_OpenReader_Unsupported: in-memory writer rejects OpenReader.
func TestWriter_OpenReader_Unsupported(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "demo", &buf)
	defer w.Close()
	if _, err := w.OpenReader(context.Background(), "ignored"); err == nil {
		t.Errorf("OpenReader returned nil error on in-memory writer")
	}
}

// closableSink wraps bytes.Buffer with a Close() method for the
// closer-delegation branch in Writer.Close.
type closableSink struct {
	bytes.Buffer
	closeCount int
	closeErr   error
}

func (s *closableSink) Close() error {
	s.closeCount++
	return s.closeErr
}

// TestWriter_Close_DelegatesToCloser: Close on a sink that implements
// io.Closer calls Close() on the sink.
func TestWriter_Close_DelegatesToCloser(t *testing.T) {
	sink := &closableSink{}
	w, err := NewWriter(WriterOptions{
		Sink: sink, Federation: "demo", Mode: core.ModeVerbose,
		Clock: core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if sink.closeCount != 1 {
		t.Errorf("sink.Close() called %d times, want 1", sink.closeCount)
	}
	// Idempotent — second Close is a no-op.
	if err := w.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if sink.closeCount != 1 {
		t.Errorf("idempotent Close called sink.Close() %d times", sink.closeCount)
	}
}

func TestP0Writer_CloseFailureRemainsRetryable(t *testing.T) {
	sink := &closableSink{closeErr: errors.New("transient close failure")}
	w, err := NewWriter(WriterOptions{
		Sink: sink, Federation: "demo", Mode: core.ModeVerbose,
		Clock: core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err == nil {
		t.Fatal("first Close succeeded, want injected failure")
	}
	sink.closeErr = nil
	if err := w.Close(); err != nil {
		t.Fatalf("retry Close: %v", err)
	}
	if sink.closeCount != 2 {
		t.Fatalf("sink close attempts = %d, want 2", sink.closeCount)
	}
}

// failingSink returns an error on every Write call.
type failingSink struct {
	headerWritten bool
}

func (s *failingSink) Write(p []byte) (int, error) {
	if !s.headerWritten {
		s.headerWritten = true
		return len(p), nil
	}
	return 0, errors.New("write failed")
}

// TestWriter_Append_PropagatesWriteError: a Write error on the sink
// during Append propagates with helpful context.
func TestWriter_Append_PropagatesWriteError(t *testing.T) {
	sink := &failingSink{}
	w, err := NewWriter(WriterOptions{
		Sink: sink, Federation: "demo", Mode: core.ModeVerbose,
		Clock: core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.Append(context.Background(), "demo", &unexportedSeqRecord{}); err == nil {
		t.Errorf("Append on failing sink returned nil error")
	}
}

type recordingSink struct {
	bytes.Buffer
	writes int
	last   []byte
}

func (s *recordingSink) Write(p []byte) (int, error) {
	s.writes++
	s.last = append(s.last[:0], p...)
	return s.Buffer.Write(p)
}

func TestWriter_AppendWritesByteIdenticalSingleFrame(t *testing.T) {
	sink := &recordingSink{}
	w, err := NewWriter(WriterOptions{
		Sink: sink, Federation: "demo", Mode: core.ModeVerbose,
		Clock: core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()
	sink.writes = 0
	sink.last = nil

	if err := w.Append(context.Background(), "demo", &unexportedSeqRecord{}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	body, err := proto.Marshal(&rtiv1.Event{Seq: 1})
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	want := make([]byte, 4+len(body))
	binary.LittleEndian.PutUint32(want[:4], uint32(len(body)))
	copy(want[4:], body)

	if sink.writes != 1 {
		t.Fatalf("Append Write calls = %d, want 1", sink.writes)
	}
	if !bytes.Equal(sink.last, want) {
		t.Errorf("Append frame = %x, want %x", sink.last, want)
	}
}

type shortAppendSink struct {
	headerWritten bool
	appendErr     error
	appendWrites  int
}

func (s *shortAppendSink) Write(p []byte) (int, error) {
	if !s.headerWritten {
		s.headerWritten = true
		return len(p), nil
	}
	s.appendWrites++
	return len(p) - 1, s.appendErr
}

func TestWriter_AppendRejectsShortFrameWrite(t *testing.T) {
	diskErr := errors.New("disk full")
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "nil error", want: io.ErrShortWrite},
		{name: "underlying error", err: diskErr, want: diskErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &shortAppendSink{appendErr: tt.err}
			w, err := NewWriter(WriterOptions{
				Sink: sink, Federation: "demo", Mode: core.ModeVerbose,
				Clock: core.NewFakeClock(time.Unix(0, 0)),
			})
			if err != nil {
				t.Fatalf("NewWriter: %v", err)
			}
			defer w.Close()

			evt := &unexportedSeqRecord{}
			err = w.Append(context.Background(), "demo", evt)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Append error = %v, want errors.Is(%v)", err, tt.want)
			}
			if sink.appendWrites != 1 {
				t.Errorf("Append Write calls = %d, want 1", sink.appendWrites)
			}
			if evt.Seq() != 1 {
				t.Errorf("event seq = %d, want 1", evt.Seq())
			}
		})
	}
}

// TestNewWriter_PropagatesHeaderWriteError: a Write error during header
// emission is propagated.
func TestNewWriter_PropagatesHeaderWriteError(t *testing.T) {
	sink := &headerFailingSink{}
	_, err := NewWriter(WriterOptions{
		Sink: sink, Federation: "demo", Mode: core.ModeVerbose,
		Clock: core.NewFakeClock(time.Unix(0, 0)),
	})
	if err == nil {
		t.Errorf("NewWriter on failing sink returned nil error")
	}
}

type headerFailingSink struct{}

func (s *headerFailingSink) Write(_ []byte) (int, error) {
	return 0, errors.New("disk full")
}

// protoStyleRecord wraps a rtiv1.Event-shaped struct to exercise the
// proto.Message marshal branch in Append. (We can't import rtiv1 in
// writer_test.go without exposing a heavier test seam; instead we
// embed an empty proto-shaped struct via the rtiv1 generated type
// directly — see the eventlog_proto_test.go file.)
