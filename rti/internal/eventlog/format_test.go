package eventlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestEncodeHeader_MagicAndSize: Magic occupies bytes 0-7 and the encoded
// region is exactly HeaderSize bytes.
func TestEncodeHeader_MagicAndSize(t *testing.T) {
	buf := make([]byte, HeaderSize)
	hdr := core.EventLogHeader{
		Magic:      Magic,
		Version:    Version,
		Federation: "demo",
		Generation: 0x0102030405060708,
		Seed:       0,
		Mode:       core.ModeVerbose,
	}
	if err := EncodeHeader(buf, hdr); err != nil {
		t.Fatalf("EncodeHeader: %v", err)
	}
	if !bytes.Equal(buf[:8], Magic[:]) {
		t.Errorf("magic = %x, want %x", buf[:8], Magic[:])
	}
	if got := binary.LittleEndian.Uint64(buf[44:52]); got != hdr.Generation {
		t.Errorf("v2 metadata at offset 44 = %#x, want generation %#x", got, hdr.Generation)
	}
}

// TestEncodeHeader_RejectsLongName: federation names exceeding the 32-byte
// cap are rejected.
func TestEncodeHeader_RejectsLongName(t *testing.T) {
	buf := make([]byte, HeaderSize)
	long := bytes.Repeat([]byte{'a'}, MaxFederationNameBytes+1)
	hdr := core.EventLogHeader{Magic: Magic, Version: Version, Federation: core.FederationName(long), Mode: core.ModeVerbose}
	if err := EncodeHeader(buf, hdr); err == nil {
		t.Errorf("EncodeHeader on %d-byte name returned nil error", len(long))
	}
}

// TestEncodeHeader_RejectsShortBuffer: a buffer smaller than HeaderSize is
// rejected.
func TestEncodeHeader_RejectsShortBuffer(t *testing.T) {
	buf := make([]byte, HeaderSize-1)
	if err := EncodeHeader(buf, core.EventLogHeader{Magic: Magic, Version: Version}); err == nil {
		t.Errorf("EncodeHeader on short buffer returned nil error")
	}
}

// TestDecodeHeader_RoundTrip: a wide range of field combinations
// round-trip through Encode → Decode.
func TestDecodeHeader_RoundTrip(t *testing.T) {
	cases := []core.EventLogHeader{
		{Magic: Magic, Version: Version, Federation: "demo", Generation: 1234567890, Seed: 42, Mode: core.ModeVerbose},
		{Magic: Magic, Version: Version, Federation: "x", Generation: 0, Seed: 0, Mode: core.ModeBestEffort},
		{Magic: Magic, Version: Version, Federation: "", Generation: 1, Seed: 0xDEADBEEF, Mode: core.ModeUnspecified},
		// 32-byte name (max length).
		{Magic: Magic, Version: Version,
			Federation: core.FederationName(bytes.Repeat([]byte{'a'}, MaxFederationNameBytes)),
			Generation: 9999, Seed: 1, Mode: core.ModeVerbose},
		// Maximum field values.
		{Magic: Magic, Version: Version, Federation: "max", Generation: ^uint64(0), Seed: ^uint64(0), Mode: core.ModeVerbose},
	}
	for _, want := range cases {
		t.Run(string(want.Federation), func(t *testing.T) {
			buf := make([]byte, HeaderSize)
			if err := EncodeHeader(buf, want); err != nil {
				t.Fatalf("EncodeHeader: %v", err)
			}
			got, err := DecodeHeader(buf)
			if err != nil {
				t.Fatalf("DecodeHeader: %v", err)
			}
			if got != want {
				t.Errorf("round-trip mismatch:\n want=%+v\n got=%+v", want, got)
			}
		})
	}
}

func TestDecodeHeader_V1CreatedAtCompatibility(t *testing.T) {
	want := core.EventLogHeader{
		Magic: Magic, Version: 1, Federation: "legacy",
		CreatedAtNs: 1234567890123, Seed: 99, Mode: core.ModeBestEffort,
	}
	// Build the legacy bytes directly so compatibility does not depend on the
	// current encoder making the same assumption as the decoder.
	buf := make([]byte, HeaderSize)
	copy(buf[0:8], Magic[:])
	binary.LittleEndian.PutUint32(buf[8:12], 1)
	copy(buf[12:44], []byte(want.Federation))
	binary.LittleEndian.PutUint64(buf[44:52], want.CreatedAtNs)
	binary.LittleEndian.PutUint64(buf[52:60], want.Seed)
	buf[60] = byte(want.Mode)
	got, err := DecodeHeader(buf)
	if err != nil {
		t.Fatalf("DecodeHeader(v1): %v", err)
	}
	if got != want {
		t.Fatalf("v1 compatibility mismatch:\n want=%+v\n got=%+v", want, got)
	}
	if got.Generation != 0 {
		t.Errorf("v1 Generation = %d, want 0", got.Generation)
	}
}

// TestDecodeHeader_BadMagic: returns ErrWireMalformedMessage on bad magic.
func TestDecodeHeader_BadMagic(t *testing.T) {
	buf := make([]byte, HeaderSize)
	for i := range buf {
		buf[i] = 0xFF
	}
	_, err := DecodeHeader(buf)
	if !errors.Is(err, core.ErrWireMalformedMessage) {
		t.Errorf("DecodeHeader bad magic: err = %v, want ErrWireMalformedMessage", err)
	}
}

// TestDecodeHeader_VersionTooNew: a header with version > supported is
// rejected with ErrWireVersionMismatch.
func TestDecodeHeader_VersionTooNew(t *testing.T) {
	buf := make([]byte, HeaderSize)
	if err := EncodeHeader(buf, core.EventLogHeader{Magic: Magic, Version: Version + 1, Federation: "demo", Mode: core.ModeVerbose}); err != nil {
		t.Fatalf("EncodeHeader: %v", err)
	}
	_, err := DecodeHeader(buf)
	if !errors.Is(err, core.ErrWireVersionMismatch) {
		t.Errorf("DecodeHeader version-too-new: err = %v, want ErrWireVersionMismatch", err)
	}
}

// TestDecodeHeader_ShortBuffer: a buffer < HeaderSize is rejected.
func TestDecodeHeader_ShortBuffer(t *testing.T) {
	buf := make([]byte, HeaderSize-1)
	if _, err := DecodeHeader(buf); err == nil {
		t.Errorf("DecodeHeader on short buffer returned nil error")
	}
}
