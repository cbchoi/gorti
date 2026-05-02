package eventlog

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/protobuf/proto"
)

// MaxEventBodyBytes caps the size of any single event record body. A
// length prefix exceeding this cap is treated as corruption rather than
// passed to the allocator, so a bit-flipped or hostile file cannot
// trigger an OOM.
//
// 16 MiB is well above any plausible HLA event payload (the largest
// realistic case is an attribute update with thousands of bytes); raise
// only with explicit justification.
const MaxEventBodyBytes = 16 * 1024 * 1024

// Reader implements core.EventLogReader over an io.Reader, iterating events
// in stored (TSO) order. Never random-access.
type Reader struct {
	src io.Reader
	hdr core.EventLogHeader
}

// NewReader constructs a Reader, reads + validates the file header, and
// positions the reader at the first event record.
//
// Returns:
//   - core.ErrWireMalformedMessage if the magic doesn't match Magic.
//   - core.ErrWireVersionMismatch if the version field exceeds Version.
//   - io.ErrUnexpectedEOF if the source is shorter than HeaderSize.
//   - the underlying error if the read itself fails.
func NewReader(src io.Reader) (*Reader, error) {
	if src == nil {
		return nil, errors.New("eventlog: NewReader src is nil")
	}
	var headerBuf [HeaderSize]byte
	if _, err := io.ReadFull(src, headerBuf[:]); err != nil {
		// io.ReadFull returns io.ErrUnexpectedEOF on short read and
		// io.EOF on zero bytes — promote io.EOF to ErrUnexpectedEOF
		// since a header is mandatory.
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("eventlog: read header: %w", io.ErrUnexpectedEOF)
		}
		return nil, fmt.Errorf("eventlog: read header: %w", err)
	}
	hdr, err := DecodeHeader(headerBuf[:])
	if err != nil {
		return nil, err
	}
	return &Reader{src: src, hdr: hdr}, nil
}

// Header implements core.EventLogReader.
func (r *Reader) Header() core.EventLogHeader {
	return r.hdr
}

// Next implements core.EventLogReader. Returns the next event record or
// (nil, io.EOF) at the clean end of the stream. On a truncated trailing
// record, returns (nil, io.ErrUnexpectedEOF). On a length prefix that
// exceeds MaxEventBodyBytes, returns a wrapped
// core.ErrWireMalformedMessage.
func (r *Reader) Next(ctx context.Context) (core.EventRecord, error) {
	_ = ctx
	var lenBuf [4]byte
	n, err := io.ReadFull(r.src, lenBuf[:])
	switch {
	case errors.Is(err, io.EOF) && n == 0:
		// Clean stream exhaustion at a record boundary.
		return nil, io.EOF
	case err != nil:
		// Short read on the length prefix is "truncated mid-record".
		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("eventlog: read length prefix: %w", err)
	}

	bodyLen := binary.LittleEndian.Uint32(lenBuf[:])
	if bodyLen > MaxEventBodyBytes {
		return nil, fmt.Errorf("%w: event body length %d exceeds cap %d",
			core.ErrWireMalformedMessage, bodyLen, MaxEventBodyBytes)
	}

	body := make([]byte, bodyLen)
	if _, err := io.ReadFull(r.src, body); err != nil {
		// Any short read on the body is mid-record truncation.
		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("eventlog: read event body: %w", err)
	}

	pbEvt := &rtiv1.Event{}
	if err := proto.Unmarshal(body, pbEvt); err != nil {
		return nil, fmt.Errorf("%w: unmarshal event: %v", core.ErrWireMalformedMessage, err)
	}
	return &eventRecord{pb: pbEvt}, nil
}

// Close releases the underlying reader (best-effort; if src isn't a
// Closer, this is a no-op returning nil).
func (r *Reader) Close() error {
	if c, ok := r.src.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// eventRecord adapts *rtiv1.Event to core.EventRecord (the proto message
// has Seq as a field, not a method, so it doesn't satisfy the interface
// directly). Exposed only through the Reader.
type eventRecord struct {
	pb *rtiv1.Event
}

func (e *eventRecord) Seq() uint64 { return e.pb.GetSeq() }

// ProtoEvent returns the underlying Event for callers that need access
// to the body fields (e.g. the replayer). Production code should use
// this rather than re-asserting on *rtiv1.Event.
func (e *eventRecord) ProtoEvent() *rtiv1.Event { return e.pb }

// Compile-time assertion that Reader implements core.EventLogReader.
var _ core.EventLogReader = (*Reader)(nil)
