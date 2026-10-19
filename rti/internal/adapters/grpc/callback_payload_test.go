package grpc

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"sync"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Adding an envelope header requires updating the read-only comparison view.
func TestRequiredCallbackComparisonHeaderCoverage(t *testing.T) {
	want := map[protoreflect.FieldNumber]protoreflect.Name{
		1: "seq", 2: "callback_receipt", 4: "ownership_receipt",
		5: "object_knowledge_epoch", 6: "callback_recovery",
	}
	d := (&rtiv1.FederateEvent{}).ProtoReflect().Descriptor()
	for i := 0; i < d.Fields().Len(); i++ {
		f := d.Fields().Get(i)
		if o := f.ContainingOneof(); o != nil && o.Name() == "event" {
			continue
		}
		if want[f.Number()] != f.Name() {
			t.Fatalf("unreviewed envelope header: %s", f.FullName())
		}
		delete(want, f.Number())
	}
	if len(want) != 0 {
		t.Fatalf("missing envelope headers: %v", want)
	}
}

// This deliberately retains the pre-optimization clone-and-normalize algorithm.
func legacyRequiredPayloadCode(event, saved *rtiv1.FederateEvent, recovery bool) codes.Code {
	cloned := proto.Clone(event).(*rtiv1.FederateEvent)
	cloned.CallbackReceipt = nil
	if authority := saved.GetOwnershipReceipt(); authority != nil {
		actual := cloned.GetOwnershipReceipt()
		if (actual != nil && actual.CallbackId == authority.CallbackId) || (recovery && actual == nil) {
			cloned.OwnershipReceipt = proto.Clone(authority).(*rtiv1.OwnershipCallbackReceipt)
		} else {
			return codes.InvalidArgument
		}
	}
	if !proto.Equal(cloned, saved) {
		return codes.InvalidArgument
	}
	return codes.OK
}

func checkRequiredPayload(t *testing.T, event, saved *rtiv1.FederateEvent, recovery bool, want codes.Code) {
	t.Helper()
	beforeEvent, beforeSaved := proto.Clone(event), proto.Clone(saved)
	entry := &callbackReceipt{fed: "f", h: 1, event: saved, required: true, admitted: true, retry: true, recoveryOutcome: recovery}
	r := &CallbackReceipts{entries: map[string]*callbackReceipt{string(event.CallbackReceipt): entry}}
	commit, skip, err := r.preparedDelivery("f", 1, event)
	if got := status.Code(err); got != want || got != legacyRequiredPayloadCode(event, saved, recovery) {
		t.Fatalf("payload result=%v, want=%v, legacy=%v", got, want, legacyRequiredPayloadCode(event, saved, recovery))
	}
	if skip || (commit != nil) != (want == codes.OK) || entry.retry != (want != codes.OK) {
		t.Fatal("preparation changed delivery/rollback state")
	}
	if !proto.Equal(event, beforeEvent) || !proto.Equal(saved, beforeSaved) {
		t.Fatal("comparison mutated input or retained ledger")
	}
}

func comparisonEvent(size int) *rtiv1.FederateEvent {
	return &rtiv1.FederateEvent{
		Seq: 17, CallbackReceipt: []byte("receipt"), ObjectKnowledgeEpoch: 9,
		Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{
			InteractionClassHandle: 1, Parameters: map[uint64][]byte{2: bytes.Repeat([]byte{3}, size)},
			UserSuppliedTag: []byte("tag"),
		}},
	}
}

func TestRequiredCallbackComparisonPayloadAndHeaders(t *testing.T) {
	base := comparisonEvent(64)
	base.CallbackRecovery = &rtiv1.CallbackRecovery{InvocationIdentity: bytes.Repeat([]byte{1}, 32)}
	base.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
	base.GetReceive().ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x02})
	saved := proto.Clone(base).(*rtiv1.FederateEvent)
	saved.CallbackReceipt = nil
	for _, tc := range []struct {
		name string
		edit func(*rtiv1.FederateEvent)
		want codes.Code
	}{
		{"exact", func(*rtiv1.FederateEvent) {}, codes.OK},
		{"new-ticket", func(e *rtiv1.FederateEvent) { e.CallbackReceipt = []byte("other receipt") }, codes.OK},
		{"sequence", func(e *rtiv1.FederateEvent) { e.Seq++ }, codes.InvalidArgument},
		{"knowledge-epoch", func(e *rtiv1.FederateEvent) { e.ObjectKnowledgeEpoch++ }, codes.InvalidArgument},
		{"recovery-identity", func(e *rtiv1.FederateEvent) { e.CallbackRecovery.InvocationIdentity[0]++ }, codes.InvalidArgument},
		{"recovery-completed", func(e *rtiv1.FederateEvent) { e.CallbackRecovery.Completed = true }, codes.InvalidArgument},
		{"extra-authority", func(e *rtiv1.FederateEvent) { e.OwnershipReceipt = &rtiv1.OwnershipCallbackReceipt{CallbackId: 1} }, codes.InvalidArgument},
		{"class", func(e *rtiv1.FederateEvent) { e.GetReceive().InteractionClassHandle++ }, codes.InvalidArgument},
		{"payload", func(e *rtiv1.FederateEvent) { e.GetReceive().Parameters[2][0]++ }, codes.InvalidArgument},
		{"map-key", func(e *rtiv1.FederateEvent) {
			e.GetReceive().Parameters[3] = e.GetReceive().Parameters[2]
			delete(e.GetReceive().Parameters, 2)
		}, codes.InvalidArgument},
		{"tag", func(e *rtiv1.FederateEvent) { e.GetReceive().UserSuppliedTag[0]++ }, codes.InvalidArgument},
		{"envelope-unknown", func(e *rtiv1.FederateEvent) { e.ProtoReflect().GetUnknown()[2]++ }, codes.InvalidArgument},
		{"payload-unknown", func(e *rtiv1.FederateEvent) { e.GetReceive().ProtoReflect().GetUnknown()[2]++ }, codes.InvalidArgument},
		{"oneof", func(e *rtiv1.FederateEvent) {
			e.Event = &rtiv1.FederateEvent_Reflect{Reflect: &rtiv1.ReflectAttributeValues{}}
		}, codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := proto.Clone(base).(*rtiv1.FederateEvent)
			tc.edit(event)
			checkRequiredPayload(t, event, saved, false, tc.want)
		})
	}
}

func TestRequiredCallbackComparisonOwnershipAttempt(t *testing.T) {
	saved := comparisonEvent(8)
	saved.CallbackReceipt = nil
	saved.OwnershipReceipt = &rtiv1.OwnershipCallbackReceipt{FederationGeneration: 1, CallbackEpoch: 2, CallbackId: 3, DeliveryAttempt: 4}
	for _, recovery := range []bool{false, true} {
		for _, kind := range []string{"exact", "new-attempt", "wrong-id", "missing"} {
			t.Run(fmt.Sprintf("%v/%s", recovery, kind), func(t *testing.T) {
				event := proto.Clone(saved).(*rtiv1.FederateEvent)
				event.CallbackReceipt = []byte("receipt")
				want := codes.OK
				switch kind {
				case "new-attempt":
					event.OwnershipReceipt.DeliveryAttempt++
					event.OwnershipReceipt.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
				case "wrong-id":
					event.OwnershipReceipt.CallbackId++
					want = codes.InvalidArgument
				case "missing":
					event.OwnershipReceipt = nil
					if !recovery {
						want = codes.InvalidArgument
					}
				}
				checkRequiredPayload(t, event, saved, recovery, want)
			})
		}
	}
}

func TestRequiredCallbackComparisonEveryVariant(t *testing.T) {
	fields := (&rtiv1.FederateEvent{}).ProtoReflect().Descriptor().Oneofs().ByName("event").Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		t.Run(string(field.Name()), func(t *testing.T) {
			event := &rtiv1.FederateEvent{Seq: 1, CallbackReceipt: []byte("receipt")}
			m := event.ProtoReflect()
			m.Set(field, m.NewField(field))
			saved := proto.Clone(event).(*rtiv1.FederateEvent)
			saved.CallbackReceipt = nil
			checkRequiredPayload(t, event, saved, false, codes.OK)
			m.Get(field).Message().SetUnknown([]byte{0xa0, 0x06, 0x01})
			checkRequiredPayload(t, event, saved, false, codes.InvalidArgument)
		})
	}
}

func TestRequiredCallbackComparisonProtobufEdgeCases(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*rtiv1.FederateEvent)
	}{
		{"nil-map", func(e *rtiv1.FederateEvent) { e.GetReceive().Parameters = nil }},
		{"empty-map", func(e *rtiv1.FederateEvent) { e.GetReceive().Parameters = map[uint64][]byte{} }},
		{"nil-map-value", func(e *rtiv1.FederateEvent) { e.GetReceive().Parameters[2] = nil }},
		{"empty-map-value", func(e *rtiv1.FederateEvent) { e.GetReceive().Parameters[2] = []byte{} }},
		{"nan", func(e *rtiv1.FederateEvent) { e.GetReceive().LogicalTime = proto.Float64(math.NaN()) }},
		{"nil-oneof-body", func(e *rtiv1.FederateEvent) { e.Event = &rtiv1.FederateEvent_Receive{} }},
		{"typed-nil-oneof", func(e *rtiv1.FederateEvent) { e.Event = (*rtiv1.FederateEvent_Receive)(nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := comparisonEvent(8)
			tc.edit(event)
			saved := proto.Clone(event).(*rtiv1.FederateEvent)
			saved.CallbackReceipt = nil
			checkRequiredPayload(t, event, saved, false, codes.OK)
		})
	}
}

func TestRequiredCallbackComparisonEquivalentEncodings(t *testing.T) {
	event := comparisonEvent(0)
	saved := proto.Clone(event).(*rtiv1.FederateEvent)
	saved.CallbackReceipt = nil
	event.GetReceive().Parameters[2] = []byte{}
	saved.GetReceive().Parameters[2] = nil
	event.GetReceive().UserSuppliedTag = []byte{}
	saved.GetReceive().UserSuppliedTag = nil
	// Unknown fields with distinct numbers can appear in a different wire order.
	event.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01, 0xa8, 0x06, 0x02})
	saved.ProtoReflect().SetUnknown([]byte{0xa8, 0x06, 0x02, 0xa0, 0x06, 0x01})
	checkRequiredPayload(t, event, saved, false, codes.OK)
	event.GetReceive().Parameters = nil
	saved.GetReceive().Parameters = map[uint64][]byte{}
	checkRequiredPayload(t, event, saved, false, codes.OK)
}

func FuzzRequiredCallbackComparison(f *testing.F) {
	fields := (&rtiv1.FederateEvent{}).ProtoReflect().Descriptor().Oneofs().ByName("event").Fields()
	for i := 0; i < fields.Len(); i++ {
		event := comparisonEvent(8)
		event.Event = nil
		m := event.ProtoReflect()
		m.Set(fields.Get(i), m.NewField(fields.Get(i)))
		wire, err := proto.Marshal(event)
		if err != nil {
			f.Fatal(err)
		}
		event.CallbackReceipt = nil
		saved, err := proto.Marshal(event)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(wire, saved, false)
	}
	f.Fuzz(func(t *testing.T, wire, retained []byte, recovery bool) {
		if len(wire) > 65536 || len(retained) > 65536 {
			t.Skip()
		}
		event, saved := &rtiv1.FederateEvent{}, &rtiv1.FederateEvent{}
		if proto.Unmarshal(wire, event) != nil || proto.Unmarshal(retained, saved) != nil {
			return
		}
		checkRequiredPayload(t, event, saved, recovery, legacyRequiredPayloadCode(event, saved, recovery))
	})
}

func TestRequiredCallbackComparisonKeepsAdmissionIsolation(t *testing.T) {
	r, _, _ := requiredCallbackFixture(t, 8)
	source := comparisonEvent(64)
	ticket, rollback, err := r.PrepareRequired(context.Background(), "f", 1, source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rollback(false) })
	entry := r.entries[string(ticket)]
	wire := proto.Clone(source).(*rtiv1.FederateEvent)
	wire.CallbackReceipt = ticket
	source.GetReceive().Parameters[2][0]++
	checkRequiredPayload(t, wire, entry.event, false, codes.OK)
	source.CallbackReceipt = ticket
	checkRequiredPayload(t, source, entry.event, false, codes.InvalidArgument)
}

func TestRequiredCallbackComparisonConcurrentReaders(t *testing.T) {
	event := comparisonEvent(64)
	saved := proto.Clone(event).(*rtiv1.FederateEvent)
	saved.CallbackReceipt = nil
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				checkRequiredPayload(t, event, saved, false, codes.OK)
			}
		}()
	}
	wg.Wait()
}

func BenchmarkRequiredCallbackPreparedDelivery(b *testing.B) {
	for _, size := range []int{64, 4096} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			event := comparisonEvent(size)
			saved := proto.Clone(event).(*rtiv1.FederateEvent)
			saved.CallbackReceipt = nil
			entry := &callbackReceipt{fed: "f", h: 1, event: saved, required: true, admitted: true}
			r := &CallbackReceipts{entries: map[string]*callbackReceipt{string(event.CallbackReceipt): entry}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				commit, skip, err := r.preparedDelivery("f", 1, event)
				if err != nil || skip || commit == nil {
					b.Fatal("preparation failed", err)
				}
			}
		})
	}
}
