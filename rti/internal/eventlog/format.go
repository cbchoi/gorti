package eventlog

import "github.com/cbchoi/gorti/rti/internal/core"

// Magic is the 8-byte file magic identifying gorti event log files.
// First 5 bytes spell "KDRTI", followed by a version triplet 0/1/0
// (major.minor.patch encoded as bytes).
//
// FROZEN: changing this value invalidates every committed event log;
// any change requires a contract-change-request.
var Magic = [8]byte{'K', 'D', 'R', 'T', 'I', 0x00, 0x01, 0x00}

// Version is the structural version of the file format. Bumped only when
// the framing or header layout changes (Protobuf schema evolution does
// not require a version bump because Event is forward-compatible).
const Version uint32 = 1

// HeaderSize is the on-disk size of the file header in bytes.
//   8 (magic) + 4 (version) + 32 (federation name, padded) + 8 (createdAtNs)
//   + 8 (seed) + 1 (mode) + 3 (reserved/padding) = 64 bytes.
const HeaderSize = 64

// MaxFederationNameBytes is the cap on the federation-name field stored
// in the file header. Federations with longer names cannot persist a log;
// the federation manager rejects at create time.
const MaxFederationNameBytes = 32

// EncodeHeader writes a fixed-width header into buf. buf MUST be at least
// HeaderSize bytes long.
//
// FROZEN-SHAPE: Agent A implements; format invariants are tested in
// tests/spec/M2/eventlog_test.go.
func EncodeHeader(buf []byte, hdr core.EventLogHeader) error {
	_ = buf
	_ = hdr
	return ErrNotImplemented
}

// DecodeHeader reads a HeaderSize-byte buffer into hdr.
//
// FROZEN-SHAPE: Agent A implements.
func DecodeHeader(buf []byte) (core.EventLogHeader, error) {
	_ = buf
	return core.EventLogHeader{}, ErrNotImplemented
}
