package grpc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

type streamKnowledgeGuard struct{ stale atomic.Bool }

func (g *streamKnowledgeGuard) Validate() error {
	if g.stale.Load() {
		return object.ErrStaleObjectKnowledge
	}
	return nil
}

func TestStreamKnowledgeEpochSuppressesOnlyObsoleteAdvice(t *testing.T) {
	for _, batched := range []bool{false, true} {
		name := "events"
		if batched {
			name = "batches"
		}
		t.Run(name, func(t *testing.T) {
			out := newFakeSubscribableOutbox()
			guard := &streamKnowledgeGuard{}
			advice := &ownership.CallbackEvent{Sequence: 1, Kind: ownership.CallbackAssumption, Object: 7, Attributes: []core.AttributeHandle{1}}
			out.push(delivery.GuardCancellableEvent(advice, guard, object.ErrStaleObjectKnowledge))
			out.push(&object.CallbackEvent{Sequence: 2, Kind: object.CallbackDiscover, Object: 7, ObjectClass: 3, ObjectName: "rediscovered"})
			out.push(&ownership.CallbackEvent{Sequence: 3, Kind: ownership.CallbackUnavailable, Object: 7, Attributes: []core.AttributeHandle{2}})
			out.push(delivery.GuardCancellableEvent(&ownership.CallbackEvent{Sequence: 4, Kind: ownership.CallbackAssumption, Object: 7, Attributes: []core.AttributeHandle{2}}, &streamKnowledgeGuard{}, object.ErrStaleObjectKnowledge))
			guard.stale.Store(true)
			out.closeChan()
			service := newStreamService(out)
			req := &rtiv1.EventsRequest{WireVersion: 1, FederationName: "f", FederateHandle: 2}
			var events []*rtiv1.FederateEvent
			if batched {
				stream := &fakeBatchStream{ctx: context.Background()}
				if err := service.EventBatches(req, stream); err != nil {
					t.Fatalf("obsolete advice closed callback stream: %v", err)
				}
				for _, batch := range stream.sent {
					events = append(events, batch.Events...)
				}
			} else {
				stream := newFakeStream(context.Background())
				if err := service.Events(req, stream); err != nil {
					t.Fatalf("obsolete advice closed callback stream: %v", err)
				}
				events = stream.sent
			}
			if len(events) != 3 || events[0].Seq != 2 || events[1].Seq != 3 || events[2].Seq != 4 {
				t.Fatalf("discovery, terminal, or current advice lost/reordered: %v", events)
			}
		})
	}
}

func TestStreamKnowledgeEpochDoesNotClassifyTerminalAsObsolete(t *testing.T) {
	guard := &streamKnowledgeGuard{}
	guard.stale.Store(true)
	terminal := delivery.GuardEvent(&ownership.CallbackEvent{Kind: ownership.CallbackUnavailable}, guard)
	_, err := delivery.ResolveEvent(terminal)
	if !errors.Is(err, object.ErrStaleObjectKnowledge) || errors.Is(err, delivery.ErrObsoleteAdvisory) {
		t.Fatalf("terminal callback classified as cancellable: %v", err)
	}
}

type invalidatingKnowledgeBatchStream struct {
	fakeBatchStream
	invalidate func()
}

func (s *invalidatingKnowledgeBatchStream) Send(batch *rtiv1.FederateEventBatch) error {
	if err := s.fakeBatchStream.Send(batch); err != nil {
		return err
	}
	if s.sendCalls == 1 {
		s.invalidate()
	}
	return nil
}

func TestStreamKnowledgeEpochRechecksAfterPreviousChunkSend(t *testing.T) {
	guard := &streamKnowledgeGuard{}
	// The second event is converted before the first frame is sent, and its
	// projection expires during that send. Rechecking only at conversion fails.
	tag := make([]byte, maxEventBatchSerializedSize/2+1024)
	first := &ownership.CallbackEvent{Sequence: 1, Kind: ownership.CallbackAssumption, Object: 7, Attributes: []core.AttributeHandle{1}, Tag: tag}
	second := delivery.GuardCancellableEvent(&ownership.CallbackEvent{Sequence: 2, Kind: ownership.CallbackAssumption, Object: 7, Attributes: []core.AttributeHandle{1}, Tag: tag}, guard, object.ErrStaleObjectKnowledge)
	terminal := &ownership.CallbackEvent{Sequence: 3, Kind: ownership.CallbackUnavailable, Object: 7, Attributes: []core.AttributeHandle{2}}
	discovery := &object.CallbackEvent{Sequence: 4, Kind: object.CallbackDiscover, Object: 7, ObjectClass: 3, ObjectName: "new-knowledge"}
	stream := &invalidatingKnowledgeBatchStream{fakeBatchStream: fakeBatchStream{ctx: context.Background()}, invalidate: func() { guard.stale.Store(true) }}
	if err := sendEventBatchChunks([]core.OutboundEvent{first, second, terminal, discovery}, stream); err != nil {
		t.Fatal(err)
	}
	var sequences []uint64
	for _, batch := range stream.sent {
		if len(batch.Events) == 0 {
			t.Fatal("obsolete advice produced an empty wire frame")
		}
		for _, event := range batch.Events {
			sequences = append(sequences, event.Seq)
		}
	}
	if len(sequences) != 3 || sequences[0] != 1 || sequences[1] != 3 || sequences[2] != 4 {
		t.Fatalf("queued stale advice survived prior chunk send: %v", sequences)
	}
}
