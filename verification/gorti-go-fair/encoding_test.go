package main

import (
	"encoding/hex"
	"testing"
)

func TestPitchCompatibleHLAEncodings(t *testing.T) {
	integer, err := encodeInteger32BE(42)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(integer); got != "0000002a" {
		t.Fatalf("integer encoding = %s", got)
	}
	decodedInteger, err := decodeInteger32BE(integer)
	if err != nil || decodedInteger != 42 {
		t.Fatalf("integer round trip = %d, %v", decodedInteger, err)
	}

	text, err := encodeASCIIString("Pitch")
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(text); got != "000000055069746368" {
		t.Fatalf("string encoding = %s", got)
	}
	decodedText, err := decodeASCIIString(text)
	if err != nil || decodedText != "Pitch" {
		t.Fatalf("string round trip = %q, %v", decodedText, err)
	}
}

func TestEncodingRejectsMalformedValues(t *testing.T) {
	if _, err := decodeInteger32BE([]byte{0, 1}); err == nil {
		t.Fatal("short integer was accepted")
	}
	if _, err := decodeASCIIString([]byte{0, 0, 0, 2, 'x'}); err == nil {
		t.Fatal("bad string length was accepted")
	}
	if _, err := encodeASCIIString("\u00e9"); err == nil {
		t.Fatal("non-ASCII string was accepted")
	}
}

func TestDeterministicPayloadUsesCallerSeed(t *testing.T) {
	if first, second := deterministicPayload("1516", "attribute", 0), deterministicPayload("1517", "attribute", 0); first == second {
		t.Fatal("different caller seeds produced the same payload")
	}
	if got := deterministicPayload("1516", "attribute", 0); len(got) != 16 {
		t.Fatalf("payload length = %d", len(got))
	}
}
