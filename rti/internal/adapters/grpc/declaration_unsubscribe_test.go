package grpc

import (
	"context"
	"net"
	"reflect"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestUnsubscriptionAttributeSetPresenceRealWire(t *testing.T) {
	for _, tc := range []struct {
		name     string
		presence *bool
		attrs    []uint64
		remain   []core.AttributeHandle
		code     codes.Code
	}{
		{"legacy-whole", nil, nil, nil, codes.OK},
		{"legacy-scoped", nil, []uint64{2}, []core.AttributeHandle{3}, codes.OK},
		{"explicit-empty", boolRef(true), nil, []core.AttributeHandle{2, 3}, codes.OK},
		{"explicit-scoped", boolRef(true), []uint64{2}, []core.AttributeHandle{3}, codes.OK},
		{"explicit-omitted", boolRef(false), nil, nil, codes.OK},
		{"contradictory", boolRef(false), []uint64{2}, []core.AttributeHandle{2, 3}, codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, m := declarationFixture()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := m.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{2, 3}); err != nil {
				t.Fatal(err)
			}
			l := bufconn.Listen(1 << 20)
			server := grpc.NewServer()
			rtiv1.RegisterDeclarationServiceServer(server, svc)
			go func() { _ = server.Serve(l) }()
			t.Cleanup(func() { server.Stop(); _ = l.Close() })
			conn, err := grpc.NewClient("passthrough:///unsubscription", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return l.Dial() }))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			request := &rtiv1.UnsubObjAttrsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2, ObjectClassHandle: 7, AttributeHandles: tc.attrs}
			if tc.presence != nil {
				field := request.ProtoReflect().Descriptor().Fields().ByName("attribute_set_present")
				if field == nil {
					t.Fatal("missing presence field")
				}
				request.ProtoReflect().Set(field, protoreflect.ValueOfBool(*tc.presence))
			}
			_, err = rtiv1.NewDeclarationServiceClient(conn).UnsubscribeObjectClassAttributes(ctx, request)
			if status.Code(err) != tc.code {
				t.Fatalf("error=%v want=%v", err, tc.code)
			}
			for _, attr := range []core.AttributeHandle{2, 3} {
				want := false
				for _, remaining := range tc.remain {
					want = want || attr == remaining
				}
				if got := m.SubscribersFor(ctx, "fed", 7, []core.AttributeHandle{attr}); (len(got) == 1) != want {
					t.Fatalf("attribute %d subscribers=%v want retained=%t", attr, got, want)
				}
			}
		})
	}
}

func boolRef(value bool) *bool { return &value }

func TestUnsubscriptionExplicitEmptyCannotFallBackToDestructiveLegacyPort(t *testing.T) {
	_, m := declarationFixture()
	ctx := context.Background()
	if err := m.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{2}); err != nil {
		t.Fatal(err)
	}
	legacy := struct{ core.DeclarationManagement }{m}
	svc := newDeclarationService(legacy)
	present := true
	_, err := svc.UnsubscribeObjectClassAttributes(ctx, &rtiv1.UnsubObjAttrsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2, ObjectClassHandle: 7, AttributeSetPresent: &present})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("unsafe fallback error=%v", err)
	}
	if got := m.SubscribersFor(ctx, "fed", 7, []core.AttributeHandle{2}); !reflect.DeepEqual(got, []core.FederateHandle{2}) {
		t.Fatalf("fallback destroyed subscriptions: %v", got)
	}
}

type unsubscribeWireFOM struct {
	core.FOMHandle
	core.FOMHandleNameLookup
}

func (unsubscribeWireFOM) ObjectClassName(class core.ObjectClassHandle) (string, bool) {
	return "Item", class == 7
}
func (unsubscribeWireFOM) AttributeName(class core.ObjectClassHandle, attr core.AttributeHandle) (string, bool) {
	return "x", class == 7 && attr == 2
}

type unsubscribeWireRepository struct{}

func (unsubscribeWireRepository) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return unsubscribeWireFOM{}, nil
}
func (unsubscribeWireRepository) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return unsubscribeWireFOM{}, nil
}

func TestUnsubscriptionWrongFOMSetNoEffectsAndIEEEErrorTrailer(t *testing.T) {
	class, attr := core.ObjectClassHandle(7), core.AttributeHandle(2)
	svc, m := declarationFixture()
	m.SetFOMRepository(unsubscribeWireRepository{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.SubscribeObjectClassAttributes(ctx, "fed", 2, class, []core.AttributeHandle{attr}); err != nil {
		t.Fatal(err)
	}
	l := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	rtiv1.RegisterDeclarationServiceServer(server, svc)
	go func() { _ = server.Serve(l) }()
	t.Cleanup(func() { server.Stop(); _ = l.Close() })
	conn, err := grpc.NewClient("passthrough:///unsubscription-errors", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return l.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	for _, tc := range []struct {
		name      string
		cls       uint64
		attrs     []uint64
		exception string
	}{
		{"mixed-invalid", uint64(class), []uint64{uint64(attr), 999999}, "AttributeNotDefined"},
		{"empty-invalid-class", 999999, nil, "ObjectClassNotDefined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := m.ExportState("fed")
			request := &rtiv1.UnsubObjAttrsRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2, ObjectClassHandle: tc.cls, AttributeHandles: tc.attrs}
			field := request.ProtoReflect().Descriptor().Fields().ByName("attribute_set_present")
			if field == nil {
				t.Fatal("missing presence field")
			}
			request.ProtoReflect().Set(field, protoreflect.ValueOfBool(true))
			var trailer metadata.MD
			_, err := rtiv1.NewDeclarationServiceClient(conn).UnsubscribeObjectClassAttributes(ctx, request, grpc.Trailer(&trailer))
			if err == nil {
				t.Fatal("invalid FOM request accepted")
			}
			if got := trailer.Get(specExceptionTrailerKey); len(got) != 1 || got[0] != tc.exception {
				t.Fatalf("IEEE error trailer=%v want=%s, error=%v", got, tc.exception, err)
			}
			if !reflect.DeepEqual(before, m.ExportState("fed")) {
				t.Fatal("invalid request changed declaration state")
			}
		})
	}
}
