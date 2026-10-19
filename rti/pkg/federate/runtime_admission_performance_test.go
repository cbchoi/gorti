package federate

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/protobuf/proto"
)

func runtimeAdmissionWire(id uint64) *rtiv1.FederateEvent {
	receipt := make([]byte, 32)
	binary.BigEndian.PutUint64(receipt, id)
	return &rtiv1.FederateEvent{CallbackReceipt: receipt, Event: &rtiv1.FederateEvent_Receive{
		Receive: &rtiv1.ReceiveInteraction{InteractionClassHandle: 1, Parameters: map[uint64][]byte{2: {3}}},
	}}
}

func TestRuntimeCallbackAdmissionRetainsRecoveryIdentity(t *testing.T) {
	f := &Federate{}
	wire := runtimeAdmissionWire(1)
	info, _ := callbackRecoveryInfo(wire)
	first := f.trackRuntimeCallback(wire, info)
	identity := sha256.Sum256(wire.CallbackReceipt)
	retry := runtimeAdmissionWire(2)
	retry.CallbackRecovery = &rtiv1.CallbackRecovery{InvocationIdentity: identity[:]}
	retryInfo, _ := callbackRecoveryInfo(retry)
	second := f.trackRuntimeCallback(retry, retryInfo)
	if first.runtimeID != second.runtimeID || len(f.runtimePending) != 1 {
		t.Fatal("fresh recovery receipt duplicated the pending invocation")
	}
	if !proto.Equal(f.runtimePending[0].wire, wire) {
		t.Fatal("deduplication replaced the original detached callback")
	}
	wire.CallbackReceipt[0] = 99
	if bytes.Equal(f.runtimePending[0].wire.CallbackReceipt, wire.CallbackReceipt) {
		t.Fatal("retained receipt aliases incoming wire storage")
	}
	f.reconcileCompletedCallback(info.identity)
	if len(f.runtimePending) != 0 {
		t.Fatal("completed recovery identity retained a pending callback")
	}
	third := f.trackRuntimeCallback(retry, retryInfo)
	if third.runtimeID == first.runtimeID || len(f.runtimePending) != 1 {
		t.Fatal("retired invocation reused stale queue membership")
	}
}

func BenchmarkRuntimeCallbackAdmission(b *testing.B) {
	for _, pending := range []int{16, 128, 1024} {
		b.Run(fmt.Sprint(pending), func(b *testing.B) {
			f := &Federate{}
			for i := 0; i < pending; i++ {
				wire := runtimeAdmissionWire(uint64(i + 1))
				info, _ := callbackRecoveryInfo(wire)
				f.trackRuntimeCallback(wire, info)
			}
			wire := runtimeAdmissionWire(uint64(pending + 1))
			info, _ := callbackRecoveryInfo(wire)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tracked := f.trackRuntimeCallback(wire, info)
				f.retireRuntimeEventLocked(ReceiveInteraction{CallbackInfo: tracked})
			}
		})
	}
}

func TestRuntimePendingRemovalPreservesCheckpointOrder(t *testing.T) {
	f := runtimeTestFederate(t, HLAfloat64Time)
	var infos []CallbackInfo
	for i := uint64(1); i <= 5; i++ {
		wire := runtimeAdmissionWire(i)
		info, _ := callbackRecoveryInfo(wire)
		infos = append(infos, f.trackRuntimeCallback(wire, info))
	}
	// Retire the middle, head and tail; the remaining invocation order is stable.
	for _, i := range []int{2, 0, 4} {
		event := ReceiveInteraction{CallbackInfo: infos[i]}
		f.retireRuntimeEventLocked(event)
		f.retireRuntimeEventLocked(event)
		if !f.runtimeEventConsumedLocked(event) {
			t.Fatal("retired invocation remains admissible")
		}
	}
	state := runtimeTestState(t, f)
	if len(state.PendingData) != 2 || state.PendingData[0].Identity != infos[1].identity || state.PendingData[1].Identity != infos[3].identity {
		t.Fatalf("checkpoint order changed: %+v", state.PendingData)
	}
	for _, i := range []int{1, 3} {
		if f.runtimeEventConsumedLocked(ReceiveInteraction{CallbackInfo: infos[i]}) {
			t.Fatal("pending invocation was lost")
		}
		f.reconcileCompletedCallback(infos[i].identity)
	}
	if len(runtimeTestState(t, f).PendingData) != 0 {
		t.Fatal("reconciled invocations remain pending")
	}
	wire := runtimeAdmissionWire(6)
	info, _ := callbackRecoveryInfo(wire)
	f.trackRuntimeCallback(wire, info)
	if state := runtimeTestState(t, f); len(state.PendingData) != 1 || state.PendingData[0].Identity != info.identity {
		t.Fatal("cannot append after complete retirement")
	}
}
