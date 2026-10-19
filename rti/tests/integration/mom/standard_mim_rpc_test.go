package mom_test

import (
	"context"
	"net"
	"testing"
	"time"

	grpcsvc "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/tests/testkit/grpctest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type switchMembers struct{ core.FederationStore }

func (switchMembers) ValidateMember(fed core.FederationName, caller core.FederateHandle) error {
	if fed != "switches" || (caller != 7 && caller != 0x8122334481020304) {
		return core.ErrFederateNotJoined
	}
	return nil
}
func (switchMembers) GenerationFor(core.FederationName) (uint64, bool) { return 1, true }

// Exercise the production repository -> gRPC -> object registry -> MOM route,
// not a direct call to the dispatcher with a manually supplied class name.
func TestStandardMIMReportingOverObjectRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := composition.NewFOMRepository()
	fom, err := repo.Load(ctx, []core.FOMModule{{Path: "minimal.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name></objectClass></objects><interactions><interactionClass><name>HLAinteractionRoot</name></interactionClass></interactions></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("switches", fom)
	m, err := mom.New(mom.Options{Outbox: unusedOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	const target core.FederateHandle = 0x8122334481020304
	for _, h := range []core.FederateHandle{7, target} {
		if err := m.FederateJoined(ctx, "switches", h, "federate", "test"); err != nil {
			t.Fatal(err)
		}
	}
	decl := declaration.New()
	reg, err := object.New(object.Options{Declarations: decl, Outbox: unusedOutbox{}, FOMs: repo, Clock: core.NewManualClock(time.Unix(0, 0)), ManagementDispatch: mom.NewDispatcher(m)})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := grpcsvc.NewServer(grpcsvc.Options{Federations: switchMembers{}, Declarations: decl, Objects: reg, Outbox: unusedOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer(grpc.UnaryInterceptor(adapter.UnaryMembershipInterceptor()))
	if err := adapter.Register(srv); err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1024 * 1024)
	grpctest.Serve(t, srv, lis)
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := rtiv1.NewObjectServiceClient(conn)
	for _, tc := range []struct {
		name string
		get  func(*mom.Manager, core.FederationName, core.FederateHandle) bool
	}{
		{mom.ClassSetServiceReporting, (*mom.Manager).ServiceReporting},
		{mom.ClassSetExceptionReporting, (*mom.Manager).ExceptionReporting},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cls, ok := fom.LookupInteractionClass(tc.name)
			if !ok {
				t.Fatal("missing adjustment class")
			}
			state, ok := fom.LookupParameter(cls, "HLAreportingState")
			if !ok {
				t.Fatal("missing state parameter")
			}
			targetParam, ok := fom.LookupParameter(cls, "HLAfederate")
			if !ok || targetParam == state {
				t.Fatal("missing or aliased target")
			}
			request := &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: "switches", FederateHandle: 7, InteractionClassHandle: uint64(cls), Parameters: map[uint64][]byte{
				uint64(state): {0, 0, 0, 1}, uint64(targetParam): {0, 0, 0, 8, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4},
			}}
			if _, err := client.SendInteraction(ctx, request); err != nil {
				t.Fatal(err)
			}
			if !tc.get(m, "switches", target) || tc.get(m, "switches", 7) {
				t.Fatal("targeted RPC did not adjust only target")
			}
			request.Parameters[uint64(state)] = []byte{0, 0, 0, 0}
			request.Parameters[uint64(targetParam)] = []byte{0, 0, 0, 8}
			if _, err := client.SendInteraction(ctx, request); err == nil {
				t.Fatal("truncated target accepted")
			}
			if !tc.get(m, "switches", target) || tc.get(m, "switches", 7) {
				t.Fatal("malformed request changed switches")
			}
			// The LocalLRC wire path shares the classification and inherited lookup.
			request.Parameters[uint64(targetParam)] = []byte{0, 0, 0, 8, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4}
			if err := reg.SendInteractionWire(ctx, "switches", 7, cls, request.Parameters, nil); err != nil {
				t.Fatal(err)
			}
			if tc.get(m, "switches", target) || tc.get(m, "switches", 7) {
				t.Fatal("wire path did not clear only target")
			}
		})
	}
}
