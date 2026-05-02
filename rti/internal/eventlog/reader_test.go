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
)

// TestNewReader_RejectsBadMagic: a buffer whose first 8 bytes don't
// match Magic returns core.ErrWireMalformedMessage.
func TestNewReader_RejectsBadMagic(t *testing.T) {
	bad := bytes.Repeat([]byte{0xAB}, HeaderSize)
	_, err := NewReader(bytes.NewReader(bad))
	if !errors.Is(err, core.ErrWireMalformedMessage) {
		t.Errorf("NewReader on bad magic: err = %v, want ErrWireMalformedMessage", err)
	}
}

// TestNewReader_RejectsVersionMismatch: a header with version > supported
// returns core.ErrWireVersionMismatch.
func TestNewReader_RejectsVersionMismatch(t *testing.T) {
	buf := make([]byte, HeaderSize)
	if err := EncodeHeader(buf, core.EventLogHeader{
		Magic: Magic, Version: Version + 1, Federation: "x", Mode: core.ModeVerbose,
	}); err != nil {
		t.Fatalf("EncodeHeader: %v", err)
	}
	_, err := NewReader(bytes.NewReader(buf))
	if !errors.Is(err, core.ErrWireVersionMismatch) {
		t.Errorf("NewReader on version mismatch: err = %v, want ErrWireVersionMismatch", err)
	}
}

// TestNewReader_RejectsShortHeader: a buffer smaller than HeaderSize
// returns io.ErrUnexpectedEOF (header read failed).
func TestNewReader_RejectsShortHeader(t *testing.T) {
	short := make([]byte, HeaderSize-1)
	_, err := NewReader(bytes.NewReader(short))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("NewReader on short header: err = %v, want io.ErrUnexpectedEOF", err)
	}
}

// TestReader_Header: Header() returns the values written by NewWriter.
func TestReader_Header(t *testing.T) {
	var buf bytes.Buffer
	clk := core.NewFakeClock(time.Unix(123, 456))
	w, err := NewWriter(WriterOptions{
		Sink: &buf, Federation: "demo", Mode: core.ModeBestEffort, Seed: 42, Clock: clk,
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	_ = w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	hdr := r.Header()
	if hdr.Federation != "demo" {
		t.Errorf("Header.Federation = %q, want %q", hdr.Federation, "demo")
	}
	if hdr.Seed != 42 {
		t.Errorf("Header.Seed = %d, want 42", hdr.Seed)
	}
	if hdr.Mode != core.ModeBestEffort {
		t.Errorf("Header.Mode = %d, want %d", hdr.Mode, core.ModeBestEffort)
	}
	if hdr.Version != Version {
		t.Errorf("Header.Version = %d, want %d", hdr.Version, Version)
	}
}

// TestReader_RoundTrip: write N events, read them back via the iterator,
// assert seq numbers preserved.
func TestReader_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "rt", &buf)

	for i := 0; i < 5; i++ {
		evt := &unexportedSeqRecord{}
		if err := w.Append(context.Background(), "rt", evt); err != nil {
			t.Fatalf("Append[%d]: %v", i, err)
		}
	}
	_ = w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

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

// TestReader_TruncatedAtRecordBoundary: a log truncated exactly at a
// record boundary surfaces clean io.EOF (no partial-record errors).
func TestReader_TruncatedAtRecordBoundary(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "rb", &buf)

	for i := 0; i < 3; i++ {
		_ = w.Append(context.Background(), "rb", &unexportedSeqRecord{})
	}
	_ = w.Close()

	// The on-disk image is HeaderSize + N * (4 + bodySize). Truncating
	// to HeaderSize + bodySize-of-2-records yields a clean boundary
	// after the 2nd record.
	all := buf.Bytes()
	// Compute the boundary by walking the records ourselves.
	off := HeaderSize
	for i := 0; i < 2; i++ {
		ln := binary.LittleEndian.Uint32(all[off : off+4])
		off += 4 + int(ln)
	}
	truncated := all[:off]

	r, err := NewReader(bytes.NewReader(truncated))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	for i := 0; i < 2; i++ {
		if _, err := r.Next(context.Background()); err != nil {
			t.Fatalf("Next[%d]: %v", i, err)
		}
	}
	if _, err := r.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Errorf("Next at clean boundary: err = %v, want io.EOF", err)
	}
}

// TestReader_TruncatedMidLengthPrefix: a log truncated in the middle of
// a length prefix surfaces io.ErrUnexpectedEOF.
func TestReader_TruncatedMidLengthPrefix(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "ml", &buf)

	for i := 0; i < 2; i++ {
		_ = w.Append(context.Background(), "ml", &unexportedSeqRecord{})
	}
	_ = w.Close()

	all := buf.Bytes()
	// First record starts at HeaderSize. Walk past it to get to the
	// boundary, then keep 2 of the next length prefix's 4 bytes.
	ln1 := binary.LittleEndian.Uint32(all[HeaderSize : HeaderSize+4])
	cutAt := HeaderSize + 4 + int(ln1) + 2
	truncated := all[:cutAt]

	r, err := NewReader(bytes.NewReader(truncated))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	if _, err := r.Next(context.Background()); err != nil {
		t.Fatalf("Next[0]: %v", err)
	}
	_, err = r.Next(context.Background())
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("Next on truncated length prefix: err = %v, want io.ErrUnexpectedEOF", err)
	}
}

// TestReader_TruncatedMidBody: a log truncated in the middle of a record
// body surfaces io.ErrUnexpectedEOF.
func TestReader_TruncatedMidBody(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "mb", &buf)

	for i := 0; i < 2; i++ {
		_ = w.Append(context.Background(), "mb", &unexportedSeqRecord{})
	}
	_ = w.Close()

	// Drop the last byte of the trailing record's body.
	all := buf.Bytes()
	truncated := all[:len(all)-1]

	r, err := NewReader(bytes.NewReader(truncated))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	if _, err := r.Next(context.Background()); err != nil {
		t.Fatalf("Next[0]: %v", err)
	}
	_, err = r.Next(context.Background())
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("Next on truncated body: err = %v, want io.ErrUnexpectedEOF", err)
	}
}

// TestReader_RejectsHugeLengthPrefix: a length prefix greater than a
// sanity cap is rejected (defends against corrupt/hostile files).
func TestReader_RejectsHugeLengthPrefix(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeHeader(make([]byte, HeaderSize), core.EventLogHeader{
		Magic: Magic, Version: Version, Federation: "x", Mode: core.ModeVerbose,
	}); err != nil {
		t.Fatalf("EncodeHeader: %v", err)
	}
	headerBuf := make([]byte, HeaderSize)
	_ = EncodeHeader(headerBuf, core.EventLogHeader{
		Magic: Magic, Version: Version, Federation: "x", Mode: core.ModeVerbose,
	})
	buf.Write(headerBuf)
	// Append a huge length prefix with no body to follow.
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], 1<<30)
	buf.Write(lenBuf[:])

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	if _, err := r.Next(context.Background()); err == nil {
		t.Errorf("Next on huge length prefix returned nil error")
	}
}

// TestReader_NilSrc: NewReader rejects a nil source.
func TestReader_NilSrc(t *testing.T) {
	if _, err := NewReader(nil); err == nil {
		t.Errorf("NewReader(nil) returned nil error")
	}
}

// closableReader is a bytes.Reader wrapper exposing Close().
type closableReader struct {
	*bytes.Reader
	closeCount int
	closeErr   error
}

func (c *closableReader) Close() error {
	c.closeCount++
	return c.closeErr
}

// TestReader_Close_DelegatesToCloser: when src implements io.Closer,
// Reader.Close calls src.Close().
func TestReader_Close_DelegatesToCloser(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "demo", &buf)
	_ = w.Close()

	src := &closableReader{Reader: bytes.NewReader(buf.Bytes())}
	r, err := NewReader(src)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if src.closeCount != 1 {
		t.Errorf("src.Close() called %d times, want 1", src.closeCount)
	}
}

// TestReader_ProtoEvent: the eventRecord type returned by Next exposes
// the underlying *rtiv1.Event via ProtoEvent() so the replayer can
// access body fields.
func TestReader_ProtoEvent(t *testing.T) {
	var buf bytes.Buffer
	w := newWriterForTest(t, "pe", &buf)
	_ = w.Append(context.Background(), "pe", &unexportedSeqRecord{})
	_ = w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	rec, err := r.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	er, ok := rec.(*eventRecord)
	if !ok {
		t.Fatalf("Next returned %T, want *eventRecord", rec)
	}
	if er.ProtoEvent() == nil {
		t.Errorf("ProtoEvent() = nil")
	}
	if er.ProtoEvent().GetSeq() != 1 {
		t.Errorf("ProtoEvent().Seq = %d, want 1", er.ProtoEvent().GetSeq())
	}
}

// TestReader_RejectsCorruptBody: a length prefix that promises N bytes
// of body but the actual bytes don't decode as a valid Event are
// surfaced as core.ErrWireMalformedMessage.
func TestReader_RejectsCorruptBody(t *testing.T) {
	var buf bytes.Buffer
	headerBuf := make([]byte, HeaderSize)
	if err := EncodeHeader(headerBuf, core.EventLogHeader{
		Magic: Magic, Version: Version, Federation: "x", Mode: core.ModeVerbose,
	}); err != nil {
		t.Fatalf("EncodeHeader: %v", err)
	}
	buf.Write(headerBuf)
	// Length prefix says 4 bytes, body is 4 bytes that aren't a valid
	// Event encoding (an invalid varint tag).
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], 4)
	buf.Write(lenBuf[:])
	buf.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF})

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	_, err = r.Next(context.Background())
	if !errors.Is(err, core.ErrWireMalformedMessage) {
		t.Errorf("Next on corrupt body: err = %v, want core.ErrWireMalformedMessage", err)
	}
}
