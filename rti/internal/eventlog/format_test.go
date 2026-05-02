package eventlog

import (
	"bytes"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestEncodeHeader_MagicAndSize: Magic occupies bytes 0-7 and the encoded
// region is exactly HeaderSize bytes.
func TestEncodeHeader_MagicAndSize(t *testing.T) {
	buf := make([]byte, HeaderSize)
	hdr := core.EventLogHeader{
		Magic:       Magic,
		Version:     Version,
		Federation:  "demo",
		CreatedAtNs: 0,
		Seed:        0,
		Mode:        core.ModeVerbose,
	}
	if err := EncodeHeader(buf, hdr); err != nil {
		t.Fatalf("EncodeHeader: %v", err)
	}
	if !bytes.Equal(buf[:8], Magic[:]) {
		t.Errorf("magic = %x, want %x", buf[:8], Magic[:])
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
		{Magic: Magic, Version: Version, Federation: "demo", CreatedAtNs: 1234567890, Seed: 42, Mode: core.ModeVerbose},
		{Magic: Magic, Version: Version, Federation: "x", CreatedAtNs: 0, Seed: 0, Mode: core.ModeBestEffort},
		{Magic: Magic, Version: Version, Federation: "", CreatedAtNs: 1, Seed: 0xDEADBEEF, Mode: core.ModeUnspecified},
		// 32-byte name (max length).
		{Magic: Magic, Version: Version,
			Federation:  core.FederationName(bytes.Repeat([]byte{'a'}, MaxFederationNameBytes)),
			CreatedAtNs: 9999, Seed: 1, Mode: core.ModeVerbose},
		// Maximum field values.
		{Magic: Magic, Version: Version, Federation: "max", CreatedAtNs: ^uint64(0), Seed: ^uint64(0), Mode: core.ModeVerbose},
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
