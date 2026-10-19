package grpc_test

import (
	"bytes"
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
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

const ieeeOODDMFederation = "ieee-ooddm"

type ieeeOODDMFixture struct {
	ctx                       context.Context
	foms                      core.FOMRepository
	objects                   *object.Registry
	owners                    *ownership.Manager
	regions                   *ddm.Manager
	decl                      *declaration.Manager
	objClient                 rtiv1.ObjectServiceClient
	ownClient                 rtiv1.OwnershipServiceClient
	ddmClient                 rtiv1.DDMServiceClient
	owner, acquirer, stranger core.FederateHandle
	cls                       core.ObjectClassHandle
	obj                       core.ObjectHandle
	value, other, privilege   core.AttributeHandle
	queues                    map[core.FederateHandle]<-chan []core.OutboundEvent
}

func newIEEEOODDMFixture(t *testing.T, configure ...func(*ownership.Options, *ieeeOODDMFixture)) *ieeeOODDMFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	f := &ieeeOODDMFixture{ctx: ctx, queues: make(map[core.FederateHandle]<-chan []core.OutboundEvent)}
	repo := composition.NewFOMRepository()
	f.foms = repo
	modules := []core.FOMModule{{Path: "ieee-ooddm.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>value</name><dataType>HLAinteger32BE</dataType><dimensions><dimension>X</dimension></dimensions></attribute><attribute><name>other</name><dataType>HLAinteger32BE</dataType><dimensions><dimension>X</dimension></dimensions></attribute></objectClass><objectClass><name>Secret</name><attribute><name>foreign</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objects><dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension><dimension><name>Y</name><upperBound>100</upperBound></dimension></dimensions></objectModel>`)}}
	fom, err := repo.Load(ctx, modules)
	ieeeOODDMOK(t, err)
	clock := core.NewManualClock(time.Unix(0, 0))
	members, err := federation.New(federation.Options{Clock: clock, FOMs: repo})
	ieeeOODDMOK(t, err)
	ieeeOODDMOK(t, members.CreateFederation(ctx, core.CreateFederationRequest{Name: ieeeOODDMFederation, FOMModules: modules}))
	repo.RememberFor(ieeeOODDMFederation, fom)
	for i, name := range []string{"owner", "acquirer", "stranger"} {
		h, err := members.JoinFederation(ctx, core.JoinFederationRequest{Federation: ieeeOODDMFederation, FederateName: name})
		ieeeOODDMOK(t, err)
		*[]*core.FederateHandle{&f.owner, &f.acquirer, &f.stranger}[i] = h
	}
	var ok bool
	if f.cls, ok = fom.LookupObjectClass("Vehicle"); !ok {
		t.Fatal("missing Vehicle")
	}
	if f.value, ok = fom.LookupAttribute(f.cls, "value"); !ok {
		t.Fatal("missing value")
	}
	if f.other, ok = fom.LookupAttribute(f.cls, "other"); !ok {
		t.Fatal("missing other")
	}
	if f.privilege, ok = fom.LookupAttribute(f.cls, "HLAprivilegeToDeleteObject"); !ok {
		t.Fatal("missing delete privilege")
	}
	out := delivery.NewWithBatch(128, 1, 0)
	for _, h := range []core.FederateHandle{f.owner, f.acquirer, f.stranger} {
		queue, closeQueue, err := out.Subscribe(ctx, ieeeOODDMFederation, h)
		ieeeOODDMOK(t, err)
		t.Cleanup(func() { _ = closeQueue() })
		f.queues[h] = queue
	}
	f.decl = declaration.New()
	f.decl.SetFOMRepository(repo)
	ownershipOptions := ownership.Options{Outbox: out, PrepareAcquisition: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
		return f.objects.PrepareOwnershipAcquisition(ctx, fed, h, obj, attrs)
	}}
	for _, setup := range configure {
		setup(&ownershipOptions, f)
	}
	f.owners, err = ownership.New(ownershipOptions)
	ieeeOODDMOK(t, err)
	f.objects, err = object.New(object.Options{Clock: clock, FOMs: repo, Declarations: f.decl, Outbox: out, Ownership: f.owners,
		OnRegister: func(fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			f.owners.RegisterInitialOwnership(fed, h, obj, attrs)
		},
		OnDelete: f.owners.OnObjectDeleted,
	})
	ieeeOODDMOK(t, err)
	f.decl.SetOnSubscribeObjectClass(f.objects.ObjectClassSubscribed)
	f.regions, err = ddm.New(ddm.Options{Outbox: out, FOMs: repo, Objects: f.objects})
	ieeeOODDMOK(t, err)
	for _, h := range []core.FederateHandle{f.owner, f.acquirer} {
		ieeeOODDMOK(t, f.decl.PublishObjectClassAttributes(ctx, ieeeOODDMFederation, h, f.cls, []core.AttributeHandle{f.value, f.other, f.privilege}))
	}
	ieeeOODDMOK(t, f.decl.SubscribeObjectClassAttributes(ctx, ieeeOODDMFederation, f.acquirer, f.cls, []core.AttributeHandle{f.value, f.other}))
	svc, err := grpcsvc.NewServer(grpcsvc.Options{Federations: members, Membership: members, Declarations: f.decl, Objects: f.objects, Ownership: f.owners, DDM: f.regions, Outbox: out, FOMs: repo})
	ieeeOODDMOK(t, err)
	server := grpcgo.NewServer(grpcgo.UnaryInterceptor(svc.UnaryMembershipInterceptor()))
	ieeeOODDMOK(t, svc.Register(server))
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpcgo.NewClient("passthrough:///ieee-ooddm", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	ieeeOODDMOK(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	f.objClient, f.ownClient, f.ddmClient = rtiv1.NewObjectServiceClient(conn), rtiv1.NewOwnershipServiceClient(conn), rtiv1.NewDDMServiceClient(conn)
	registered, err := f.objClient.RegisterObjectInstance(ctx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectClassHandle: uint64(f.cls)})
	ieeeOODDMOK(t, err)
	f.obj = core.ObjectHandle(registered.ObjectHandle)
	for h := range f.queues {
		f.drain(h)
	}
	return f
}

func ieeeOODDMOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func ieeeOODDMException(t *testing.T, err error, trailer metadata.MD, want string) {
	t.Helper()
	if err == nil || !reflect.DeepEqual(trailer.Get("rti-spec-exception"), []string{want}) {
		t.Fatalf("error=%v trailer=%v; want %s", err, trailer, want)
	}
}

func (f *ieeeOODDMFixture) drain(h core.FederateHandle) []core.OutboundEvent {
	var events []core.OutboundEvent
	for {
		select {
		case batch := <-f.queues[h]:
			events = append(events, batch...)
		default:
			return events
		}
	}
}

func (f *ieeeOODDMFixture) acquire(h core.FederateHandle, attrs []core.AttributeHandle, available bool, tag []byte, opts ...grpcgo.CallOption) error {
	handles := make([]uint64, len(attrs))
	for i, a := range attrs {
		handles[i] = uint64(a)
	}
	_, err := f.ownClient.AttributeOwnershipAcquisition(f.ctx, &rtiv1.AcquireRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(h), ObjectHandle: uint64(f.obj), AttributeHandles: handles, IfAvailable: available, Tag: tag}, opts...)
	return err
}

// IEEE 1516.1-2010 7.8, supplied redline PDF pp. 167-169.
func TestIEEE2010OwnershipRepeatedAcquisitionRealWire(t *testing.T) {
	for _, negotiated := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "negotiated"}[negotiated], func(t *testing.T) {
			f := newIEEEOODDMFixture(t)
			if negotiated {
				_, err := f.ownClient.NegotiatedAttributeOwnershipDivestiture(f.ctx, &rtiv1.NegotiatedDivestRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.value), uint64(f.other)}, TwoPhase: true})
				ieeeOODDMOK(t, err)
			}
			firstTag := []byte{0, 255, 1}
			ieeeOODDMOK(t, f.acquire(f.acquirer, []core.AttributeHandle{f.value}, false, firstTag))
			f.drain(f.owner)
			before := f.owners.ExportState(ieeeOODDMFederation)
			ieeeOODDMOK(t, f.acquire(f.acquirer, []core.AttributeHandle{f.value}, false, []byte("ignored")))
			if !reflect.DeepEqual(before, f.owners.ExportState(ieeeOODDMFederation)) || len(f.drain(f.owner)) != 0 {
				t.Fatal("repeat changed pending state or duplicated callback")
			}
			errors := make(chan error, 16)
			for i := 0; i < cap(errors); i++ {
				go func() { errors <- f.acquire(f.acquirer, []core.AttributeHandle{f.value}, false, []byte("concurrent")) }()
			}
			for i := 0; i < cap(errors); i++ {
				ieeeOODDMOK(t, <-errors)
			}
			if !reflect.DeepEqual(before, f.owners.ExportState(ieeeOODDMFederation)) || len(f.drain(f.owner)) != 0 {
				t.Fatal("concurrent repeats changed state or duplicated callback")
			}
			ieeeOODDMOK(t, f.acquire(f.acquirer, []core.AttributeHandle{f.value, f.other}, false, []byte("new")))
			state := f.owners.ExportState(ieeeOODDMFederation)
			if len(state.PendingAcquires) != 2 {
				t.Fatalf("pending=%v", state.PendingAcquires)
			}
			for _, p := range state.PendingAcquires {
				if p.Attribute == f.value && !bytes.Equal(p.Tag, firstTag) {
					t.Fatal("repeat replaced original tag")
				}
			}
			events := f.drain(f.owner)
			if len(events) != 1 {
				t.Fatalf("new attribute callbacks=%v", events)
			}
			e := events[0].(*ownership.CallbackEvent)
			wantKind := ownership.CallbackReleaseRequest
			if negotiated {
				wantKind = ownership.CallbackDivestitureConfirmation
			}
			if e.Kind != wantKind || !reflect.DeepEqual(e.Attributes, []core.AttributeHandle{f.other}) {
				t.Fatalf("callback=%+v", e)
			}
		})
	}
}

// IEEE 1516.1-2010 7.9.3(h), 7.9.5(a), supplied PDF p. 170.
func TestIEEE2010OwnershipIfAvailablePendingRealWire(t *testing.T) {
	f := newIEEEOODDMFixture(t)
	ieeeOODDMOK(t, f.acquire(f.acquirer, []core.AttributeHandle{f.value}, false, []byte("pending")))
	f.drain(f.owner)
	before := f.owners.ExportState(ieeeOODDMFederation)
	var trailer metadata.MD
	err := f.acquire(f.acquirer, []core.AttributeHandle{f.other, f.value}, true, nil, grpcgo.Trailer(&trailer))
	ieeeOODDMException(t, err, trailer, "AttributeAlreadyBeingAcquired")
	if !reflect.DeepEqual(before, f.owners.ExportState(ieeeOODDMFederation)) || len(f.drain(f.acquirer)) != 0 {
		t.Fatal("rejected request changed state or emitted callback")
	}
}

// IEEE 1516.1-2010 6.14.3(e), 6.14.5(c), supplied PDF p. 132.
func TestIEEE2010ObjectDeleteKnowledgeRealWire(t *testing.T) {
	f := newIEEEOODDMFixture(t)
	before := f.owners.ExportState(ieeeOODDMFederation)
	for _, tc := range []struct {
		caller    core.FederateHandle
		exception string
	}{{f.stranger, "ObjectInstanceNotKnown"}, {f.acquirer, "DeletePrivilegeNotHeld"}, {^core.FederateHandle(0), "FederateNotExecutionMember"}} {
		var trailer metadata.MD
		_, err := f.objClient.DeleteObjectInstance(f.ctx, &rtiv1.DeleteObjectInstanceRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(tc.caller), ObjectHandle: uint64(f.obj)}, grpcgo.Trailer(&trailer))
		ieeeOODDMException(t, err, trailer, tc.exception)
	}
	if err := f.objects.Delete(f.ctx, ieeeOODDMFederation, ^core.FederateHandle(0), f.obj, nil, nil); err != core.ErrObjectHandleInvalid {
		t.Fatalf("internal identity bypassed knowledge for a federate-owned object: %v", err)
	}
	if !reflect.DeepEqual(before, f.owners.ExportState(ieeeOODDMFederation)) {
		t.Fatal("rejected deletion changed ownership")
	}
	if _, _, known := f.objects.LookupKnownObjectInstance(ieeeOODDMFederation, f.owner, f.obj); !known {
		t.Fatal("rejected deletion removed object")
	}
}

// IEEE 1516.1-2010 6.14 and 7.6, supplied PDF pp. 131-132, 165-166.
func TestIEEE2010ObjectTransferredDeletePrivilegeRealWire(t *testing.T) {
	f := newIEEEOODDMFixture(t)
	ieeeOODDMOK(t, f.decl.SubscribeObjectClassAttributes(f.ctx, ieeeOODDMFederation, f.stranger, f.cls, []core.AttributeHandle{f.value}))
	ieeeOODDMOK(t, f.decl.UnsubscribeObjectClassAttributes(f.ctx, ieeeOODDMFederation, f.stranger, f.cls, nil))
	ieeeOODDMOK(t, f.acquire(f.acquirer, []core.AttributeHandle{f.privilege}, false, nil))
	_, err := f.ownClient.NegotiatedAttributeOwnershipDivestiture(f.ctx, &rtiv1.NegotiatedDivestRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.privilege)}, TwoPhase: true})
	ieeeOODDMOK(t, err)
	_, err = f.ownClient.ConfirmDivestiture(f.ctx, &rtiv1.ConfirmDivestitureRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.privilege)}})
	ieeeOODDMOK(t, err)
	if !f.owners.IsOwnedBy(ieeeOODDMFederation, f.acquirer, f.obj, f.privilege) {
		t.Fatal("delete privilege not transferred")
	}
	var trailer metadata.MD
	_, err = f.objClient.DeleteObjectInstance(f.ctx, &rtiv1.DeleteObjectInstanceRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectHandle: uint64(f.obj)}, grpcgo.Trailer(&trailer))
	ieeeOODDMException(t, err, trailer, "DeletePrivilegeNotHeld")
	for h := range f.queues {
		f.drain(h)
	}
	tag := []byte{0, 255, 9}
	_, err = f.objClient.DeleteObjectInstance(f.ctx, &rtiv1.DeleteObjectInstanceRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.acquirer), ObjectHandle: uint64(f.obj), UserSuppliedTag: tag})
	ieeeOODDMOK(t, err)
	for _, h := range []core.FederateHandle{f.owner, f.stranger} {
		events := f.drain(h)
		if len(events) != 1 {
			t.Fatalf("recipient %d removals=%v", h, events)
		}
		e := events[0].(*object.CallbackEvent)
		if e.Kind != object.CallbackRemove || !bytes.Equal(e.UserSuppliedTag, tag) {
			t.Fatalf("removal=%+v", e)
		}
	}
	if len(f.drain(f.acquirer)) != 0 {
		t.Fatal("deleter received its own removal")
	}
	for h := range f.queues {
		if _, _, known := f.objects.LookupKnownObjectInstance(ieeeOODDMFederation, h, f.obj); known {
			t.Fatalf("deleted object still known by %d", h)
		}
	}
	if len(f.owners.ExportState(ieeeOODDMFederation).Owners) != 0 {
		t.Fatal("deleted object retained ownership")
	}
}

// IEEE 1516.1-2010 6.16, supplied PDF pp. 135-137.
func TestIEEE2010ObjectLocalDeleteRealWire(t *testing.T) {
	for _, mode := range []string{"owner", "pending", "unknown", "forget"} {
		t.Run(mode, func(t *testing.T) {
			f := newIEEEOODDMFixture(t)
			caller, want := f.acquirer, ""
			switch mode {
			case "owner":
				caller, want = f.owner, "FederateOwnsAttributes"
			case "pending":
				ieeeOODDMOK(t, f.acquire(f.acquirer, []core.AttributeHandle{f.value}, false, nil))
				want = "OwnershipAcquisitionPending"
			case "unknown":
				caller, want = f.stranger, "ObjectInstanceNotKnown"
			case "forget":
				ieeeOODDMOK(t, f.decl.UnsubscribeObjectClassAttributes(f.ctx, ieeeOODDMFederation, f.acquirer, f.cls, nil))
			}
			before := f.owners.ExportState(ieeeOODDMFederation)
			for h := range f.queues {
				f.drain(h)
			}
			var trailer metadata.MD
			_, err := f.objClient.LocalDeleteObjectInstance(f.ctx, &rtiv1.LocalDeleteObjectInstanceRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(caller), ObjectHandle: uint64(f.obj)}, grpcgo.Trailer(&trailer))
			if want != "" {
				ieeeOODDMException(t, err, trailer, want)
			} else {
				ieeeOODDMOK(t, err)
				if _, _, known := f.objects.LookupKnownObjectInstance(ieeeOODDMFederation, caller, f.obj); known {
					t.Fatal("local delete retained caller knowledge")
				}
			}
			if _, _, known := f.objects.LookupKnownObjectInstance(ieeeOODDMFederation, f.owner, f.obj); !known {
				t.Fatal("local delete removed shared object")
			}
			if !reflect.DeepEqual(before, f.owners.ExportState(ieeeOODDMFederation)) {
				t.Fatal("local deletion changed ownership")
			}
			for h := range f.queues {
				if len(f.drain(h)) != 0 {
					t.Fatalf("unexpected local-delete callback to %d", h)
				}
			}
		})
	}
}

// IEEE 1516.1-2010 7.1.5, 7.8.3 and 7.8.5, supplied PDF pp. 160, 168-169.
func TestIEEE2010OwnershipAcquisitionPreconditionsRealWire(t *testing.T) {
	for _, mode := range []string{"unknown", "attribute", "unpublished-class", "unpublished-attribute", "already-owned"} {
		t.Run(mode, func(t *testing.T) {
			f := newIEEEOODDMFixture(t)
			caller, attrs, want := f.acquirer, []core.AttributeHandle{f.value, f.other}, ""
			switch mode {
			case "unknown":
				caller, want = f.stranger, "ObjectInstanceNotKnown"
			case "attribute":
				attrs, want = []core.AttributeHandle{f.value, 999999}, "AttributeNotDefined"
			case "unpublished-class":
				ieeeOODDMOK(t, f.decl.UnpublishObjectClassAttributes(f.ctx, ieeeOODDMFederation, caller, f.cls, nil))
				want = "ObjectClassNotPublished"
			case "unpublished-attribute":
				ieeeOODDMOK(t, f.decl.UnpublishObjectClassAttributes(f.ctx, ieeeOODDMFederation, caller, f.cls, []core.AttributeHandle{f.other}))
				want = "AttributeNotPublished"
			case "already-owned":
				caller, want = f.owner, "FederateOwnsAttributes"
			}
			before := f.owners.ExportState(ieeeOODDMFederation)
			var trailer metadata.MD
			err := f.acquire(caller, attrs, false, nil, grpcgo.Trailer(&trailer))
			ieeeOODDMException(t, err, trailer, want)
			if !reflect.DeepEqual(before, f.owners.ExportState(ieeeOODDMFederation)) {
				t.Fatal("failed acquisition partially mutated ownership")
			}
			for h := range f.queues {
				if len(f.drain(h)) != 0 {
					t.Fatal("failed acquisition emitted a callback")
				}
			}
		})
	}
}

// IEEE 1516.1-2010 7.3, 7.5 and 7.6, supplied PDF pp. 161-162, 164-166.
func TestIEEE2010OwnershipNegotiatedConfirmationRealWire(t *testing.T) {
	f := newIEEEOODDMFixture(t)
	request := &rtiv1.NegotiatedDivestRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.value)}, TwoPhase: true}
	_, err := f.ownClient.NegotiatedAttributeOwnershipDivestiture(f.ctx, request)
	ieeeOODDMOK(t, err)
	before := f.owners.ExportState(ieeeOODDMFederation)
	request.AttributeHandles = []uint64{uint64(f.other), uint64(f.value)}
	var trailer metadata.MD
	_, err = f.ownClient.NegotiatedAttributeOwnershipDivestiture(f.ctx, request, grpcgo.Trailer(&trailer))
	ieeeOODDMException(t, err, trailer, "AttributeAlreadyBeingDivested")
	if !reflect.DeepEqual(before, f.owners.ExportState(ieeeOODDMFederation)) {
		t.Fatal("duplicate divest partially changed set")
	}
	ieeeOODDMOK(t, f.acquire(f.acquirer, []core.AttributeHandle{f.value}, false, []byte("acquire")))
	if !f.owners.IsOwnedBy(ieeeOODDMFederation, f.owner, f.obj, f.value) {
		t.Fatal("ownership transferred before confirmation")
	}
	events := f.drain(f.owner)
	if len(events) != 1 || events[0].(*ownership.CallbackEvent).Kind != ownership.CallbackDivestitureConfirmation {
		t.Fatalf("confirmation request=%v", events)
	}
	if len(f.drain(f.acquirer)) != 0 {
		t.Fatal("acquisition notified before confirmation")
	}
	tag := []byte{0, 128, 255}
	_, err = f.ownClient.ConfirmDivestiture(f.ctx, &rtiv1.ConfirmDivestitureRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.owner), ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.value)}, Tag: tag})
	ieeeOODDMOK(t, err)
	if !f.owners.IsOwnedBy(ieeeOODDMFederation, f.acquirer, f.obj, f.value) {
		t.Fatal("confirmation did not transfer")
	}
	events = f.drain(f.acquirer)
	if len(events) != 1 {
		t.Fatalf("acquisition callbacks=%v", events)
	}
	e := events[0].(*ownership.CallbackEvent)
	if e.Kind != ownership.CallbackAcquisitionNotification || !bytes.Equal(e.Tag, tag) {
		t.Fatalf("acquisition=%+v", e)
	}
	if len(f.drain(f.owner)) != 0 {
		t.Fatal("confirmation repeated after completed divest")
	}
}

func (f *ieeeOODDMFixture) region(t *testing.T, caller core.FederateHandle, dimension string) uint64 {
	t.Helper()
	space, ok := f.regions.LookupRoutingSpace(ieeeOODDMFederation, "default")
	if !ok {
		t.Fatal("routing space")
	}
	dim, ok := f.regions.LookupDimension(ieeeOODDMFederation, space, dimension)
	if !ok {
		t.Fatal("dimension")
	}
	r, err := f.ddmClient.CreateRegion(f.ctx, &rtiv1.CreateRegionRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(caller), RoutingSpaceHandle: uint64(space), DimensionHandles: []uint64{uint64(dim)}})
	ieeeOODDMOK(t, err)
	_, err = f.ddmClient.SetRangeBounds(f.ctx, &rtiv1.SetRangeBoundsRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(caller), RegionHandle: r.RegionHandle, DimensionHandle: uint64(dim), Bounds: &rtiv1.Range{Upper: 100}})
	ieeeOODDMOK(t, err)
	_, err = f.ddmClient.CommitRegionModifications(f.ctx, &rtiv1.CommitRegionRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(caller), RegionHandles: []uint64{r.RegionHandle}})
	ieeeOODDMOK(t, err)
	return r.RegionHandle
}

// IEEE 1516.1-2010 9.8.3 and 9.8.5, supplied PDF pp. 239-240.
func TestIEEE2010DDMRegionalContextRealWire(t *testing.T) {
	for _, mode := range []string{"class", "attribute", "missing-region", "foreign-region", "dimensions"} {
		t.Run(mode, func(t *testing.T) {
			f := newIEEEOODDMFixture(t)
			region := f.region(t, f.acquirer, "X")
			req := &rtiv1.SubscribeOCAWithRegionsRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.acquirer), ObjectClassHandle: uint64(f.cls), AttributeHandles: []uint64{uint64(f.value)}, RegionHandles: []uint64{region}}
			want := ""
			switch mode {
			case "class":
				req.ObjectClassHandle, want = 999999, "ObjectClassNotDefined"
			case "attribute":
				req.AttributeHandles, want = []uint64{uint64(f.value), 999999}, "AttributeNotDefined"
			case "missing-region":
				req.RegionHandles, want = []uint64{region, 999999}, "InvalidRegion"
			case "foreign-region":
				req.RegionHandles, want = []uint64{region, f.region(t, f.owner, "X")}, "RegionNotCreatedByThisFederate"
			case "dimensions":
				req.RegionHandles, want = []uint64{region, f.region(t, f.acquirer, "Y")}, "InvalidRegionContext"
			}
			before := f.regions.ExportState(ieeeOODDMFederation)
			var trailer metadata.MD
			_, err := f.ddmClient.SubscribeObjectClassAttributesWithRegions(f.ctx, req, grpcgo.Trailer(&trailer))
			ieeeOODDMException(t, err, trailer, want)
			if !reflect.DeepEqual(before, f.regions.ExportState(ieeeOODDMFederation)) {
				t.Fatal("invalid regional subscription partially changed state")
			}
		})
	}
}

// IEEE 1516.1-2010 9.8, supplied PDF pp. 238-240.
func TestIEEE2010DDMRegionalSubscriptionSetRealWire(t *testing.T) {
	f := newIEEEOODDMFixture(t)
	r1, r2 := f.region(t, f.acquirer, "X"), f.region(t, f.acquirer, "X")
	req := &rtiv1.SubscribeOCAWithRegionsRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.acquirer), ObjectClassHandle: uint64(f.cls), AttributeHandles: []uint64{uint64(f.value)}}
	for _, regions := range [][]uint64{{r1, r2}, {r1, r2}} {
		req.RegionHandles = regions
		_, err := f.ddmClient.SubscribeObjectClassAttributesWithRegions(f.ctx, req)
		ieeeOODDMOK(t, err)
	}
	before := f.regions.ExportState(ieeeOODDMFederation)
	var got []core.DDMRegionHandleCore
	for _, sub := range before.ObjectSubscriptions {
		if sub.ObjectClass == f.cls && sub.Attribute == f.value && sub.Subscriber == f.acquirer {
			got = append(got, sub.Regions...)
		}
	}
	if !reflect.DeepEqual(got, []core.DDMRegionHandleCore{core.DDMRegionHandleCore(r1), core.DDMRegionHandleCore(r2)}) {
		t.Fatalf("regional set=%v", got)
	}
	ieeeOODDMOK(t, f.decl.UnsubscribeObjectClassAttributes(f.ctx, ieeeOODDMFederation, f.acquirer, f.cls, nil))
	if !reflect.DeepEqual(before, f.regions.ExportState(ieeeOODDMFederation)) {
		t.Fatal("ordinary unsubscribe erased regional subscriptions")
	}
}

// IEEE 1516.1-2010 9.8(e), supplied PDF p. 239.
func TestIEEE2010DDMRegionalEmptySetPreservesActiveRealWire(t *testing.T) {
	f := newIEEEOODDMFixture(t)
	r := f.region(t, f.acquirer, "X")
	req := &rtiv1.SubscribeOCAWithRegionsRequest{WireVersion: 1, FederationName: ieeeOODDMFederation, FederateHandle: uint64(f.acquirer), ObjectClassHandle: uint64(f.cls), AttributeHandles: []uint64{uint64(f.value)}, RegionHandles: []uint64{r}}
	_, err := f.ddmClient.SubscribeObjectClassAttributesWithRegions(f.ctx, req)
	ieeeOODDMOK(t, err)
	before := f.regions.ExportState(ieeeOODDMFederation)
	req.RegionHandles = nil
	passive := false
	req.Active = &passive
	_, err = f.ddmClient.SubscribeObjectClassAttributesWithRegions(f.ctx, req)
	ieeeOODDMOK(t, err)
	if !reflect.DeepEqual(before, f.regions.ExportState(ieeeOODDMFederation)) {
		t.Fatal("empty region set changed active subscriptions")
	}
}
