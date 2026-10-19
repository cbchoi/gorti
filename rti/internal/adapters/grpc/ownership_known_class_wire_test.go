package grpc_test

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

func TestOwnershipKnownClassRealRegistryWireRoundTrip(t *testing.T) {
	for _, atomic := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "atomic"}[atomic], func(t *testing.T) {
			ownershipKnownClassWireRoundTrip(t, atomic)
		})
	}
}

type ownershipAtomicWireObjects struct{ *object.Registry }

func (ownershipAtomicWireObjects) MapKnownAttributesToRegistered(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, error) {
	return nil, core.ErrObjectHandleInvalid
}

func ownershipKnownClassWireRoundTrip(t *testing.T, atomic bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const fed core.FederationName = "known-wire"
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	repo := composition.NewFOMRepository()
	modules := []core.FOMModule{{Path: "known.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Base</name><attribute><name>position</name></attribute><objectClass><name>Child</name><attribute><name>childOnly</name></attribute></objectClass></objectClass></objectClass></objects></objectModel>`)}}
	fom, err := repo.Load(ctx, modules)
	check(err)
	repo.RememberFor(fed, fom)
	clock := core.NewManualClock(time.Unix(0, 0))
	members, err := federation.New(federation.Options{Clock: clock, FOMs: repo})
	check(err)
	check(members.CreateFederation(ctx, core.CreateFederationRequest{Name: fed, FOMModules: modules}))
	join := func(name string) core.FederateHandle {
		h, err := members.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: name})
		check(err)
		return h
	}
	owner, candidate, peer := join("owner"), join("candidate"), join("peer")
	base, _ := fom.LookupObjectClass("Base")
	child, _ := fom.LookupObjectClass("Child")
	known, _ := fom.LookupAttribute(base, "position")
	canonical, _ := fom.LookupAttribute(child, "position")
	childOnly, _ := fom.LookupAttribute(child, "childOnly")
	if known == canonical || known != childOnly {
		t.Fatalf("fixture must have colliding class-relative handles: Base.position=%d Child.position=%d Child.childOnly=%d", known, canonical, childOnly)
	}
	out := delivery.NewWithBatch(128, 1, 0)
	for _, h := range []core.FederateHandle{owner, candidate, peer} {
		_, closeQueue, err := out.Subscribe(ctx, fed, h)
		check(err)
		t.Cleanup(func() { _ = closeQueue() })
	}
	decl := declaration.New()
	decl.SetFOMRepository(repo)
	var registry *object.Registry
	ownerOptions := ownership.Options{Outbox: out,
		PrepareAcquisition: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			return registry.PrepareOwnershipAcquisition(ctx, fed, h, obj, attrs)
		},
		PrepareDivestiture: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			return registry.PrepareOwnershipReleaseDenied(ctx, fed, h, obj, attrs)
		},
		PrepareReleaseDenied: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) (func(), error) {
			return registry.PrepareOwnershipReleaseDenied(ctx, fed, h, obj, attrs)
		},
	}
	if atomic {
		ownerOptions.PrepareKnownAcquisition = func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
			return registry.PrepareKnownOwnershipAcquisition(ctx, fed, h, obj, attrs)
		}
		ownerOptions.PrepareKnownOperation = func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
			return registry.PrepareKnownOwnershipOperation(ctx, fed, h, obj, attrs)
		}
	}
	owners, err := ownership.New(ownerOptions)
	check(err)
	if owners.KnownOwnershipAvailable() != atomic {
		t.Fatal("known ownership availability does not match configured ports")
	}
	registry, err = object.New(object.Options{Clock: clock, FOMs: repo, Declarations: decl, Outbox: out, Ownership: owners,
		OnRegister: func(fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			owners.RegisterInitialOwnership(fed, h, obj, attrs)
		},
	})
	check(err)
	decl.SetOnSubscribeObjectClass(registry.ObjectClassSubscribed)
	check(decl.PublishObjectClassAttributes(ctx, fed, owner, child, []core.AttributeHandle{canonical, childOnly}))
	for _, h := range []core.FederateHandle{candidate, peer} {
		check(decl.SubscribeObjectClassAttributes(ctx, fed, h, base, []core.AttributeHandle{known}))
		check(decl.PublishObjectClassAttributes(ctx, fed, h, base, []core.AttributeHandle{known}))
	}
	obj, _, err := registry.Register(ctx, fed, owner, child, "")
	check(err)
	check(decl.SubscribeObjectClassAttributes(ctx, fed, candidate, child, []core.AttributeHandle{canonical, childOnly}))
	check(decl.UnsubscribeObjectClassAttributes(ctx, fed, candidate, base, nil))
	if _, cls, found := registry.LookupKnownObjectInstance(fed, candidate, obj); !found || cls != base {
		t.Fatal("subscription changes must not replace the remembered Base class")
	}
	var handlerObjects core.ObjectRegistry = registry
	if atomic {
		handlerObjects = ownershipAtomicWireObjects{registry}
	}
	service, err := grpcsvc.NewServer(grpcsvc.Options{Federations: members, Membership: members, Declarations: decl, Objects: handlerObjects, Ownership: owners, Outbox: out, FOMs: repo})
	check(err)
	server := grpcgo.NewServer(grpcgo.UnaryInterceptor(service.UnaryMembershipInterceptor()))
	check(service.Register(server))
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpcgo.NewClient("passthrough:///known-wire", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	check(err)
	t.Cleanup(func() { _ = conn.Close() })
	client := rtiv1.NewOwnershipServiceClient(conn)
	acquire := func(h core.FederateHandle, available bool, attrs ...core.AttributeHandle) error {
		wire := make([]uint64, len(attrs))
		for i, attr := range attrs {
			wire[i] = uint64(attr)
		}
		_, err := client.AttributeOwnershipAcquisition(ctx, &rtiv1.AcquireRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(h), ObjectHandle: uint64(obj), AttributeHandles: wire, IfAvailable: available})
		return err
	}
	check(owners.UnconditionalDivest(ctx, fed, owner, obj, []core.AttributeHandle{canonical}))
	before := owners.ExportState(fed)
	var trailer metadata.MD
	_, err = client.AttributeOwnershipAcquisition(ctx, &rtiv1.AcquireRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(candidate), ObjectHandle: uint64(obj), AttributeHandles: []uint64{uint64(known), 999}, IfAvailable: true}, grpcgo.Trailer(&trailer))
	if err == nil || !reflect.DeepEqual(trailer.Get("rti-spec-exception"), []string{"AttributeNotDefined"}) || !reflect.DeepEqual(before, owners.ExportState(fed)) {
		t.Fatalf("invalid mixed known-class set was not atomic/typed: %v %v", err, trailer)
	}
	check(acquire(candidate, true, known))
	if !owners.IsOwnedBy(fed, candidate, obj, canonical) || !owners.IsOwnedBy(fed, owner, obj, childOnly) {
		t.Fatal("acquisition wrote caller coordinates into canonical ownership")
	}
	query, err := client.QueryAttributeOwnership(ctx, &rtiv1.QueryOwnershipRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(candidate), ObjectHandle: uint64(obj), AttributeHandle: uint64(known)})
	check(err)
	if !query.Owned || query.OwnerFederateHandle != uint64(candidate) {
		t.Fatalf("existing query mapping was changed: %v", query)
	}
	check(acquire(peer, false, known))
	_, err = client.AttributeOwnershipReleaseDenied(ctx, &rtiv1.ReleaseDeniedRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(candidate), ObjectHandle: uint64(obj), AttributeHandles: []uint64{uint64(known)}})
	check(err)
	if len(owners.ExportState(fed).PendingAcquires) != 0 {
		t.Fatal("release denied did not clear canonical acquisition")
	}
	check(acquire(peer, false, known))
	result, err := client.AttributeOwnershipDivestitureIfWantedWithResult(ctx, &rtiv1.DivestIfWantedRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(candidate), ObjectHandle: uint64(obj), AttributeHandles: []uint64{uint64(known)}})
	check(err)
	if !reflect.DeepEqual(result.DivestedAttributeHandles, []uint64{uint64(known)}) {
		t.Fatalf("result leaked canonical attribute: %v", result)
	}
	if !owners.IsOwnedBy(fed, peer, obj, canonical) || !owners.IsOwnedBy(fed, owner, obj, childOnly) {
		t.Fatal("divest result transferred the wrong attribute")
	}
}
