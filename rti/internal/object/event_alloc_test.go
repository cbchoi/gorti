package object

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// W6a acceptance pin: the per-subscriber callback envelope (outboundEvent
// adapter + FederateEvent + oneof wrapper) is ONE combined allocation
// (reflectEventBlock / receiveEventBlock, landed with W3). The inner
// ReflectAttributeValues / ReceiveInteraction stays a separate pointer
// shared across recipients, so the fanout pays 2 envelope allocations per
// op total instead of the historical 4.

func TestNewReflectEventSingleAllocation(t *testing.T) {
	reflect := &rtiv1.ReflectAttributeValues{
		ObjectHandle:      7,
		ObjectClassHandle: 3,
		Attributes:        map[uint64][]byte{1: {0xAB}},
	}
	allocs := testing.AllocsPerRun(1000, func() {
		evt := newReflectEvent(42, reflect)
		if evt.Inner().GetReflect() != reflect {
			t.Fatal("reflect payload not shared")
		}
	})
	if allocs != 1 {
		t.Fatalf("newReflectEvent allocations = %v, want exactly 1 (combined block)", allocs)
	}
}

func TestNewReceiveEventSingleAllocation(t *testing.T) {
	receive := &rtiv1.ReceiveInteraction{
		InteractionClassHandle: 9,
		Parameters:             map[uint64][]byte{2: {0xCD}},
	}
	allocs := testing.AllocsPerRun(1000, func() {
		evt := newReceiveEvent(43, receive)
		if evt.Inner().GetReceive() != receive {
			t.Fatal("receive payload not shared")
		}
	})
	if allocs != 1 {
		t.Fatalf("newReceiveEvent allocations = %v, want exactly 1 (combined block)", allocs)
	}
}
