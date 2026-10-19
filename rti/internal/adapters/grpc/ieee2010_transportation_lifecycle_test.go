package grpc_test

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

func TestIEEE2010TransportationOwnershipLifecycle(t *testing.T) {
	for _, queryWhileUnowned := range []bool{false, true} {
		name := "without-intermediate-query"
		if queryWhileUnowned {
			name = "with-unowned-query"
		}
		t.Run(name, func(t *testing.T) {
			f := newIEEETransportationFixture(t)
			_, err := f.objClient.RequestAttributeTransportationTypeChange(f.ctx, &rtiv1.ChangeAttributeTransportRequest{
				WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner),
				ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.value)},
				TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT,
			})
			ieeeOODDMOK(t, err)
			query := func(want rtiv1.TransportationType) {
				t.Helper()
				response, err := f.objClient.QueryAttributeTransportationType(f.ctx, &rtiv1.QueryAttributeTransportationTypeRequest{
					WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.acquirer),
					ObjectHandle: uint64(f.obj), AttributeHandle: uint64(f.value),
				})
				ieeeOODDMOK(t, err)
				if response.GetTransportType() != want {
					t.Fatalf("transportation=%v, want %v", response.GetTransportType(), want)
				}
			}
			query(rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT)
			_, err = f.ownClient.UnconditionalAttributeOwnershipDivestiture(f.ctx, &rtiv1.UnconditionalDivestRequest{
				WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner),
				ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.value)},
			})
			ieeeOODDMOK(t, err)
			if queryWhileUnowned {
				query(rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE)
			}
			_, err = f.ownClient.AttributeOwnershipAcquisition(f.ctx, &rtiv1.AcquireRequest{
				WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner),
				ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.value)}, IfAvailable: true,
			})
			ieeeOODDMOK(t, err)
			query(rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE)
		})
	}
}

type ieeeTransportationFixture struct {
	*ieeeOODDMFixture
	ownerContext context.Context
	peerContext  context.Context
	epochs       *transportationEpochOutbox
	admission    *transportationAdmission
}

type transportationAdmission struct {
	mu      sync.RWMutex
	failure error
}

func (a *transportationAdmission) AcquireService(core.FederationName) (func(), error) {
	a.mu.RLock()
	if err := a.failure; err != nil {
		a.mu.RUnlock()
		return nil, err
	}
	return a.mu.RUnlock, nil
}

func (a *transportationAdmission) AcquireCheckpoint(core.FederationName) func() {
	a.mu.RLock()
	return a.mu.RUnlock
}

func (a *transportationAdmission) block(err error) { a.mu.Lock(); defer a.mu.Unlock(); a.failure = err }

type transportationEpochOutbox struct {
	*delivery.MultiOutbox
	epoch atomic.Uint64
}

func (o *transportationEpochOutbox) CallbackEpoch(core.FederationName, core.FederateHandle) (uint64, error) {
	return o.epoch.Load(), nil
}

func newIEEETransportationFixture(t *testing.T) *ieeeTransportationFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	f := &ieeeTransportationFixture{ieeeOODDMFixture: &ieeeOODDMFixture{ctx: ctx, queues: make(map[core.FederateHandle]<-chan []core.OutboundEvent)}}
	f.admission = &transportationAdmission{}
	repo := composition.NewFOMRepository()
	modules := []core.FOMModule{{Path: "transportation.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>TransportItem</name><attribute><name>value</name><dataType>HLAinteger32BE</dataType><transportation>HLAreliable</transportation></attribute></objectClass></objectClass></objects></objectModel>`)}}
	fom, err := repo.Load(ctx, modules)
	ieeeOODDMOK(t, err)
	clock := core.NewManualClock(time.Unix(0, 0))
	members, err := federation.New(federation.Options{Clock: clock, FOMs: repo})
	ieeeOODDMOK(t, err)
	sessions, err := session.New(session.Options{Membership: members})
	ieeeOODDMOK(t, err)
	t.Cleanup(sessions.Close)
	ieeeOODDMOK(t, members.CreateFederation(ctx, core.CreateFederationRequest{Name: ieeeOODDMFederation, FOMModules: modules}))
	repo.RememberFor(ieeeOODDMFederation, fom)
	for i, name := range []string{"owner", "acquirer"} {
		h, err := members.JoinFederation(ctx, core.JoinFederationRequest{Federation: ieeeOODDMFederation, FederateName: name})
		ieeeOODDMOK(t, err)
		*[]*core.FederateHandle{&f.owner, &f.acquirer}[i] = h
		id, err := members.MemberIdentity(ieeeOODDMFederation, h)
		ieeeOODDMOK(t, err)
		token, err := session.NewToken()
		ieeeOODDMOK(t, err)
		sessions.Register(id, token)
		*[]*context.Context{&f.ownerContext, &f.peerContext}[i] = metadata.NewOutgoingContext(ctx, metadata.Pairs(session.MetadataKey, string(token)))
	}
	var found bool
	f.cls, found = fom.LookupObjectClass("TransportItem")
	if !found {
		t.Fatal("missing transportation class")
	}
	f.value, found = fom.LookupAttribute(f.cls, "value")
	if !found {
		t.Fatal("missing transportation attribute")
	}
	out := &transportationEpochOutbox{MultiOutbox: delivery.NewWithBatch(128, 1, 0)}
	out.epoch.Store(1)
	f.epochs = out
	for _, h := range []core.FederateHandle{f.owner, f.acquirer} {
		queue, closeQueue, err := out.Subscribe(ctx, ieeeOODDMFederation, h)
		ieeeOODDMOK(t, err)
		t.Cleanup(func() { _ = closeQueue() })
		f.queues[h] = queue
	}
	f.decl = declaration.New()
	f.decl.SetFOMRepository(repo)
	f.owners, err = ownership.New(ownership.Options{Outbox: out,
		PrepareAcquisition: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			return f.objects.PrepareOwnershipAcquisition(ctx, fed, h, obj, attrs)
		},
		PrepareDivestiture: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			return f.objects.PrepareOwnershipReleaseDenied(ctx, fed, h, obj, attrs)
		},
	})
	ieeeOODDMOK(t, err)
	f.objects, err = object.New(object.Options{Clock: clock, FOMs: repo, Declarations: f.decl, Outbox: out, Ownership: f.owners,
		OnRegister: func(fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			f.owners.RegisterInitialOwnership(fed, h, obj, attrs)
		},
	})
	ieeeOODDMOK(t, err)
	for _, h := range []core.FederateHandle{f.owner, f.acquirer} {
		ieeeOODDMOK(t, f.decl.PublishObjectClassAttributes(ctx, ieeeOODDMFederation, h, f.cls, []core.AttributeHandle{f.value}))
	}
	ieeeOODDMOK(t, f.decl.SubscribeObjectClassAttributes(ctx, ieeeOODDMFederation, f.acquirer, f.cls, []core.AttributeHandle{f.value}))
	svc, err := grpcsvc.NewServer(grpcsvc.Options{Federations: members, Membership: members, Sessions: sessions, CheckpointAdmission: f.admission, Declarations: f.decl, Objects: f.objects, Ownership: f.owners, Outbox: out, FOMs: repo})
	ieeeOODDMOK(t, err)
	server := grpcgo.NewServer(grpcgo.UnaryInterceptor(svc.UnaryMembershipInterceptor()))
	ieeeOODDMOK(t, svc.Register(server))
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpcgo.NewClient("passthrough:///transportation", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	ieeeOODDMOK(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	f.objClient, f.ownClient = rtiv1.NewObjectServiceClient(conn), rtiv1.NewOwnershipServiceClient(conn)
	registered, err := f.objClient.RegisterObjectInstance(ctx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectClassHandle: uint64(f.cls)})
	ieeeOODDMOK(t, err)
	f.obj = core.ObjectHandle(registered.GetObjectHandle())
	return f
}
