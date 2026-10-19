package server

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const ownershipAtomicFOM = `<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Base</name><attribute><name>position</name></attribute><objectClass><name>Child</name><attribute><name>childValue</name></attribute></objectClass></objectClass></objectClass></objects></objectModel>`

type ownershipAtomicBarrier struct {
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
	release func()
}

func newOwnershipAtomicBarrier(t *testing.T) *ownershipAtomicBarrier {
	t.Helper()
	b := &ownershipAtomicBarrier{entered: make(chan struct{}), resume: make(chan struct{})}
	b.release = sync.OnceFunc(func() { close(b.resume) })
	t.Cleanup(b.release)
	return b
}

func (b *ownershipAtomicBarrier) wait(ctx context.Context) error {
	var err error
	b.once.Do(func() {
		close(b.entered)
		select {
		case <-b.resume:
		case <-ctx.Done():
			err = ctx.Err()
		}
	})
	return err
}

func ownershipAtomicAwait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("ownership operation did not reach its next barrier; possible recursive object lock")
		var zero T
		return zero
	}
}

func ownershipAtomicOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// The same port boundary pauses either dispatch: legacy handlers arrive here
// after snapshot mapping, while atomic handlers still carry caller coordinates.
type ownershipAtomicRequestPort struct {
	*ownership.Manager
	barrier *ownershipAtomicBarrier
}

func (p *ownershipAtomicRequestPort) KnownOwnershipAvailable() bool {
	return p.Manager.KnownOwnershipAvailable()
}

func (p *ownershipAtomicRequestPort) ExecuteKnownOwnership(ctx context.Context, command core.OwnershipCommand) ([]core.AttributeHandle, error) {
	if err := p.barrier.wait(ctx); err != nil {
		return nil, err
	}
	return p.Manager.ExecuteKnownOwnership(ctx, command)
}

func (p *ownershipAtomicRequestPort) Acquire(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, tag []byte) error {
	if err := p.barrier.wait(ctx); err != nil {
		return err
	}
	return p.Manager.Acquire(ctx, fed, caller, obj, attrs, tag)
}

func (p *ownershipAtomicRequestPort) AcquireIfAvailable(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, tag []byte) error {
	if err := p.barrier.wait(ctx); err != nil {
		return err
	}
	return p.Manager.AcquireIfAvailable(ctx, fed, caller, obj, attrs, tag)
}

func ownershipAtomicClient(t *testing.T, srv *rtid, port core.OwnershipCoordinator) rtiv1.OwnershipServiceClient {
	t.Helper()
	adapter, err := grpcadapter.NewServer(grpcadapter.Options{
		Federations: srv.fedMgr, Declarations: srv.declMgr, Objects: srv.objReg,
		Ownership: port, Outbox: srv.outbox, FOMs: srv.foms,
	})
	ownershipAtomicOK(t, err)
	server := stdgrpc.NewServer(stdgrpc.UnaryInterceptor(adapter.UnaryMembershipInterceptor()))
	ownershipAtomicOK(t, adapter.Register(server))
	listener := bufconn.Listen(1 << 20)
	stopped := make(chan error, 1)
	go func() { stopped <- server.Serve(listener) }()
	conn, err := stdgrpc.NewClient("passthrough:///ownership-atomic", stdgrpc.WithTransportCredentials(insecure.NewCredentials()), stdgrpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(ctx)
	}))
	ownershipAtomicOK(t, err)
	t.Cleanup(func() {
		_ = conn.Close()
		server.Stop()
		_ = listener.Close()
		<-stopped
	})
	return rtiv1.NewOwnershipServiceClient(conn)
}

func TestOwnershipAtomicRediscoveryAtCommandBoundary(t *testing.T) {
	for _, ifAvailable := range []bool{false, true} {
		name := "acquire"
		if ifAvailable {
			name = "acquire-if-available"
		}
		t.Run(name, func(t *testing.T) {
			const fed core.FederationName = "atomic-command"
			ctx, srv, conn, _ := momCompletionReviewServer(t, fed)
			created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{
				WireVersion: 1, FederationName: string(fed), FomModules: []*rtiv1.FOMModule{{Path: "atomic.xml", Xml: []byte(ownershipAtomicFOM)}},
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
			if known == actual || known != collision {
				t.Fatalf("fixture needs an inherited-handle collision: known=%d actual=%d child=%d", known, actual, collision)
			}
			queue, closeQueue, err := srv.outbox.Subscribe(ctx, fed, caller)
			ownershipAtomicOK(t, err)
			t.Cleanup(func() { _ = closeQueue() })
			ownershipAtomicOK(t, srv.declMgr.PublishObjectClassAttributes(ctx, fed, owner, child, []core.AttributeHandle{actual, collision}))
			ownershipAtomicOK(t, srv.declMgr.PublishObjectClassAttributes(ctx, fed, caller, base, []core.AttributeHandle{known}))
			ownershipAtomicOK(t, srv.declMgr.PublishObjectClassAttributes(ctx, fed, caller, child, []core.AttributeHandle{actual, collision}))
			ownershipAtomicOK(t, srv.declMgr.SubscribeObjectClassAttributes(ctx, fed, caller, base, []core.AttributeHandle{known}))
			obj, _, err := srv.objReg.Register(ctx, fed, owner, child, "")
			ownershipAtomicOK(t, err)
			ownershipAtomicOK(t, srv.declMgr.SubscribeObjectClassAttributes(ctx, fed, caller, child, []core.AttributeHandle{actual, collision}))
			if _, cls, ok := srv.objReg.LookupKnownObjectInstance(fed, caller, obj); !ok || cls != base {
				t.Fatalf("later subscription changed remembered class: %d %t", cls, ok)
			}
			manager := srv.ownMgr.(*ownership.Manager)
			if !manager.KnownOwnershipAvailable() {
				t.Fatal("production composition did not wire atomic ownership preparation")
			}
			drainScheduledEvents(queue)
			barrier := newOwnershipAtomicBarrier(t)
			client := ownershipAtomicClient(t, srv, &ownershipAtomicRequestPort{Manager: manager, barrier: barrier})
			finished := make(chan error, 1)
			go func() {
				_, err := client.AttributeOwnershipAcquisition(ctx, &rtiv1.AcquireRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(caller), ObjectHandle: uint64(obj), AttributeHandles: []uint64{uint64(known)}, IfAvailable: ifAvailable})
				finished <- err
			}()
			ownershipAtomicAwait(t, barrier.entered)
			rediscovered := make(chan error, 1)
			go func() { rediscovered <- srv.objReg.LocalDelete(ctx, fed, caller, obj) }()
			ownershipAtomicOK(t, ownershipAtomicAwait(t, rediscovered))
			if _, cls, ok := srv.objReg.LookupKnownObjectInstance(fed, caller, obj); !ok || cls != child {
				t.Fatalf("rediscovery did not establish child coordinates: %d %t", cls, ok)
			}
			ownershipAtomicOK(t, manager.UnconditionalDivest(ctx, fed, owner, obj, []core.AttributeHandle{actual, collision}))
			drainScheduledEvents(queue)
			barrier.release()
			ownershipAtomicOK(t, ownershipAtomicAwait(t, finished))
			if got, owned := manager.QueryOwnership(fed, obj, actual); owned {
				t.Errorf("stale Base mapping mutated position after Child rediscovery: owner=%d", got)
			}
			if got, owned := manager.QueryOwnership(fed, obj, collision); !owned || got != caller {
				t.Errorf("current Child childValue was not acquired: owner=%d owned=%t", got, owned)
			}
			callbacks := 0
			for _, event := range drainScheduledEvents(queue) {
				if cb := ownershipReviewWireEvent(t, event).GetOwnershipAcquired(); cb != nil {
					callbacks++
					if !slices.Equal(cb.AttributeHandles, []uint64{uint64(collision)}) {
						t.Errorf("acquired callback leaked stale registered coordinates: %v", cb.AttributeHandles)
					}
				}
			}
			if callbacks != 1 {
				t.Errorf("acquisition callbacks=%d; want exactly one", callbacks)
			}
		})
	}
}

type ownershipAtomicReservationPort struct {
	*multiOutbox
	barrier  *ownershipAtomicBarrier
	reserves int
}

func (p *ownershipAtomicReservationPort) Reserve(ctx context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	p.reserves++
	if err := p.barrier.wait(ctx); err != nil {
		return nil, err
	}
	return p.multiOutbox.Reserve(ctx, fed, deliveries)
}

func TestOwnershipAtomicReleaseDeniedRediscoveryDuringReservation(t *testing.T) {
	for _, reacquireCollision := range []bool{false, true} {
		name := "reject-new-unowned-coordinate"
		if reacquireCollision {
			name = "retry-new-owned-coordinate"
		}
		t.Run(name, func(t *testing.T) {
			const fed core.FederationName = "atomic-denial"
			const owner, caller core.FederateHandle = 1, 2
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			foms := composition.NewFOMRepository()
			model, err := foms.Load(ctx, []core.FOMModule{{Path: "atomic.xml", XML: []byte(ownershipAtomicFOM)}})
			ownershipAtomicOK(t, err)
			foms.RememberFor(fed, model)
			base, _ := model.LookupObjectClass("Base")
			child, _ := model.LookupObjectClass("Child")
			known, _ := model.LookupAttribute(base, "position")
			actual, _ := model.LookupAttribute(child, "position")
			collision, _ := model.LookupAttribute(child, "childValue")
			if known != collision || known == actual {
				t.Fatal("fixture must have colliding inherited handles")
			}
			barrier := newOwnershipAtomicBarrier(t)
			out := &ownershipAtomicReservationPort{multiOutbox: newMultiOutboxWithBatch(64, 1, 0), barrier: barrier}
			queue, closeQueue, err := out.Subscribe(ctx, fed, owner)
			ownershipAtomicOK(t, err)
			t.Cleanup(func() { _ = closeQueue() })
			_, closeCaller, err := out.Subscribe(ctx, fed, caller)
			ownershipAtomicOK(t, err)
			t.Cleanup(func() { _ = closeCaller() })
			decl := declaration.New()
			decl.SetFOMRepository(foms)
			var objects *object.Registry
			manager, err := ownership.New(ownership.Options{
				Outbox: ownershipOutbox{next: out, project: func(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, error) {
					_, mapped, err := objects.ProjectKnownObjectAttributes(ctx, fed, recipient, obj, attrs)
					return mapped, err
				}},
				PrepareKnownAcquisition: func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
					return objects.PrepareKnownOwnershipAcquisition(ctx, fed, caller, obj, attrs)
				},
				PrepareKnownOperation: func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
					return objects.PrepareKnownOwnershipOperation(ctx, fed, caller, obj, attrs)
				},
				PrepareAcquisition: func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
					return objects.PrepareOwnershipAcquisition(ctx, fed, caller, obj, attrs)
				},
				PrepareDivestiture: func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
					return objects.PrepareOwnershipReleaseDenied(ctx, fed, caller, obj, attrs)
				},
			})
			ownershipAtomicOK(t, err)
			objects, err = object.New(object.Options{Declarations: decl, FOMs: foms, Outbox: out, Clock: core.NewManualClock(time.Unix(0, 0)), Ownership: manager,
				OnRegister: func(fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
					manager.RegisterInitialOwnership(fed, owner, obj, attrs)
				},
			})
			ownershipAtomicOK(t, err)
			decl.SetOnSubscribeObjectClass(objects.ObjectClassSubscribed)
			ownershipAtomicOK(t, decl.PublishObjectClassAttributes(ctx, fed, owner, child, []core.AttributeHandle{actual, collision}))
			ownershipAtomicOK(t, decl.PublishObjectClassAttributes(ctx, fed, caller, base, []core.AttributeHandle{known}))
			ownershipAtomicOK(t, decl.PublishObjectClassAttributes(ctx, fed, caller, child, []core.AttributeHandle{actual, collision}))
			ownershipAtomicOK(t, decl.SubscribeObjectClassAttributes(ctx, fed, caller, base, []core.AttributeHandle{known}))
			obj, _, err := objects.Register(ctx, fed, owner, child, "")
			ownershipAtomicOK(t, err)
			ownershipAtomicOK(t, decl.SubscribeObjectClassAttributes(ctx, fed, caller, child, []core.AttributeHandle{actual, collision}))
			ownershipAtomicOK(t, manager.UnconditionalDivest(ctx, fed, owner, obj, []core.AttributeHandle{actual}))
			ownershipAtomicOK(t, manager.AcquireIfAvailable(ctx, fed, caller, obj, []core.AttributeHandle{actual}, nil))
			ownershipAtomicOK(t, manager.Acquire(ctx, fed, owner, obj, []core.AttributeHandle{actual}, nil))
			drainScheduledEvents(queue)
			finished := make(chan error, 1)
			go func() {
				_, err := manager.ExecuteKnownOwnership(ctx, core.OwnershipCommand{Operation: core.OwnershipReleaseDenied, Federation: fed, Federate: caller, Object: obj, Attributes: []core.AttributeHandle{known}})
				finished <- err
			}()
			ownershipAtomicAwait(t, barrier.entered)
			// Reservation must not retain either domain lock. These are real
			// ownership transfers and local deletion, not direct snapshot rewrites.
			changed := make(chan error, 1)
			go func() {
				if err := manager.UnconditionalDivest(ctx, fed, caller, obj, []core.AttributeHandle{actual}); err != nil {
					changed <- err
					return
				}
				if err := objects.LocalDelete(ctx, fed, caller, obj); err != nil {
					changed <- err
					return
				}
				attrs := []core.AttributeHandle{actual}
				if reacquireCollision {
					attrs = append(attrs, collision)
				}
				if err := manager.UnconditionalDivest(ctx, fed, owner, obj, attrs); err != nil {
					changed <- err
					return
				}
				if err := manager.AcquireIfAvailable(ctx, fed, caller, obj, attrs, nil); err != nil {
					changed <- err
					return
				}
				changed <- manager.Acquire(ctx, fed, owner, obj, attrs, nil)
			}()
			ownershipAtomicOK(t, ownershipAtomicAwait(t, changed))
			if _, cls, ok := objects.LookupKnownObjectInstance(fed, caller, obj); !ok || cls != child {
				t.Fatalf("caller was not rediscovered at Child: %d %t", cls, ok)
			}
			drainScheduledEvents(queue)
			barrier.release()
			err = ownershipAtomicAwait(t, finished)
			wantReserves := 1
			if reacquireCollision {
				ownershipAtomicOK(t, err)
				wantReserves = 2
			} else if !errors.Is(err, core.ErrAttributeNotOwned) {
				t.Errorf("changed coordinates must revalidate ownership: %v", err)
			}
			if out.reserves != wantReserves {
				t.Errorf("reservations=%d; want %d", out.reserves, wantReserves)
			}
			pendingPosition := false
			for _, pending := range manager.ExportState(fed).PendingAcquires {
				if pending.Object == obj && pending.Acquirer == owner {
					if pending.Attribute == actual {
						pendingPosition = true
					} else if pending.Attribute == collision {
						t.Error("new Child coordinate's pending acquisition was not denied")
					}
				}
			}
			if !pendingPosition {
				t.Error("stale Base mapping incorrectly removed position acquisition")
			}
			unavailable := 0
			for _, event := range drainScheduledEvents(queue) {
				if cb := ownershipReviewWireEvent(t, event).GetOwnershipUnavailable(); cb != nil {
					unavailable++
					if !slices.Equal(cb.AttributeHandles, []uint64{uint64(collision)}) {
						t.Errorf("stale reservation was committed: %v", cb.AttributeHandles)
					}
				}
			}
			wantUnavailable := 0
			if reacquireCollision {
				wantUnavailable = 1
			}
			if unavailable != wantUnavailable {
				t.Errorf("unavailable callbacks=%d; want %d", unavailable, wantUnavailable)
			}
		})
	}
}
