package object

import (
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// W6a acceptance pin: the per-subscriber callback envelope (outboundEvent
// adapter + FederateEvent + oneof wrapper) is ONE combined allocation
// (reflectEventBlock / receiveEventBlock, landed with W3). The inner
// ReflectAttributeValues / ReceiveInteraction stays a separate pointer
// shared across recipients, so the fanout pays 2 envelope allocations per
// op total instead of the historical 4.

func TestNewReflectEventSingleAllocation(t *testing.T) {
	attrs := map[uint64][]byte{1: {0xAB}}
	allocs := testing.AllocsPerRun(1000, func() {
		evt := newReflectEvent(42, 7, 3, attrs, nil)
		if evt.Attributes[1][0] != 0xAB {
			t.Fatal("reflect payload not shared")
		}
	})
	if allocs > 1 {
		t.Fatalf("newReflectEvent allocations = %v, want at most 1", allocs)
	}
}

func TestNewReceiveEventSingleAllocation(t *testing.T) {
	params := map[uint64][]byte{2: {0xCD}}
	allocs := testing.AllocsPerRun(1000, func() {
		evt := newReceiveEvent(43, core.InteractionClassHandle(9), params, nil)
		if evt.Parameters[2][0] != 0xCD {
			t.Fatal("receive payload not shared")
		}
	})
	if allocs > 1 {
		t.Fatalf("newReceiveEvent allocations = %v, want at most 1", allocs)
	}
}
