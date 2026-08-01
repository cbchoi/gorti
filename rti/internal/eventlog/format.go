package eventlog

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/core"
)

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
const Version uint32 = 2

// HeaderSize is the on-disk size of the file header in bytes.
//
//	8 (magic) + 4 (version) + 32 (federation name, padded) + 8 (metadata)
//	+ 8 (seed) + 1 (mode) + 3 (reserved/padding) = 64 bytes.
//
// The metadata field is CreatedAtNs in version 1 and Generation in version 2.
const HeaderSize = 64

// MaxFederationNameBytes is the cap on the federation-name field stored
// in the file header. Federations with longer names cannot persist a log;
// the federation manager rejects at create time.
const MaxFederationNameBytes = 32

// On-disk header field offsets.
const (
	offMagic      = 0
	offVersion    = 8
	offFederation = 12
	offMetadata   = 44
	offSeed       = 52
	offMode       = 60
	// offReserved (61..63) is zero-padding.
)

// errFederationNameTooLong is returned by EncodeHeader when the
// federation name exceeds MaxFederationNameBytes.
var errFederationNameTooLong = fmt.Errorf("eventlog: federation name exceeds %d bytes", MaxFederationNameBytes)

// EncodeHeader writes a fixed-width header into buf. buf MUST be at least
// HeaderSize bytes long. Returns an error if buf is too small or if the
// federation name exceeds MaxFederationNameBytes.
//
// FROZEN-SHAPE: layout invariants are tested in
// tests/spec/M2/eventlog_test.go.
func EncodeHeader(buf []byte, hdr core.EventLogHeader) error {
	if len(buf) < HeaderSize {
		return fmt.Errorf("eventlog: header buffer too small: %d < %d", len(buf), HeaderSize)
	}
	name := []byte(hdr.Federation)
	if len(name) > MaxFederationNameBytes {
		return errFederationNameTooLong
	}

	// Zero the entire header region so reserved bytes / unused name
	// bytes are deterministic.
	for i := 0; i < HeaderSize; i++ {
		buf[i] = 0
	}

	copy(buf[offMagic:offMagic+8], Magic[:])
	binary.LittleEndian.PutUint32(buf[offVersion:offVersion+4], hdr.Version)
	copy(buf[offFederation:offFederation+MaxFederationNameBytes], name)
	if hdr.Version <= 1 {
		binary.LittleEndian.PutUint64(buf[offMetadata:offMetadata+8], hdr.CreatedAtNs)
	} else {
		binary.LittleEndian.PutUint64(buf[offMetadata:offMetadata+8], hdr.Generation)
	}
	binary.LittleEndian.PutUint64(buf[offSeed:offSeed+8], hdr.Seed)
	buf[offMode] = byte(hdr.Mode)
	// bytes 61..63 already zeroed by the loop above.
	return nil
}

// DecodeHeader reads a HeaderSize-byte buffer into hdr. Returns
// core.ErrWireMalformedMessage if the magic doesn't match Magic, and
// core.ErrWireVersionMismatch if the version field exceeds Version.
//
// FROZEN-SHAPE contract.
func DecodeHeader(buf []byte) (core.EventLogHeader, error) {
	if len(buf) < HeaderSize {
		return core.EventLogHeader{}, fmt.Errorf("eventlog: header buffer too small: %d < %d", len(buf), HeaderSize)
	}
	var hdr core.EventLogHeader
	copy(hdr.Magic[:], buf[offMagic:offMagic+8])
	if !bytes.Equal(hdr.Magic[:], Magic[:]) {
		return core.EventLogHeader{}, fmt.Errorf("%w: bad event-log magic", core.ErrWireMalformedMessage)
	}
	hdr.Version = binary.LittleEndian.Uint32(buf[offVersion : offVersion+4])
	if hdr.Version > Version {
		return core.EventLogHeader{}, fmt.Errorf("%w: event-log version %d > supported %d",
			core.ErrWireVersionMismatch, hdr.Version, Version)
	}
	// Federation name occupies a null-padded 32-byte region. Trim
	// trailing NULs to recover the original string. Names with embedded
	// NUL bytes are rejected at federation create time, so any trailing
	// NUL is padding.
	nameRaw := buf[offFederation : offFederation+MaxFederationNameBytes]
	end := len(nameRaw)
	for end > 0 && nameRaw[end-1] == 0 {
		end--
	}
	hdr.Federation = core.FederationName(string(nameRaw[:end]))
	metadata := binary.LittleEndian.Uint64(buf[offMetadata : offMetadata+8])
	if hdr.Version <= 1 {
		hdr.CreatedAtNs = metadata
	} else {
		hdr.Generation = metadata
	}
	hdr.Seed = binary.LittleEndian.Uint64(buf[offSeed : offSeed+8])
	hdr.Mode = core.Mode(buf[offMode])
	return hdr, nil
}
