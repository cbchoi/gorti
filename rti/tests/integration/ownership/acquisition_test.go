package ownership_test

import (
	"context"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	grpcsvc "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/tests/testkit/grpctest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

type outbound struct {
	to    core.FederateHandle
	event core.OutboundEvent
}
type recorder struct {
	mu     sync.Mutex
	events []outbound
	onSend func()
}

func (r *recorder) Send(_ context.Context, _ core.FederationName, to core.FederateHandle, event core.OutboundEvent) error {
	r.mu.Lock()
	r.events = append(r.events, outbound{to, event})
	hook := r.onSend
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}
func (r *recorder) snapshot() []outbound {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]outbound(nil), r.events...)
}

type repository struct{ handle core.FOMHandle }

func (r repository) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return r.handle, nil
}
func (r repository) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return r.handle, nil
}

type federations struct{ core.FederationStore }

func (federations) ValidateMember(fed core.FederationName, h core.FederateHandle) error {
	if fed != "fed" || h < 1 || h > 3 {
		return core.ErrFederateNotJoined
	}
	return nil
}
func (federations) GenerationFor(core.FederationName) (uint64, bool) { return 1, true }

type fixture struct {
	client rtiv1.OwnershipServiceClient
	own    *ownership.Manager
	decl   *declaration.Manager
	reg    *object.Registry
	out    *recorder
	obj    core.ObjectHandle
	cls    core.ObjectClassHandle
	x, y   core.AttributeHandle
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func setup(t *testing.T) fixture {
	return setupDeclarations(t, func(m *declaration.Manager) core.DeclarationManagement { return m })
}

func setupDeclarations(t *testing.T, wrap func(*declaration.Manager) core.DeclarationManagement, journals ...core.EventLog) fixture {
	t.Helper()
	ctx := context.Background()
	fom := composition.NewModelHandle(model.NewFOM([]model.ObjectClass{
		{Name: "Root", Attributes: []model.Attribute{{Name: "X"}}},
		{Name: "Vehicle", ParentName: "Root", Attributes: []model.Attribute{{Name: "Y"}}},
	}, nil, nil))
	cls, _ := fom.LookupObjectClass("Vehicle")
	x, _ := fom.LookupAttribute(cls, "X")
	y, _ := fom.LookupAttribute(cls, "Y")
	out := &recorder{}
	decl := declaration.New()
	var reg *object.Registry
	opts := ownership.Options{Outbox: out, PrepareAcquisition: func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
		return reg.PrepareOwnershipAcquisition(ctx, fed, caller, obj, attrs)
	}, PrepareReleaseDenied: func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
		return reg.PrepareOwnershipReleaseDenied(ctx, fed, caller, obj, attrs)
	}}
	if len(journals) > 0 {
		opts.EventLog = journals[0]
	}
	own, err := ownership.New(opts)
	must(t, err)
	reg, err = object.New(object.Options{
		Declarations: wrap(decl), Outbox: out, FOMs: repository{fom}, Clock: core.NewManualClock(time.Unix(0, 0)), Ownership: own,
		OnRegister: func(fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			own.RegisterInitialOwnership(fed, owner, obj, attrs)
		}, OnDelete: own.OnObjectDeleted,
	})
	must(t, err)
	for _, h := range []core.FederateHandle{1, 2, 3} {
		must(t, decl.PublishObjectClassAttributes(ctx, "fed", h, cls, []core.AttributeHandle{x, y}))
	}
	must(t, decl.SubscribeObjectClassAttributes(ctx, "fed", 2, cls, []core.AttributeHandle{x, y}))
	obj, _, err := reg.Register(ctx, "fed", 1, cls, "")
	must(t, err)
	must(t, own.UnconditionalDivest(ctx, "fed", 1, obj, []core.AttributeHandle{x}))
	adapter, err := grpcsvc.NewServer(grpcsvc.Options{Federations: federations{}, Declarations: decl, Objects: reg, Ownership: own, Outbox: out})
	must(t, err)
	server := grpc.NewServer(grpc.UnaryInterceptor(adapter.UnaryMembershipInterceptor()))
	must(t, adapter.Register(server))
	lis := bufconn.Listen(1024 * 1024)
	grpctest.Serve(t, server, lis)
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
	must(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return fixture{rtiv1.NewOwnershipServiceClient(conn), own, decl, reg, out, obj, cls, x, y}
}

type changingPublications struct {
	*declaration.Manager
	active bool
	x, y   core.AttributeHandle
}

func (d *changingPublications) PublishersFor(ctx context.Context, fed core.FederationName, cls core.ObjectClassHandle, attr core.AttributeHandle) []core.FederateHandle {
	result := d.Manager.PublishersFor(ctx, fed, cls, attr)
	if d.active && attr == d.x {
		_ = d.Manager.UnpublishObjectClassAttributes(ctx, fed, 2, cls, []core.AttributeHandle{d.x})
		_ = d.Manager.PublishObjectClassAttributes(ctx, fed, 2, cls, []core.AttributeHandle{d.y})
	}
	return result
}

func TestAcquisitionPublicationSetIsCoherent(t *testing.T) {
	var changing *changingPublications
	f := setupDeclarations(t, func(m *declaration.Manager) core.DeclarationManagement {
		changing = &changingPublications{Manager: m}
		return changing
	})
	ctx := context.Background()
	must(t, f.decl.UnpublishObjectClassAttributes(ctx, "fed", 2, f.cls, []core.AttributeHandle{f.y}))
	changing.x, changing.y, changing.active = f.x, f.y, true
	before := f.own.ExportState("fed")
	callbacks := f.out.snapshot()
	err := f.own.Acquire(ctx, "fed", 2, f.obj, []core.AttributeHandle{f.x, f.y}, nil)
	if err != core.ErrAttributeNotPublished {
		t.Fatalf("incoherent publication set accepted: %v", err)
	}
	if !reflect.DeepEqual(before, f.own.ExportState("fed")) || !reflect.DeepEqual(callbacks, f.out.snapshot()) {
		t.Fatal("publication rejection mutated ownership or emitted callbacks")
	}
}

func TestAcquisitionPreconditionsAreAtomic(t *testing.T) {
	for _, available := range []bool{false, true} {
		for _, scenario := range []string{unknownObjectName, "not-discovered", undefinedAttributeName, "class-not-published", "attribute-not-published", "already-owned"} {
			t.Run(scenario+map[bool]string{false: "/acquire", true: "/if-available"}[available], func(t *testing.T) {
				f := setup(t)
				ctx := context.Background()
				req := &rtiv1.AcquireRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2, ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.x)}, IfAvailable: available}
				want := ""
				switch scenario {
				case unknownObjectName:
					req.ObjectHandle = 999
					want = objectNotKnownErrorName
				case "not-discovered":
					req.FederateHandle = 3
					want = objectNotKnownErrorName
				case undefinedAttributeName:
					req.AttributeHandles = append(req.AttributeHandles, 999)
					want = attributeNotDefinedErrorName
				case "class-not-published":
					must(t, f.decl.UnpublishObjectClassAttributes(ctx, "fed", 2, f.cls, nil))
					want = "ObjectClassNotPublished"
				case "attribute-not-published":
					must(t, f.decl.UnpublishObjectClassAttributes(ctx, "fed", 2, f.cls, []core.AttributeHandle{f.y}))
					req.AttributeHandles = append(req.AttributeHandles, uint64(f.y))
					want = "AttributeNotPublished"
				case "already-owned":
					req.FederateHandle = 1
					req.AttributeHandles = append(req.AttributeHandles, uint64(f.y))
					want = "FederateOwnsAttributes"
				}
				before := f.own.ExportState("fed")
				callbacks := f.out.snapshot()
				var trailer metadata.MD
				_, err := f.client.AttributeOwnershipAcquisition(ctx, req, grpc.Trailer(&trailer))
				if err == nil || !reflect.DeepEqual(trailer.Get("rti-spec-exception"), []string{want}) {
					t.Errorf("error = %v, trailer = %v; want %s", err, trailer, want)
				}
				if after := f.own.ExportState("fed"); !reflect.DeepEqual(before, after) {
					t.Errorf("rejected acquisition changed ownership: before=%+v after=%+v", before, after)
				}
				if !reflect.DeepEqual(callbacks, f.out.snapshot()) {
					t.Error("rejected acquisition emitted callbacks")
				}
			})
		}
	}
}

func TestAcquisitionValidInheritedAttribute(t *testing.T) {
	for _, available := range []bool{false, true} {
		f := setup(t)
		before := len(f.out.snapshot())
		_, err := f.client.AttributeOwnershipAcquisition(context.Background(), &rtiv1.AcquireRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2, ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.x)}, IfAvailable: available})
		must(t, err)
		if !f.own.IsOwnedBy("fed", 2, f.obj, f.x) {
			t.Fatal("successful acquisition did not transfer ownership")
		}
		events := f.out.snapshot()[before:]
		if len(events) != 1 || events[0].to != 2 {
			t.Fatalf("acquisition callbacks: %+v", events)
		}
		event, ok := events[0].event.(*ownership.CallbackEvent)
		if !ok || event.Kind != ownership.CallbackAcquisitionNotification || event.Object != f.obj || event.Federate != 2 || !reflect.DeepEqual(event.Attributes, []core.AttributeHandle{f.x}) {
			t.Fatalf("acquisition metadata: %+v", event)
		}
	}
}

func TestDeletedObjectCannotBeRecreatedByAcquisition(t *testing.T) {
	f := setup(t)
	must(t, f.reg.Delete(context.Background(), "fed", 1, f.obj, nil, nil))
	before := f.own.ExportState("fed")
	callbacks := f.out.snapshot()
	for _, available := range []bool{false, true} {
		var trailer metadata.MD
		_, err := f.client.AttributeOwnershipAcquisition(context.Background(), &rtiv1.AcquireRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2, ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.x)}, IfAvailable: available}, grpc.Trailer(&trailer))
		if err == nil || !reflect.DeepEqual(trailer.Get("rti-spec-exception"), []string{objectNotKnownErrorName}) {
			t.Fatalf("deleted object: %v %v", err, trailer)
		}
		if !reflect.DeepEqual(before, f.own.ExportState("fed")) || !reflect.DeepEqual(callbacks, f.out.snapshot()) {
			t.Fatal("deleted object acquisition had side effects")
		}
	}
}

func TestAcquisitionReleasesObjectBeforeCallbacks(t *testing.T) {
	for _, negotiated := range []bool{false, true} {
		f := setup(t)
		if negotiated {
			must(t, f.own.NegotiatedDivest(context.Background(), "fed", 1, f.obj, []core.AttributeHandle{f.y}, nil))
		}
		f.out.mu.Lock()
		f.out.onSend = func() {
			done := make(chan struct{})
			go func() { f.reg.ClassOf("fed", f.obj); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("callback delivery retained object lock")
			}
		}
		f.out.mu.Unlock()
		attr := f.x
		if negotiated {
			attr = f.y
		}
		must(t, f.own.Acquire(context.Background(), "fed", 2, f.obj, []core.AttributeHandle{attr}, nil))
		if !f.own.IsOwnedBy("fed", 2, f.obj, attr) {
			t.Fatal("acquisition did not commit")
		}
	}
}

type journalHook struct {
	core.EventLog
	append func()
}

func (j *journalHook) Append(context.Context, core.FederationName, core.EventRecord) error {
	if j.append != nil {
		j.append()
	}
	return nil
}

func TestAcquisitionCommitsBeforeObserverAndDoesNotResurrectDeletedObject(t *testing.T) {
	journal := &journalHook{}
	f := setupDeclarations(t, func(m *declaration.Manager) core.DeclarationManagement { return m }, journal)
	ctx := context.Background()
	must(t, f.own.Acquire(ctx, "fed", 1, f.obj, []core.AttributeHandle{f.x}, nil))
	must(t, f.own.NegotiatedDivest(ctx, "fed", 1, f.obj, []core.AttributeHandle{f.x, f.y}, nil))
	observed := false
	journal.append = func() {
		journal.append = nil
		observed = true
		if !f.own.IsOwnedBy("fed", 2, f.obj, f.x) || !f.own.IsOwnedBy("fed", 2, f.obj, f.y) {
			t.Error("ready transfer was not committed before external observer")
		}
		state := f.own.Snapshot("fed")
		if state.PendingAcquiresCount != 0 || state.PendingDivestsCount != 0 {
			t.Errorf("pending state not committed: %+v", state)
		}
		must(t, f.reg.Delete(ctx, "fed", 1, f.obj, nil, nil))
	}
	must(t, f.own.Acquire(ctx, "fed", 2, f.obj, []core.AttributeHandle{f.x, f.y}, nil))
	if !observed {
		t.Fatal("observer was not called")
	}
	if f.reg.ClassOf("fed", f.obj) != 0 || f.own.Snapshot("fed") != (core.OwnershipSnapshot{}) {
		t.Fatal("acquisition resurrected deleted object ownership")
	}
}

const undefinedAttributeName = "undefined-attribute"

const unknownObjectName = "unknown-object"

const objectNotKnownErrorName = "ObjectInstanceNotKnown"

const attributeNotDefinedErrorName = "AttributeNotDefined"
