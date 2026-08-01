package m2spec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/eventlog"
)

// TestSpec_M2_EventLog_HeaderMagic: the first 8 bytes of any new log are
// the canonical magic bytes K D R T I 0x00 0x01 0x00.
//
// Implements: FR-EVT-1, NFR-CRASH-1.
func TestSpec_M2_EventLog_HeaderMagic(t *testing.T) {
	var buf bytes.Buffer
	clk := core.NewFakeClock(time.Unix(0, 0))
	w, err := eventlog.NewWriter(eventlog.WriterOptions{
		Sink: &buf, Federation: "demo", Mode: core.ModeVerbose, Seed: 42, Clock: clk,
	})
	if err != nil {
		t.Skipf("NewWriter not yet implemented: %v", err)
	}
	defer w.Close()

	if buf.Len() < len(eventlog.Magic) {
		t.Fatalf("header writes %d bytes, want at least %d", buf.Len(), len(eventlog.Magic))
	}
	got := buf.Bytes()[:len(eventlog.Magic)]
	if !bytes.Equal(got, eventlog.Magic[:]) {
		t.Errorf("magic = %x, want %x", got, eventlog.Magic[:])
	}
}

// TestSpec_M2_EventLog_HeaderSize: header is exactly HeaderSize bytes,
// fixed width regardless of federation name length.
//
// Implements: FR-EVT-1.
func TestSpec_M2_EventLog_HeaderSize(t *testing.T) {
	var buf bytes.Buffer
	w, err := eventlog.NewWriter(eventlog.WriterOptions{
		Sink: &buf, Federation: "x", Mode: core.ModeVerbose, Clock: core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Skipf("NewWriter not yet implemented: %v", err)
	}
	defer w.Close()
	if buf.Len() != eventlog.HeaderSize {
		t.Errorf("header size = %d, want %d", buf.Len(), eventlog.HeaderSize)
	}
}

// TestSpec_M2_EventLog_RoundTrip: write N events, read them back via
// Reader, assert seq numbers and bodies are preserved in order.
//
// Implements: FR-EVT-1, FR-EVT-2.
func TestSpec_M2_EventLog_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	clk := core.NewFakeClock(time.Unix(0, 0))
	w, err := eventlog.NewWriter(eventlog.WriterOptions{
		Sink: &buf, Federation: "rt", Mode: core.ModeVerbose, Seed: 7, Clock: clk,
	})
	if err != nil {
		t.Skipf("NewWriter not yet implemented: %v", err)
	}
	for i := 0; i < 5; i++ {
		evt := &fakeEventRecord{tag: "e" + string(rune('0'+i))}
		if err := w.Append(context.Background(), "rt", evt); err != nil {
			t.Skipf("Append not yet implemented: %v", err)
		}
	}
	if err := w.Sync(context.Background(), "rt"); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	r, err := eventlog.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Skipf("NewReader not yet implemented: %v", err)
	}
	defer r.Close()

	hdr := r.Header()
	if string(hdr.Federation) != "rt" {
		t.Errorf("Header.Federation = %q, want %q", hdr.Federation, "rt")
	}
	if hdr.Seed != 7 {
		t.Errorf("Header.Seed = %d, want 7", hdr.Seed)
	}

	for i := 0; i < 5; i++ {
		rec, err := r.Next(context.Background())
		if err != nil {
			t.Fatalf("Next[%d]: %v", i, err)
		}
		if rec.Seq() != uint64(i+1) {
			t.Errorf("Next[%d].Seq = %d, want %d", i, rec.Seq(), i+1)
		}
	}
	if _, err := r.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Errorf("Next after exhaust: err = %v, want io.EOF", err)
	}
}

// TestSpec_M2_EventLog_TruncatedTrailingRecord: a log truncated mid-record
// surfaces io.ErrUnexpectedEOF rather than panicking. The seq numbers up
// to (but not including) the truncated record are still readable.
//
// Implements: FR-EVT-2, NFR-CRASH-1.
func TestSpec_M2_EventLog_TruncatedTrailingRecord(t *testing.T) {
	var buf bytes.Buffer
	w, err := eventlog.NewWriter(eventlog.WriterOptions{
		Sink: &buf, Federation: "trunc", Mode: core.ModeVerbose, Clock: core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Skipf("NewWriter not yet implemented: %v", err)
	}
	for i := 0; i < 3; i++ {
		_ = w.Append(context.Background(), "trunc", &fakeEventRecord{tag: "x"})
	}
	_ = w.Sync(context.Background(), "trunc")

	// Truncate by 5 bytes — guaranteed to fall inside the trailing
	// record's payload (Protobuf-encoded Event is at least 4 bytes for
	// a non-empty oneof + length prefix).
	all := buf.Bytes()
	if len(all) < 6 {
		t.Skip("not enough bytes to construct truncation; impl may inline")
	}
	truncated := all[:len(all)-5]

	r, err := eventlog.NewReader(bytes.NewReader(truncated))
	if err != nil {
		t.Fatalf("NewReader on truncated header: %v", err)
	}
	defer r.Close()

	// First two records read cleanly. Third returns ErrUnexpectedEOF.
	for i := 0; i < 2; i++ {
		if _, err := r.Next(context.Background()); err != nil {
			t.Fatalf("Next[%d] on truncated log: %v", i, err)
		}
	}
	_, err = r.Next(context.Background())
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("Next on truncated last record: err = %v, want io.ErrUnexpectedEOF", err)
	}
}

// TestSpec_M2_EventLog_Reader_RejectsBadMagic: NewReader on a buffer
// whose first 8 bytes don't match Magic returns
// core.ErrWireMalformedMessage.
//
// Implements: FR-EVT-2.
func TestSpec_M2_EventLog_Reader_RejectsBadMagic(t *testing.T) {
	bad := bytes.Repeat([]byte{0xFF}, eventlog.HeaderSize)
	_, err := eventlog.NewReader(bytes.NewReader(bad))
	if !errors.Is(err, core.ErrWireMalformedMessage) {
		t.Errorf("NewReader on bad magic: err = %v, want ErrWireMalformedMessage", err)
	}
}

// TestSpec_M2_EventLog_Append_AssignsMonotonicSeq: each Append assigns
// the next monotonic seq starting at 1.
//
// Implements: FR-EVT-1, NFR-DET-1.
func TestSpec_M2_EventLog_Append_AssignsMonotonicSeq(t *testing.T) {
	var buf bytes.Buffer
	w, err := eventlog.NewWriter(eventlog.WriterOptions{
		Sink: &buf, Federation: "seq", Mode: core.ModeVerbose, Clock: core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Skipf("NewWriter not yet implemented: %v", err)
	}
	defer w.Close()

	for i := 0; i < 4; i++ {
		evt := &fakeEventRecord{}
		if err := w.Append(context.Background(), "seq", evt); err != nil {
			t.Fatalf("Append[%d]: %v", i, err)
		}
		if got := evt.Seq(); got != uint64(i+1) {
			t.Errorf("Append[%d] assigned seq = %d, want %d", i, got, i+1)
		}
	}
}
