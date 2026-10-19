package server

import (
	"context"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

type ownershipEpochSendBarrier struct {
	*multiOutbox
	barrier *ownershipAtomicBarrier
}

func TestOwnershipCallbackEpochProductionQueuedRediscovery(t *testing.T) {
	for _, batched := range []bool{false, true} {
		for _, sameClass := range []bool{false, true} {
			name := "events/child"
			if batched {
				name = "batches/child"
			}
			if sameClass {
				name += "/same-class"
			}
			t.Run(name, func(t *testing.T) {
				const fed core.FederationName = "callback-epoch-production"
				ctx, srv, conn, _ := momCompletionReviewServer(t, fed)
				ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{
					WireVersion: 1, FederationName: string(fed), FomModules: []*rtiv1.FOMModule{{Path: "callback.xml", Xml: []byte(ownershipAtomicFOM)}},
				})
				ownershipAtomicOK(t, err)
				owner := core.FederateHandle(momCompletionReviewJoin(ctx, t, conn, string(fed), "owner", "", created.FederationGeneration).FederateHandle)
				caller := core.FederateHandle(momCompletionReviewJoin(ctx, t, conn, string(fed), "caller", "", created.FederationGeneration).FederateHandle)
				model, err := srv.foms.Get(ctx, fed)
				ownershipAtomicOK(t, err)
				base, _ := model.LookupObjectClass("Base")
				child, _ := model.LookupObjectClass("Child")
				known, _ := model.LookupAttribute(base, "position")
				actual, _ := model.LookupAttribute(child, "position")
				collision, _ := model.LookupAttribute(child, "childValue")
				ownershipAtomicOK(t, srv.declMgr.PublishObjectClassAttributes(ctx, fed, owner, child, []core.AttributeHandle{actual, collision}))
				ownershipAtomicOK(t, srv.declMgr.PublishObjectClassAttributes(ctx, fed, caller, base, []core.AttributeHandle{known}))
				ownershipAtomicOK(t, srv.declMgr.PublishObjectClassAttributes(ctx, fed, caller, child, []core.AttributeHandle{actual, collision}))
				ownershipAtomicOK(t, srv.declMgr.SubscribeObjectClassAttributes(ctx, fed, caller, base, []core.AttributeHandle{known}))
				obj, _, err := srv.objReg.Register(ctx, fed, owner, child, "")
				ownershipAtomicOK(t, err)
				manager := srv.ownMgr.(*ownership.Manager)
				ownershipAtomicOK(t, manager.NegotiatedDivestTwoPhase(ctx, fed, owner, obj, []core.AttributeHandle{actual}, []byte("old-knowledge")))
				wantClass, wantAttr := base, known
				if !sameClass {
					ownershipAtomicOK(t, srv.declMgr.SubscribeObjectClassAttributes(ctx, fed, caller, child, []core.AttributeHandle{actual, collision}))
					wantClass, wantAttr = child, actual
				}
				ownershipAtomicOK(t, srv.objReg.LocalDelete(ctx, fed, caller, obj))
				ownershipAtomicOK(t, manager.CancelDivest(ctx, fed, owner, obj, []core.AttributeHandle{actual}))
				ownershipAtomicOK(t, manager.NegotiatedDivestTwoPhase(ctx, fed, owner, obj, []core.AttributeHandle{actual}, []byte("new-knowledge")))
				// A final non-ownership callback proves obsolete advice did not close
				// the stream or discard the remainder of its queued batch.
				ownershipAtomicOK(t, srv.outbox.Send(ctx, fed, caller, &object.CallbackEvent{Kind: object.CallbackReservationSucceeded, ObjectName: "epoch-end"}))
				req := &rtiv1.EventsRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(caller)}
				client := rtiv1.NewStreamServiceClient(conn)
				var receive func() ([]*rtiv1.FederateEvent, error)
				if batched {
					stream, err := client.EventBatches(ctx, req)
					ownershipAtomicOK(t, err)
					receive = func() ([]*rtiv1.FederateEvent, error) {
						batch, err := stream.Recv()
						if err != nil {
							return nil, err
						}
						return batch.Events, nil
					}
				} else {
					stream, err := client.Events(ctx, req)
					ownershipAtomicOK(t, err)
					receive = func() ([]*rtiv1.FederateEvent, error) {
						event, err := stream.Recv()
						return []*rtiv1.FederateEvent{event}, err
					}
				}
				discoveries, assumptions, ended := 0, 0, false
				lastClass := uint64(0)
				for !ended {
					events, err := receive()
					ownershipAtomicOK(t, err)
					for _, event := range events {
						if discovered := event.GetDiscover(); discovered != nil && discovered.ObjectHandle == uint64(obj) {
							discoveries++
							lastClass = discovered.ObjectClassHandle
						}
						if advice := event.GetOwnershipAssumption(); advice != nil {
							assumptions++
							if string(advice.Tag) != "new-knowledge" || len(advice.AttributeHandles) != 1 || advice.AttributeHandles[0] != uint64(wantAttr) {
								t.Errorf("obsolete or wrong-class advice on live stream: %v", advice)
							}
						}
						if reserved := event.GetReservationSucceeded(); reserved != nil && reserved.ObjectName == "epoch-end" {
							ended = true
						}
					}
				}
				if discoveries != 2 || lastClass != uint64(wantClass) || assumptions != 1 {
					t.Fatalf("discoveries=%d lastClass=%d assumptions=%d; want 2/%d/1", discoveries, lastClass, assumptions, wantClass)
				}
			})
		}
	}
}

func (o *ownershipEpochSendBarrier) Send(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, event core.OutboundEvent) error {
	if err := o.barrier.wait(ctx); err != nil {
		return err
	}
	return o.multiOutbox.Send(ctx, fed, recipient, event)
}

func TestOwnershipCallbackEpochRediscoveryAfterProjection(t *testing.T) {
	const fed core.FederationName = "callback-knowledge"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	foms := composition.NewFOMRepository()
	model, err := foms.Load(ctx, []core.FOMModule{{Path: "callback.xml", XML: []byte(ownershipAtomicFOM)}})
	ownershipAtomicOK(t, err)
	foms.RememberFor(fed, model)
	base, _ := model.LookupObjectClass("Base")
	child, _ := model.LookupObjectClass("Child")
	known, _ := model.LookupAttribute(base, "position")
	actual, _ := model.LookupAttribute(child, "position")
	collision, _ := model.LookupAttribute(child, "childValue")
	if known != collision || known == actual {
		t.Fatal("fixture requires colliding ancestor attribute coordinates")
	}
	out := newMultiOutboxWithBatch(64, 1, 0)
	queue, closeQueue, err := out.Subscribe(ctx, fed, 2)
	ownershipAtomicOK(t, err)
	t.Cleanup(func() { _ = closeQueue() })
	decl := declaration.New()
	decl.SetFOMRepository(foms)
	objects, err := object.New(object.Options{Declarations: decl, FOMs: foms, Outbox: out, Clock: core.NewManualClock(time.Unix(0, 0))})
	ownershipAtomicOK(t, err)
	decl.SetOnSubscribeObjectClass(objects.ObjectClassSubscribed)
	ownershipAtomicOK(t, decl.PublishObjectClassAttributes(ctx, fed, 1, child, []core.AttributeHandle{actual, collision}))
	ownershipAtomicOK(t, decl.SubscribeObjectClassAttributes(ctx, fed, 2, base, []core.AttributeHandle{known}))
	obj, _, err := objects.Register(ctx, fed, 1, child, "")
	ownershipAtomicOK(t, err)
	ownershipAtomicOK(t, decl.SubscribeObjectClassAttributes(ctx, fed, 2, child, []core.AttributeHandle{actual, collision}))
	drainScheduledEvents(queue)
	barrier := newOwnershipAtomicBarrier(t)
	projected := ownershipOutbox{next: &ownershipEpochSendBarrier{multiOutbox: out, barrier: barrier}, capture: objects.CaptureKnownObjectAttributes}
	done := make(chan error, 1)
	go func() {
		done <- projected.Send(ctx, fed, 2, &ownership.CallbackEvent{Kind: ownership.CallbackAssumption, Object: obj, Attributes: []core.AttributeHandle{actual}})
	}()
	ownershipAtomicAwait(t, barrier.entered)
	deleted := make(chan error, 1)
	go func() { deleted <- objects.LocalDelete(ctx, fed, 2, obj) }()
	ownershipAtomicOK(t, ownershipAtomicAwait(t, deleted))
	if _, cls, ok := objects.LookupKnownObjectInstance(fed, 2, obj); !ok || cls != child {
		t.Fatalf("rediscovery class=%d known=%t", cls, ok)
	}
	barrier.release()
	if err := ownershipAtomicAwait(t, done); err == nil {
		t.Error("callback admission accepted ancestor coordinates after child rediscovery")
	}
	for _, event := range drainScheduledEvents(queue) {
		if cb, ok := event.(*ownership.CallbackEvent); ok {
			t.Errorf("stale callback would address childValue instead of position: attributes=%v", cb.Attributes)
		}
	}
}

func TestOwnershipCallbackEpochDoesNotGuardTerminalOrUnclassifiedCallbacks(t *testing.T) {
	sink := &ownershipProjectionSink{}
	projected, captured := 0, 0
	out := ownershipOutbox{
		next: sink,
		project: func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, error) {
			projected++
			return []core.AttributeHandle{17}, nil
		},
		capture: func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (object.KnownAttributeProjection, error) {
			captured++
			return object.KnownAttributeProjection{}, object.ErrStaleObjectKnowledge
		},
	}
	kinds := []ownership.CallbackKind{ownership.CallbackAcquisitionNotification, ownership.CallbackUnavailable, ownership.CallbackDivestitureConfirmation, ownership.CallbackReleaseRequest}
	for _, kind := range kinds {
		ownershipAtomicOK(t, out.Send(context.Background(), "f", 2, &ownership.CallbackEvent{Kind: kind, Attributes: []core.AttributeHandle{1}}))
	}
	if captured != 0 || projected != len(kinds) || len(sink.events) != len(kinds) {
		t.Fatalf("terminal/unclassified path changed: captured=%d projected=%d events=%d", captured, projected, len(sink.events))
	}
	for i, event := range sink.events {
		callback, ok := event.(*ownership.CallbackEvent)
		if !ok || callback.Kind != kinds[i] || len(callback.Attributes) != 1 || callback.Attributes[0] != 17 {
			t.Fatalf("terminal/unclassified callback wrapped or lost: %v", event)
		}
	}
}
