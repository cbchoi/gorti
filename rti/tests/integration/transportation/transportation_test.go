package transportation_test

import (
	"context"
	"errors"
	"net"
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

type outbox struct{}

func (outbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return nil
}

type repository struct{ handle core.FOMHandle }

func (r repository) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return r.handle, nil
}
func (r repository) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return r.handle, nil
}

type fixture struct {
	registry *object.Registry
	decl     *declaration.Manager
	own      *ownership.Manager
	fom      core.FOMHandle
	obj      core.ObjectHandle
	cls      core.ObjectClassHandle
	x, y     core.AttributeHandle
	inter    core.InteractionClassHandle
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func setup(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	fom := composition.NewModelHandle(model.NewFOM([]model.ObjectClass{
		{Name: "Root", Attributes: []model.Attribute{{Name: "Inherited", Transportation: "HLAbestEffort"}}},
		{Name: "Vehicle", ParentName: "Root", Attributes: []model.Attribute{
			{Name: "X", Transportation: "HLAreliable"}, {Name: "Y", Transportation: "HLAbestEffort"},
		}},
	}, []model.InteractionClass{{Name: "Signal", Transportation: "HLAbestEffort"}}, nil))
	cls, _ := fom.LookupObjectClass("Vehicle")
	x, _ := fom.LookupAttribute(cls, "X")
	y, _ := fom.LookupAttribute(cls, "Y")
	inter, _ := fom.LookupInteractionClass("Signal")
	decl := declaration.New()
	own, err := ownership.New(ownership.Options{Outbox: outbox{}})
	must(t, err)
	reg, err := object.New(object.Options{
		Declarations: decl, Outbox: outbox{}, FOMs: repository{fom},
		Clock: core.NewManualClock(time.Unix(0, 0)), Ownership: own,
		OnRegister: func(fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			own.RegisterInitialOwnership(fed, owner, obj, attrs)
		},
	})
	must(t, err)
	must(t, decl.PublishObjectClassAttributes(ctx, "fed", 1, cls, []core.AttributeHandle{x, y}))
	must(t, decl.SubscribeObjectClassAttributes(ctx, "fed", 2, cls, []core.AttributeHandle{x, y}))
	obj, _, err := reg.Register(ctx, "fed", 1, cls, "")
	must(t, err)
	return fixture{reg, decl, own, fom, obj, cls, x, y, inter}
}

func TestAttributeTransportationDefaultsAndKnowledge(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	inherited, ok := f.fom.LookupAttribute(f.cls, "Inherited")
	if !ok {
		t.Fatal("missing inherited attribute")
	}
	for attr, want := range map[core.AttributeHandle]core.TransportType{
		f.x: core.TransportTypeReliable, f.y: core.TransportTypeBestEffort, inherited: core.TransportTypeBestEffort,
	} {
		got, err := f.registry.QueryAttributeTransportationType(ctx, "fed", 2, f.obj, attr)
		must(t, err)
		if got != want {
			t.Errorf("attribute %d: got %v, want FOM default %v", attr, got, want)
		}
	}
	_, err := f.registry.QueryAttributeTransportationType(ctx, "fed", 3, f.obj, f.x)
	if !errors.Is(err, core.ErrObjectHandleInvalid) {
		t.Fatalf("undiscovered object: %v", err)
	}
	_, err = f.registry.QueryAttributeTransportationType(ctx, "fed", 1, f.obj, 999)
	if !errors.Is(err, core.ErrAttributeNotFound) {
		t.Fatalf("undefined attribute: %v", err)
	}
}

func TestAttributeTransportationTransferAndAtomicFailure(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	must(t, f.registry.RequestAttributeTransportationTypeChange(ctx, "fed", 1, f.obj, []core.AttributeHandle{f.x}, core.TransportTypeBestEffort))
	must(t, f.own.UnconditionalDivest(ctx, "fed", 1, f.obj, []core.AttributeHandle{f.x}))
	must(t, f.own.Acquire(ctx, "fed", 2, f.obj, []core.AttributeHandle{f.x}, nil))
	must(t, f.registry.RequestAttributeTransportationTypeChange(ctx, "fed", 2, f.obj, []core.AttributeHandle{f.x}, core.TransportTypeReliable))
	for _, tc := range []struct {
		caller core.FederateHandle
		attrs  []core.AttributeHandle
		want   error
	}{
		{1, []core.AttributeHandle{f.x}, core.ErrAttributeNotOwned},
		{2, []core.AttributeHandle{f.x, f.y}, core.ErrAttributeNotOwned},
		{2, []core.AttributeHandle{f.x, 999}, core.ErrAttributeNotFound},
	} {
		err := f.registry.RequestAttributeTransportationTypeChange(ctx, "fed", tc.caller, f.obj, tc.attrs, core.TransportTypeBestEffort)
		if !errors.Is(err, tc.want) {
			t.Fatalf("caller %d attrs %v: got %v, want %v", tc.caller, tc.attrs, err, tc.want)
		}
		got, err := f.registry.QueryAttributeTransportationType(ctx, "fed", 1, f.obj, f.x)
		must(t, err)
		if got != core.TransportTypeReliable {
			t.Fatalf("failed request partially mutated X: %v", got)
		}
	}
	for _, invalid := range []core.TransportType{0, 3, 255} {
		err := f.registry.RequestAttributeTransportationTypeChange(ctx, "fed", 2, f.obj, []core.AttributeHandle{f.x}, invalid)
		if !errors.Is(err, core.ErrTransportTypeUnspecified) {
			t.Fatalf("invalid type %d: %v", invalid, err)
		}
	}
}

func TestInteractionTransportationPublicationAndOtherPublisher(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	err := f.registry.RequestInteractionTransportationTypeChange(ctx, "fed", 1, f.inter, core.TransportTypeReliable)
	if !errors.Is(err, core.ErrInteractionClassNotPublished) {
		t.Fatalf("unpublished interaction: %v", err)
	}
	for _, publisher := range []core.FederateHandle{1, 2} {
		must(t, f.decl.PublishInteractionClass(ctx, "fed", publisher, f.inter))
	}
	must(t, f.registry.RequestInteractionTransportationTypeChange(ctx, "fed", 2, f.inter, core.TransportTypeReliable))
	for target, want := range map[core.FederateHandle]core.TransportType{1: core.TransportTypeBestEffort, 2: core.TransportTypeReliable} {
		got, err := f.registry.QueryInteractionTransportationType(ctx, "fed", 1, target, f.inter)
		must(t, err)
		if got != want {
			t.Fatalf("target %d got %v, want %v", target, got, want)
		}
	}
	err = f.registry.RequestInteractionTransportationTypeChange(ctx, "fed", 1, 999, core.TransportTypeReliable)
	if !errors.Is(err, core.ErrInteractionClassNotFound) {
		t.Fatalf("undefined interaction: %v", err)
	}
	_, err = f.registry.QueryInteractionTransportationType(ctx, "fed", 1, 2, 999)
	if !errors.Is(err, core.ErrInteractionClassNotFound) {
		t.Fatalf("undefined query: %v", err)
	}
}

type federations struct{ core.FederationStore }

func (federations) ValidateMember(fed core.FederationName, h core.FederateHandle) error {
	if fed != "fed" || (h != 1 && h != 2) {
		return core.ErrFederateNotJoined
	}
	return nil
}
func (federations) GenerationFor(core.FederationName) (uint64, bool) { return 1, true }

func TestTransportationRPCs(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	must(t, f.decl.PublishInteractionClass(ctx, "fed", 2, f.inter))
	adapter, err := grpcsvc.NewServer(grpcsvc.Options{
		Federations: federations{}, Declarations: f.decl, Objects: f.registry, Outbox: outbox{},
	})
	must(t, err)
	server := grpc.NewServer(grpc.UnaryInterceptor(adapter.UnaryMembershipInterceptor()))
	must(t, adapter.Register(server))
	lis := bufconn.Listen(1024 * 1024)
	grpctest.Serve(t, server, lis)
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
	must(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := rtiv1.NewObjectServiceClient(conn)
	_, err = client.RequestAttributeTransportationTypeChange(ctx, &rtiv1.ChangeAttributeTransportRequest{
		WireVersion: 1, FederationName: "fed", FederateHandle: 1, ObjectHandle: uint64(f.obj),
		AttributeHandles: []uint64{uint64(f.x)}, TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT,
	})
	must(t, err)
	attr, err := client.QueryAttributeTransportationType(ctx, &rtiv1.QueryAttributeTransportationTypeRequest{
		WireVersion: 1, FederationName: "fed", FederateHandle: 2, ObjectHandle: uint64(f.obj), AttributeHandle: uint64(f.x),
	})
	must(t, err)
	if attr.TransportType != rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT {
		t.Fatalf("attribute RPC result: %v", attr)
	}
	_, err = client.RequestInteractionTransportationTypeChange(ctx, &rtiv1.ChangeInteractionTransportRequest{
		WireVersion: 1, FederationName: "fed", FederateHandle: 2, InteractionClassHandle: uint64(f.inter),
		TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE,
	})
	must(t, err)
	inter, err := client.QueryInteractionTransportationType(ctx, &rtiv1.QueryInteractionTransportationTypeRequest{
		WireVersion: 1, FederationName: "fed", FederateHandle: 1, TargetFederateHandle: 2, InteractionClassHandle: uint64(f.inter),
	})
	must(t, err)
	if inter.TransportType != rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE {
		t.Fatalf("interaction RPC target result: %v", inter)
	}
	for _, tc := range []struct {
		caller uint64
		typeID rtiv1.TransportationType
		want   string
	}{
		{1, 99, "InvalidTransportationType"}, {2, 1, "AttributeNotOwned"}, {99, 1, "FederateNotExecutionMember"},
	} {
		var trailer metadata.MD
		_, err = client.RequestAttributeTransportationTypeChange(ctx, &rtiv1.ChangeAttributeTransportRequest{
			WireVersion: 1, FederationName: "fed", FederateHandle: tc.caller, ObjectHandle: uint64(f.obj),
			AttributeHandles: []uint64{uint64(f.x)}, TransportType: tc.typeID,
		}, grpc.Trailer(&trailer))
		if err == nil || len(trailer.Get("rti-spec-exception")) != 1 || trailer.Get("rti-spec-exception")[0] != tc.want {
			t.Fatalf("want %s, got err=%v trailer=%v", tc.want, err, trailer)
		}
	}
}
