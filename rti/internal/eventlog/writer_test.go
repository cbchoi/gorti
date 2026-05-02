package eventlog

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
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
