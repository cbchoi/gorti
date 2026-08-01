package main

import (
	"encoding/binary"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

func TestControlKind(t *testing.T) {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, controlStartTAR)
	event := federate.ReceiveInteraction{
		ClassName:  "Control",
		Parameters: map[string][]byte{"kind": payload},
	}

	got, ok := controlKind(event)
	if !ok {
		t.Fatal("controlKind did not recognize Control interaction")
	}
	if got != controlStartTAR {
		t.Fatalf("controlKind = %d, want %d", got, controlStartTAR)
	}
}

func TestControlKindRejectsMalformedPayload(t *testing.T) {
	event := federate.ReceiveInteraction{
		ClassName:  "Control",
		Parameters: map[string][]byte{"kind": {1}},
	}
	if _, ok := controlKind(event); ok {
		t.Fatal("controlKind accepted malformed payload")
	}
}
