package m37spec

import (
	"context"
	"net"
	"reflect"
	"testing"
	"time"

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

const divestFollowupFed = core.FederationName("ieee2010-divest-followup")

type divestFollowupFixture struct {
	ctx                       context.Context
	owners                    *ownership.Manager
	objects                   *object.Registry
	decl                      *declaration.Manager
	client                    rtiv1.OwnershipServiceClient
	owner, acquirer, stranger core.FederateHandle
	obj                       core.ObjectHandle
	cls                       core.ObjectClassHandle
	value, other              core.AttributeHandle
	queues                    map[core.FederateHandle]<-chan []core.OutboundEvent
}

func divestFollowupOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newDivestFollowupFixture(t *testing.T) *divestFollowupFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	f := &divestFollowupFixture{ctx: ctx, queues: make(map[core.FederateHandle]<-chan []core.OutboundEvent)}
	repo := composition.NewFOMRepository()
	modules := []core.FOMModule{{Path: "divest-followup.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>value</name><dataType>HLAinteger32BE</dataType></attribute><attribute><name>other</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objects></objectModel>`)}}
	fom, err := repo.Load(ctx, modules)
	divestFollowupOK(t, err)
	clock := core.NewManualClock(time.Unix(0, 0))
	members, err := federation.New(federation.Options{Clock: clock, FOMs: repo})
	divestFollowupOK(t, err)
	divestFollowupOK(t, members.CreateFederation(ctx, core.CreateFederationRequest{Name: divestFollowupFed, FOMModules: modules}))
	repo.RememberFor(divestFollowupFed, fom)
	for i, name := range []string{"owner", "acquirer", "stranger"} {
		h, err := members.JoinFederation(ctx, core.JoinFederationRequest{Federation: divestFollowupFed, FederateName: name})
		divestFollowupOK(t, err)
		*[]*core.FederateHandle{&f.owner, &f.acquirer, &f.stranger}[i] = h
	}
	var ok bool
	if f.cls, ok = fom.LookupObjectClass("Vehicle"); !ok {
		t.Fatal("Vehicle missing")
	}
	if f.value, ok = fom.LookupAttribute(f.cls, "value"); !ok {
		t.Fatal("value missing")
	}
	if f.other, ok = fom.LookupAttribute(f.cls, "other"); !ok {
		t.Fatal("other missing")
	}
	out := delivery.NewWithBatch(128, 1, 0)
	for _, h := range []core.FederateHandle{f.owner, f.acquirer, f.stranger} {
		queue, closeQueue, err := out.Subscribe(ctx, divestFollowupFed, h)
		divestFollowupOK(t, err)
		t.Cleanup(func() { _ = closeQueue() })
		f.queues[h] = queue
	}
	f.decl = declaration.New()
	f.decl.SetFOMRepository(repo)
	f.owners, err = ownership.New(ownership.Options{
		Outbox: out,
		PrepareAcquisition: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			return f.objects.PrepareOwnershipAcquisition(ctx, fed, h, obj, attrs)
		},
		PrepareDivestiture: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			return f.objects.PrepareOwnershipReleaseDenied(ctx, fed, h, obj, attrs)
		},
		Subscribers: func(ctx context.Context, fed core.FederationName, _ core.ObjectHandle, attrs []core.AttributeHandle) []core.FederateHandle {
			return f.decl.SubscribersFor(ctx, fed, f.cls, attrs)
		},
	})
	divestFollowupOK(t, err)
	f.objects, err = object.New(object.Options{Clock: clock, FOMs: repo, Declarations: f.decl, Outbox: out, Ownership: f.owners,
		OnRegister: func(fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			f.owners.RegisterInitialOwnership(fed, h, obj, attrs)
		},
		OnDelete: f.owners.OnObjectDeleted,
	})
	divestFollowupOK(t, err)
	f.decl.SetOnSubscribeObjectClass(f.objects.ObjectClassSubscribed)
	for _, h := range []core.FederateHandle{f.owner, f.acquirer} {
		divestFollowupOK(t, f.decl.PublishObjectClassAttributes(ctx, divestFollowupFed, h, f.cls, []core.AttributeHandle{f.value, f.other}))
	}
	divestFollowupOK(t, f.decl.SubscribeObjectClassAttributes(ctx, divestFollowupFed, f.acquirer, f.cls, []core.AttributeHandle{f.value, f.other}))
	svc, err := grpcsvc.NewServer(grpcsvc.Options{Federations: members, Membership: members, Declarations: f.decl, Objects: f.objects, Ownership: f.owners, Outbox: out, FOMs: repo})
	divestFollowupOK(t, err)
	server := grpcgo.NewServer(grpcgo.UnaryInterceptor(svc.UnaryMembershipInterceptor()))
	divestFollowupOK(t, svc.Register(server))
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpcgo.NewClient("passthrough:///divest-followup", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	divestFollowupOK(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	f.client = rtiv1.NewOwnershipServiceClient(conn)
	registered, err := rtiv1.NewObjectServiceClient(conn).RegisterObjectInstance(ctx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: string(divestFollowupFed), FederateHandle: uint64(f.owner), ObjectClassHandle: uint64(f.cls)})
	divestFollowupOK(t, err)
	f.obj = core.ObjectHandle(registered.ObjectHandle)
	f.drain()
	return f
}

func (f *divestFollowupFixture) drain() []core.OutboxDelivery {
	var events []core.OutboxDelivery
	for recipient, queue := range f.queues {
	draining:
		for {
			select {
			case batch := <-queue:
				for _, event := range batch {
					events = append(events, core.OutboxDelivery{Recipient: recipient, Event: event})
				}
			default:
				break draining
			}
		}
	}
	return events
}

func (f *divestFollowupFixture) call(op string, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, opts ...grpcgo.CallOption) error {
	handles := make([]uint64, len(attrs))
	for i, attr := range attrs {
		handles[i] = uint64(attr)
	}
	var err error
	switch op {
	case "unconditional":
		_, err = f.client.UnconditionalAttributeOwnershipDivestiture(f.ctx, &rtiv1.UnconditionalDivestRequest{WireVersion: 1, FederationName: string(divestFollowupFed), FederateHandle: uint64(h), ObjectHandle: uint64(obj), AttributeHandles: handles}, opts...)
	case "negotiated":
		_, err = f.client.NegotiatedAttributeOwnershipDivestiture(f.ctx, &rtiv1.NegotiatedDivestRequest{WireVersion: 1, FederationName: string(divestFollowupFed), FederateHandle: uint64(h), ObjectHandle: uint64(obj), AttributeHandles: handles, TwoPhase: true, Tag: []byte("offer")}, opts...)
	case confirmServiceName:
		_, err = f.client.ConfirmDivestiture(f.ctx, &rtiv1.ConfirmDivestitureRequest{WireVersion: 1, FederationName: string(divestFollowupFed), FederateHandle: uint64(h), ObjectHandle: uint64(obj), AttributeHandles: handles, Tag: []byte(confirmServiceName)}, opts...)
	default:
		panic("unknown divest operation")
	}
	return err
}

func (f *divestFollowupFixture) acquire(t *testing.T, attrs []core.AttributeHandle) {
	t.Helper()
	handles := make([]uint64, len(attrs))
	for i, attr := range attrs {
		handles[i] = uint64(attr)
	}
	_, err := f.client.AttributeOwnershipAcquisition(f.ctx, &rtiv1.AcquireRequest{WireVersion: 1, FederationName: string(divestFollowupFed), FederateHandle: uint64(f.acquirer), ObjectHandle: uint64(f.obj), AttributeHandles: handles, Tag: []byte("acquire")})
	divestFollowupOK(t, err)
}

// Active IEEE 1516.1-2010 7.2.5(a-c), 7.3.5(b-d), 7.6.5(c-e),
// supplied redline PDF pp. 161-162, 165-166. Real serialization, membership,
// parsed FOM, object preparation and callback outboxes; no Python binding claim.
func TestIEEE2010DivestFollowupRealWire(t *testing.T) {
	for _, op := range []string{"unconditional", "negotiated", confirmServiceName} {
		for _, name := range []string{"missing-object", "unknown-caller", "unknown-empty-set", "undefined", "mixed-undefined", "undefined-first", "nonowner", "mixed-nonowner", "nonowner-first"} {
			t.Run(op+"/"+name, func(t *testing.T) {
				f := newDivestFollowupFixture(t)
				if name == "mixed-nonowner" || name == "nonowner-first" {
					divestFollowupOK(t, f.call("unconditional", f.owner, f.obj, []core.AttributeHandle{f.other}))
					f.acquire(t, []core.AttributeHandle{f.other})
				}
				f.acquire(t, []core.AttributeHandle{f.value})
				if op == confirmServiceName {
					divestFollowupOK(t, f.call("negotiated", f.owner, f.obj, []core.AttributeHandle{f.value}))
				}
				f.drain()
				caller, obj, attrs, want := f.owner, f.obj, []core.AttributeHandle{f.value}, "ObjectInstanceNotKnown"
				switch name {
				case "missing-object":
					obj = ^core.ObjectHandle(0)
				case "unknown-caller":
					caller = f.stranger
				case "unknown-empty-set":
					caller, attrs = f.stranger, nil
				case "undefined":
					attrs, want = []core.AttributeHandle{999999}, attributeNotDefinedErrorName
				case "mixed-undefined":
					attrs, want = []core.AttributeHandle{f.value, 999999}, attributeNotDefinedErrorName
				case "undefined-first":
					attrs, want = []core.AttributeHandle{999999, f.value}, attributeNotDefinedErrorName
				case "nonowner":
					caller, want = f.acquirer, attributeNotOwnedErrorName
				case "mixed-nonowner":
					attrs, want = []core.AttributeHandle{f.value, f.other}, attributeNotOwnedErrorName
				case "nonowner-first":
					attrs, want = []core.AttributeHandle{f.other, f.value}, attributeNotOwnedErrorName
				}
				before := f.owners.ExportState(divestFollowupFed)
				objectsBefore := f.objects.ExportState(divestFollowupFed)
				if owner, owned := f.owners.QueryOwnership(divestFollowupFed, f.obj, f.value); !owned || owner != f.owner {
					t.Fatalf("before rejection: owner=%d owned=%t, want original owner %d", owner, owned, f.owner)
				}
				wantPending := []ownership.PendingAcquireState{{Object: f.obj, Attribute: f.value, Acquirer: f.acquirer, Tag: []byte("acquire")}}
				if !reflect.DeepEqual(before.PendingAcquires, wantPending) {
					t.Fatalf("before rejection: pending acquisitions=%+v, want %+v", before.PendingAcquires, wantPending)
				}
				var trailer metadata.MD
				err := f.call(op, caller, obj, attrs, grpcgo.Trailer(&trailer))
				if err == nil || !reflect.DeepEqual(trailer.Get("rti-spec-exception"), []string{want}) {
					t.Errorf("error=%v trailer=%v; want %s", err, trailer, want)
				}
				if !reflect.DeepEqual(before, f.owners.ExportState(divestFollowupFed)) || !reflect.DeepEqual(objectsBefore, f.objects.ExportState(divestFollowupFed)) || len(f.drain()) != 0 {
					t.Fatal("invalid request changed ownership/pending state or emitted callbacks")
				}
				divestFollowupOK(t, f.call(op, f.owner, f.obj, []core.AttributeHandle{f.value}))
				if op == "negotiated" {
					divestFollowupOK(t, f.call(confirmServiceName, f.owner, f.obj, []core.AttributeHandle{f.value}))
				}
				owner, owned := f.owners.QueryOwnership(divestFollowupFed, f.obj, f.value)
				// 7.2 permits a temporary unowned state; the queued acquirer can
				// already own the attribute when unconditional divestiture returns.
				if !owned || owner != f.acquirer {
					t.Fatalf("successful retry: owner=%d owned=%t, want queued acquirer %d", owner, owned, f.acquirer)
				}
				after := f.owners.ExportState(divestFollowupFed)
				if len(after.PendingAcquires) != 0 || len(after.PendingDivests) != 0 {
					t.Fatalf("successful retry left pending ownership requests: %+v", after)
				}
				wantTag := confirmServiceName
				if op == "unconditional" {
					wantTag = ""
				}
				acquired := 0
				for _, delivery := range f.drain() {
					if callback, ok := delivery.Event.(*ownership.CallbackEvent); ok && callback.Kind == ownership.CallbackAcquisitionNotification {
						acquired++
						if delivery.Recipient != f.acquirer || callback.Federate != f.acquirer || callback.Object != f.obj || !reflect.DeepEqual(callback.Attributes, []core.AttributeHandle{f.value}) {
							t.Errorf("acquisition recipient=%d callback=%+v; want acquirer=%d object=%d attribute=%d", delivery.Recipient, callback, f.acquirer, f.obj, f.value)
						}
						if string(callback.Tag) != wantTag {
							t.Errorf("acquisition tag=%q, want %q", callback.Tag, wantTag)
						}
					}
				}
				if acquired != 1 {
					t.Fatalf("acquisition callbacks=%d, want 1", acquired)
				}
			})
		}
	}
}

func TestIEEE2010DivestDoesNotRequirePublicationRealWire(t *testing.T) {
	for _, op := range []string{"unconditional", "negotiated", confirmServiceName} {
		t.Run(op, func(t *testing.T) {
			f := newDivestFollowupFixture(t)
			f.acquire(t, []core.AttributeHandle{f.value})
			if op == confirmServiceName {
				divestFollowupOK(t, f.call("negotiated", f.owner, f.obj, []core.AttributeHandle{f.value}))
			}
			divestFollowupOK(t, f.decl.UnpublishObjectClassAttributes(f.ctx, divestFollowupFed, f.owner, f.cls, []core.AttributeHandle{f.value, f.other}))
			divestFollowupOK(t, f.call(op, f.owner, f.obj, []core.AttributeHandle{f.value}))
		})
	}
}

const confirmServiceName = "confirm"

const attributeNotDefinedErrorName = "AttributeNotDefined"

const attributeNotOwnedErrorName = "AttributeNotOwned"
