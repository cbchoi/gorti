package grpc

import (
	"context"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

type declarationWireOutbox struct {
	mu    sync.Mutex
	kinds []declaration.AdvisoryKind
}

func (o *declarationWireOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, event core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.kinds = append(o.kinds, event.(*declaration.AdvisoryEvent).Kind)
	return nil
}
func (o *declarationWireOutbox) count() int { o.mu.Lock(); defer o.mu.Unlock(); return len(o.kinds) }

func TestDeclarationOptionsRealWire(t *testing.T) {
	m := declaration.New()
	out := &declarationWireOutbox{}
	m.SetAdvisoryOutbox(out)
	listener := bufconn.Listen(1 << 20)
	server := grpcgo.NewServer()
	rtiv1.RegisterDeclarationServiceServer(server, newDeclarationService(m))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpcgo.NewClient("passthrough:///declaration-options", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := rtiv1.NewDeclarationServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.PublishObjectClassAttributes(ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, ObjectClassHandle: 10, AttributeHandles: []uint64{1}})
	if err != nil {
		t.Fatal(err)
	}
	passive := false
	req := &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: "f", FederateHandle: 2, ObjectClassHandle: 10, AttributeHandles: []uint64{1}, Active: &passive}
	if _, err = client.SubscribeObjectClassAttributes(ctx, req); err != nil {
		t.Fatal(err)
	}
	options, ok := m.ObjectSubscriptionOptions("f", 2, 10, 1)
	if !ok || options.Active || out.count() != 0 {
		t.Fatalf("explicit false lost: %+v callbacks=%d", options, out.count())
	}
	if !reflect.DeepEqual(m.SubscribersFor(ctx, "f", 10, []core.AttributeHandle{1}), []core.FederateHandle{2}) {
		t.Fatal("passive no longer data eligible")
	}
	before := m.ExportState("f")
	req.UpdateRateDesignator = "unknown"
	req.AttributeHandles = []uint64{1, 2}
	var trailer metadata.MD
	_, err = client.SubscribeObjectClassAttributes(ctx, req, grpcgo.Trailer(&trailer))
	if status.Code(err) != codes.InvalidArgument || !reflect.DeepEqual(trailer.Get(specExceptionTrailerKey), []string{"InvalidUpdateRateDesignator"}) {
		t.Fatalf("invalid rate status=%v trailer=%v", err, trailer)
	}
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("invalid options partially mutated state")
	}
	req.UpdateRateDesignator = ""
	req.AttributeHandles = []uint64{1}
	req.Active = nil
	if _, err = client.SubscribeObjectClassAttributes(ctx, req); err != nil {
		t.Fatal(err)
	}
	options, _ = m.ObjectSubscriptionOptions("f", 2, 10, 1)
	if !options.Active || out.count() != 1 {
		t.Fatal("old omitted active did not retain legacy active default")
	}
	switchReq := &rtiv1.SetAdvisorySwitchRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, SwitchKind: rtiv1.AdvisorySwitchKind_ADVISORY_SWITCH_KIND_OBJECT_CLASS_RELEVANCE, Enabled: false}
	if _, err = client.SetAdvisorySwitch(ctx, switchReq); err != nil {
		t.Fatal(err)
	}
	trailer = nil
	_, err = client.SetAdvisorySwitch(ctx, switchReq, grpcgo.Trailer(&trailer))
	if status.Code(err) != codes.FailedPrecondition || !reflect.DeepEqual(trailer.Get(specExceptionTrailerKey), []string{"ObjectClassRelevanceAdvisorySwitchIsOff"}) {
		t.Fatalf("duplicate toggle=%v trailer=%v", err, trailer)
	}
	req.Active = &passive
	if _, err = client.SubscribeObjectClassAttributes(ctx, req); err != nil {
		t.Fatal(err)
	}
	if out.count() != 1 {
		t.Fatal("disabled switch did not suppress edge")
	}
	switchReq.Enabled = true
	if _, err = client.SetAdvisorySwitch(ctx, switchReq); err != nil {
		t.Fatal(err)
	}
	req.Active = nil
	if _, err = client.SubscribeObjectClassAttributes(ctx, req); err != nil {
		t.Fatal(err)
	}
	if out.count() != 2 {
		t.Fatal("re-enabled switch did not emit subsequent edge")
	}
	_, err = client.SubscribeInteractionClass(ctx, &rtiv1.SubInterRequest{WireVersion: 1, FederationName: "f", FederateHandle: 2, InteractionClassHandle: 20, Active: &passive})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.PublishInteractionClass(ctx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, InteractionClassHandle: 20})
	if err != nil {
		t.Fatal(err)
	}
	if out.count() != 2 {
		t.Fatal("passive interaction contributed active interest")
	}
	_, err = client.SubscribeInteractionClass(ctx, &rtiv1.SubInterRequest{WireVersion: 1, FederationName: "f", FederateHandle: 2, InteractionClassHandle: 20})
	if err != nil {
		t.Fatal(err)
	}
	if out.count() != 3 {
		t.Fatal("omitted interaction active did not contribute interest")
	}
}
