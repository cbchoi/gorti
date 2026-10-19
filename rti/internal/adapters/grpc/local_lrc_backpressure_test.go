package grpc

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type pressureWireRegistry struct {
	*stubObjectRegistry
	out      *MandatoryCallbackOutbox
	applied  []string
	attempts int
}

func (r *pressureWireRegistry) apply(ctx context.Context, kind string) error {
	r.attempts++
	reservation, err := r.out.Reserve(ctx, "f", []core.OutboxDelivery{{Recipient: 1, Event: &syncpkg.SynchronizedEvent{Sequence: uint64(len(r.applied) + 1)}}})
	if err != nil {
		return &core.UncommittedWireError{Err: err}
	}
	defer reservation.Release()
	if err := reservation.Commit(); err != nil {
		return err
	}
	r.applied = append(r.applied, kind)
	return nil
}
func (r *pressureWireRegistry) UpdateAttributesWire(ctx context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ map[uint64][]byte, _ *core.LogicalTime) error {
	return r.apply(ctx, "update")
}
func (r *pressureWireRegistry) SendInteractionWire(ctx context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, _ map[uint64][]byte, _ *core.LogicalTime) error {
	return r.apply(ctx, "interaction")
}

type pressureAckStream struct {
	scriptedLocalLRCServerStream
	onAck func(uint64)
}

func (s *pressureAckStream) Send(ack *rtiv1.LocalLRCAck) error {
	if s.onAck != nil {
		s.onAck(ack.CommittedThrough)
	}
	return s.scriptedLocalLRCServerStream.Send(ack)
}

func finishPressureReceipts(t *testing.T, r *CallbackReceipts) {
	t.Helper()
	var events []*rtiv1.FederateEvent
	for ticket, entry := range r.entries {
		if entry.applied {
			continue
		}
		event := proto.Clone(entry.event).(*rtiv1.FederateEvent)
		event.CallbackReceipt = []byte(ticket)
		events = append(events, event)
	}
	for _, event := range events {
		commit, skip, err := r.preparedDelivery("f", 1, event)
		if err != nil || skip {
			t.Fatalf("prepare: skip=%v err=%v", skip, err)
		}
		commit(true)
		if err := r.ReportEntry(context.Background(), "f", 1, event.CallbackReceipt); err != nil {
			t.Fatal(err)
		}
		if err := r.Report(context.Background(), "f", 1, event.CallbackReceipt, true, ""); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLocalLRCReceiptPressurePreservesOrderAndObservesOnce(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprint(batch), func(t *testing.T) {
			r, _, out := requiredCallbackFixture(t, 1)
			registry := &pressureWireRegistry{stubObjectRegistry: &stubObjectRegistry{}, out: out}
			membership := &fixedLocalLRCMembership{generation: 1, federation: "fed", handle: 7}
			var observations []ServiceObservation
			server := &Server{membership: membership, objService: newObjectService(registry), momObserver: NewMOMObserver(MOMObserverOptions{Observe: func(_ context.Context, o ServiceObservation) error {
				observations = append(observations, o)
				return nil
			}})}
			ops := []*rtiv1.LocalLRCOperation{localLRCUpdateOperation(1, 3), localLRCInteractionOperation(2), localLRCUpdateOperation(3, 3)}
			requests := []*rtiv1.LocalLRCRequest{localLRCOpenRequest(1, 32)}
			if batch {
				requests = append(requests, localLRCBatchRequest(ops...))
			} else {
				requests = append(requests, &rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_AttributeUpdate{AttributeUpdate: ops[0].GetAttributeUpdate()}}, &rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_Interaction{Interaction: ops[1].GetInteraction()}}, &rtiv1.LocalLRCRequest{Body: &rtiv1.LocalLRCRequest_AttributeUpdate{AttributeUpdate: ops[2].GetAttributeUpdate()}})
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			stream := &pressureAckStream{scriptedLocalLRCServerStream: scriptedLocalLRCServerStream{ctx: ctx, requests: requests}}
			stream.onAck = func(n uint64) {
				if membership.acquired != membership.released {
					t.Fatal("membership held during pressure ACK")
				}
				if n != 0 {
					finishPressureReceipts(t, r)
				}
			}
			if err := newLocalLRCService(server).Exchange(stream); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(registry.applied, []string{"update", "interaction", "update"}) || registry.attempts != 5 {
				t.Fatalf("delivery/retry order: %v attempts=%d", registry.applied, registry.attempts)
			}
			if len(observations) != 3 {
				t.Fatalf("MOM counted retry attempts: %v", observations)
			}
			for _, o := range observations {
				if !o.Success {
					t.Fatal("transient pressure became service failure")
				}
			}
			var acks []uint64
			for _, ack := range stream.responses {
				acks = append(acks, ack.CommittedThrough)
			}
			if !reflect.DeepEqual(acks, []uint64{0, 1, 2, 3}) || r.pending != 0 || r.reserved != 0 {
				t.Fatalf("acks=%v pending=%d reserved=%d", acks, r.pending, r.reserved)
			}
		})
	}
}

func TestLocalLRCReceiptPressureCancellationAndGenerationFence(t *testing.T) {
	for _, changeGeneration := range []bool{false, true} {
		t.Run(fmt.Sprint(changeGeneration), func(t *testing.T) {
			_, _, out := requiredCallbackFixture(t, 1)
			registry := &pressureWireRegistry{stubObjectRegistry: &stubObjectRegistry{}, out: out}
			membership := &fixedLocalLRCMembership{generation: 1, federation: "fed", handle: 7}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			stream := &pressureAckStream{scriptedLocalLRCServerStream: scriptedLocalLRCServerStream{ctx: ctx, requests: []*rtiv1.LocalLRCRequest{localLRCOpenRequest(1, 32), localLRCBatchRequest(localLRCInteractionOperation(1), localLRCInteractionOperation(2))}}}
			stream.onAck = func(n uint64) {
				if n == 1 {
					if membership.acquired != membership.released {
						t.Fatal("teardown blocked by member lease")
					}
					if changeGeneration {
						membership.generation++
					} else {
						cancel()
					}
				}
			}
			err := newLocalLRCService(&Server{membership: membership, objService: newObjectService(registry)}).Exchange(stream)
			want := codes.Canceled
			if changeGeneration {
				want = codes.FailedPrecondition
			}
			if status.Code(err) != want || len(registry.applied) != 1 {
				t.Fatalf("err=%v applied=%v", err, registry.applied)
			}
			if membership.acquired != membership.released {
				t.Fatal("lease leaked")
			}
		})
	}
}

func TestLocalLRCQueuePressureDrainsWithoutPartialOrDuplicateDelivery(t *testing.T) {
	r, raw, out := requiredCallbackFixture(t, 32)
	ch, stop, err := raw.Subscribe(context.Background(), "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stop() }()
	registry := &pressureWireRegistry{stubObjectRegistry: &stubObjectRegistry{}, out: out}
	membership := &fixedLocalLRCMembership{generation: 1, federation: "fed", handle: 7}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var operations []*rtiv1.LocalLRCOperation
	for n := uint64(1); n <= 20; n++ {
		operations = append(operations, localLRCInteractionOperation(n))
	}
	stream := &pressureAckStream{scriptedLocalLRCServerStream: scriptedLocalLRCServerStream{ctx: ctx, requests: []*rtiv1.LocalLRCRequest{localLRCOpenRequest(1, 32), localLRCBatchRequest(operations...)}}}
	var delivered []uint64
	stream.onAck = func(n uint64) {
		if membership.acquired != membership.released {
			t.Fatal("queue wait retained lease")
		}
		for uint64(len(delivered)) < n {
			select {
			case batch := <-ch:
				for _, e := range batch {
					delivered = append(delivered, e.Seq())
				}
			default:
				t.Fatal("ACK preceded callback commit")
			}
		}
		finishPressureReceipts(t, r)
	}
	if err := newLocalLRCService(&Server{membership: membership, objService: newObjectService(registry)}).Exchange(stream); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 20 || registry.attempts != 22 || r.pending != 0 || r.reserved != 0 {
		t.Fatalf("deliveries=%v attempts=%d pending=%d reserved=%d", delivered, registry.attempts, r.pending, r.reserved)
	}
	for i, seq := range delivered {
		if seq != uint64(i+1) {
			t.Fatalf("duplicate/out-of-order callback: %v", delivered)
		}
	}
}

func TestLocalLRCCapacityNeverRetriesUncertifiedFailure(t *testing.T) {
	err := &callbackCapacityPressure{ready: make(chan struct{})}
	if localLRCCapacityPressure(err) != nil {
		t.Fatal("raw capacity error is not evidence of an uncommitted operation")
	}
	if localLRCCapacityPressure(&core.UncommittedWireError{Err: fmt.Errorf("journal failure")}) != nil {
		t.Fatal("non-capacity error became retryable")
	}
}

func TestLocalLRCReceiptPressureRechecksCheckpointAndRestoreEpoch(t *testing.T) {
	for _, restoreEpoch := range []bool{false, true} {
		t.Run(fmt.Sprint(restoreEpoch), func(t *testing.T) {
			r, _, out := requiredCallbackFixture(t, 1)
			registry := &pressureWireRegistry{stubObjectRegistry: &stubObjectRegistry{}, out: out}
			membership := &fixedLocalLRCMembership{generation: 1, federation: "fed", handle: 7}
			probe := new(checkpointProbe)
			server := &Server{membership: membership, checkpointAdmission: probe, objService: newObjectService(registry)}
			epoch := uint64(1)
			server.objService.transportationScope = func(context.Context, core.FederationName, core.FederateHandle) (core.TransportationScope, error) {
				return core.TransportationScope{CallbackEpoch: epoch}, nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			stream := &pressureAckStream{scriptedLocalLRCServerStream: scriptedLocalLRCServerStream{ctx: ctx, requests: []*rtiv1.LocalLRCRequest{localLRCOpenRequest(1, 32), localLRCBatchRequest(localLRCInteractionOperation(1), localLRCInteractionOperation(2))}}}
			stream.onAck = func(n uint64) {
				if n != 1 {
					return
				}
				if !probe.rw.TryLock() {
					t.Fatal("checkpoint blocked by capacity wait")
				}
				if restoreEpoch {
					epoch++
				} else {
					probe.deny = core.ErrSaveAlreadyInProgress
				}
				probe.rw.Unlock()
				finishPressureReceipts(t, r)
			}
			err := newLocalLRCService(server).Exchange(stream)
			if status.Code(err) != codes.FailedPrecondition || len(registry.applied) != 1 {
				t.Fatalf("stale operation crossed checkpoint: error=%v applied=%v", err, registry.applied)
			}
			if !probe.rw.TryLock() {
				t.Fatal("checkpoint lease leaked")
			}
			probe.rw.Unlock()
		})
	}
}
