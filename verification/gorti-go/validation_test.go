package main

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

func TestPreencodedWorkloadIsDeterministicAndHLAEncoded(t *testing.T) {
	first, err := preencodeWorkload(91, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := preencodeWorkload(91, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first[1].attributes[payloadField], second[1].attributes[payloadField]) {
		t.Fatal("same seed and index produced different payloads")
	}
	if got := binary.BigEndian.Uint32(first[1].attributes[sequenceField]); got != 1 {
		t.Fatalf("sequence = %d, want 1", got)
	}
	payload := first[1].attributes[payloadField]
	if got := binary.BigEndian.Uint32(payload[:4]); got != 16 || len(payload) != 20 {
		t.Fatalf("HLAASCIIstring length = %d, bytes = %d", got, len(payload))
	}
}

func TestValidateReflectChecksPayloadAndTimestamp(t *testing.T) {
	payloads, err := preencodeWorkload(42, 1)
	if err != nil {
		t.Fatal(err)
	}
	expected := payloads[0]
	timestamp := expected.logicalTime
	event := federate.ReflectAttributeValues{
		ObjectHandle: 7,
		ClassName:    objectClass,
		Attributes:   clonePayloadMap(expected.attributes),
		Timestamp:    &timestamp,
	}
	if err := validateReflect(event, 7, expected); err != nil {
		t.Fatalf("valid reflection rejected: %v", err)
	}

	event.Attributes[payloadField][4] ^= 0xff
	if err := validateReflect(event, 7, expected); err == nil || !strings.Contains(err.Error(), "Payload payload mismatch") {
		t.Fatalf("payload validation error = %v", err)
	}
	event.Attributes = clonePayloadMap(expected.attributes)
	badTimestamp := timestamp + 1
	event.Timestamp = &badTimestamp
	if err := validateReflect(event, 7, expected); err == nil || !strings.Contains(err.Error(), "timestamp") {
		t.Fatalf("timestamp validation error = %v", err)
	}
}

func TestValidateInteractionChecksPayloadAndTimestamp(t *testing.T) {
	payloads, err := preencodeWorkload(42, 1)
	if err != nil {
		t.Fatal(err)
	}
	expected := payloads[0]
	timestamp := expected.logicalTime
	event := federate.ReceiveInteraction{
		ClassName:  interactionClass,
		Parameters: clonePayloadMap(expected.interactionParameters),
		Timestamp:  &timestamp,
	}
	if err := validateInteraction(event, expected); err != nil {
		t.Fatalf("valid interaction rejected: %v", err)
	}

	event.Timestamp = nil
	if err := validateInteraction(event, expected); err == nil || !strings.Contains(err.Error(), "timestamp") {
		t.Fatalf("timestamp validation error = %v", err)
	}
}

func clonePayloadMap(source map[string][]byte) map[string][]byte {
	result := make(map[string][]byte, len(source))
	for name, payload := range source {
		result[name] = append([]byte(nil), payload...)
	}
	return result
}
