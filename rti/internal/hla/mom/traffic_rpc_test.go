package mom_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type catalogMembers struct{ core.FederationStore }

func (catalogMembers) ValidateMember(fed core.FederationName, h core.FederateHandle) error {
	if fed != catalogFederation || (h != 7 && h != catalogTarget) {
		return core.ErrFederateNotJoined
	}
	return nil
}
func (catalogMembers) GenerationFor(core.FederationName) (uint64, bool) { return 1, true }

// Real accepted traffic traverses the registry hooks and accumulator; requests
// and reports traverse ObjectService and StreamService over gRPC. This is not
// a substitute for the production executable's composition/lifecycle test.
func TestMOMTrafficReportsOverObjectAndStreamRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := composition.NewFOMRepository()
	fom, err := repo.Load(ctx, []core.FOMModule{{XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>VehicleA</name><attribute><name>value</name><dataType>HLAinteger32BE</dataType></attribute></objectClass><objectClass><name>VehicleB</name><attribute><name>value</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objects><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>PulseA</name></interactionClass><interactionClass><name>PulseB</name></interactionClass></interactionClass></interactions></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor(catalogFederation, fom)
	out := delivery.NewWithBatch(64, 1, 0)
	for _, h := range []core.FederateHandle{7, catalogTarget} {
		out.Bind(catalogFederation, h)
	}
	t.Cleanup(func() { out.UnbindFederation(catalogFederation) })
	m, err := mom.New(mom.Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	traffic := mom.NewTrafficAccumulator()
	if err := m.SetTrafficAccumulator(traffic); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{7, catalogTarget} {
		if err := m.FederateJoined(ctx, catalogFederation, h, "participant", "test"); err != nil {
			t.Fatal(err)
		}
	}
	d := mom.NewDispatcher(m)
	d.SetCounterSource(traffic.Snapshot)
	d.SetEmitter(mom.NewProductionEmitter(out))
	decl := declaration.New()
	reg, err := object.New(object.Options{Declarations: decl, Outbox: out, FOMs: repo, Clock: core.NewManualClock(time.Unix(0, 0)), ManagementDispatch: d,
		OnObjectTraffic: func(f core.FederationName, h core.FederateHandle, class core.ObjectClassHandle, incoming bool) {
			kind := mom.UpdatesSent
			if incoming {
				kind = mom.ReflectionsReceived
			}
			if err := traffic.Record(f, h, kind, uint64(class), core.TransportTypeReliable); err != nil {
				t.Error(err)
			}
		},
		OnInteractionTraffic: func(f core.FederationName, h core.FederateHandle, class core.InteractionClassHandle, incoming bool) {
			kind := mom.InteractionsSent
			if incoming {
				kind = mom.InteractionsReceived
			}
			if err := traffic.Record(f, h, kind, uint64(class), core.TransportTypeReliable); err != nil {
				t.Error(err)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantObjects, wantInteractions := map[uint64]uint32{}, map[uint64]uint32{}
	for index, name := range []string{"VehicleA", "VehicleB"} {
		class, ok := fom.LookupObjectClass(name)
		if !ok {
			t.Fatal(name)
		}
		attr, ok := fom.LookupAttribute(class, "value")
		if !ok {
			t.Fatal("value")
		}
		if err := decl.PublishObjectClassAttributes(ctx, catalogFederation, catalogTarget, class, []core.AttributeHandle{attr}); err != nil {
			t.Fatal(err)
		}
		if err := decl.SubscribeObjectClassAttributes(ctx, catalogFederation, 7, class, []core.AttributeHandle{attr}); err != nil {
			t.Fatal(err)
		}
		obj, _, err := reg.Register(ctx, catalogFederation, catalogTarget, class, "")
		if err != nil {
			t.Fatal(err)
		}
		for n := 0; n <= index; n++ {
			if err := reg.UpdateAttributes(ctx, catalogFederation, catalogTarget, obj, map[core.AttributeHandle][]byte{attr: {0, 0, 0, 1}}, nil); err != nil {
				t.Fatal(err)
			}
		}
		wantObjects[uint64(class)] = uint32(index + 1)
	}
	for index, name := range []string{"PulseA", "PulseB"} {
		class, ok := fom.LookupInteractionClass(name)
		if !ok {
			t.Fatal(name)
		}
		if err := decl.PublishInteractionClass(ctx, catalogFederation, catalogTarget, class); err != nil {
			t.Fatal(err)
		}
		if err := decl.SubscribeInteractionClass(ctx, catalogFederation, 7, class); err != nil {
			t.Fatal(err)
		}
		for n := 0; n <= index; n++ {
			if err := reg.SendInteraction(ctx, catalogFederation, catalogTarget, class, nil, nil); err != nil {
				t.Fatal(err)
			}
		}
		wantInteractions[uint64(class)] = uint32(index + 1)
		if err := reg.SendInteraction(ctx, catalogFederation, 7, class, nil, nil); err == nil {
			t.Fatal("unpublished sender accepted")
		}
	}
	adapter, err := grpcsvc.NewServer(grpcsvc.Options{Federations: catalogMembers{}, Declarations: decl, Objects: reg, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer(grpc.UnaryInterceptor(adapter.UnaryMembershipInterceptor()))
	if err := adapter.Register(srv); err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1024 * 1024)
	serveResult := make(chan error, 1)
	go func() { serveResult <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := <-serveResult; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("serve: %v", err)
		}
	})
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	stream, err := rtiv1.NewStreamServiceClient(conn).Events(ctx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: string(catalogFederation), FederateHandle: 7})
	if err != nil {
		t.Fatal(err)
	}
	client := rtiv1.NewObjectServiceClient(conn)
	for index, request := range counterRequests {
		target := catalogTarget
		if index == 1 || index == 3 {
			target = 7
		}
		class, ok := fom.LookupInteractionClass(request)
		if !ok {
			t.Fatal(request)
		}
		param, ok := fom.LookupParameter(class, "HLAfederate")
		if !ok {
			t.Fatal("HLAfederate")
		}
		wire := &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: string(catalogFederation), FederateHandle: 7, InteractionClassHandle: uint64(class), Parameters: map[uint64][]byte{uint64(param): catalogHandle(uint64(target))}}
		if _, err := client.SendInteraction(ctx, wire); err != nil {
			t.Fatal(err)
		}
		reportClass, ok := fom.LookupInteractionClass(strings.ReplaceAll(request, "HLArequest", "HLAreport"))
		if !ok {
			t.Fatal("report class")
		}
		var report *rtiv1.ReceiveInteraction
		for report == nil {
			event, err := stream.Recv()
			if err != nil {
				t.Fatal(err)
			}
			if received := event.GetReceive(); received != nil && received.InteractionClassHandle == uint64(reportClass) {
				report = received
			}
		}
		if len(report.Parameters) != 3 {
			t.Fatalf("wrong report parameter count: %d", len(report.Parameters))
		}
		read := func(name string) []byte {
			p, ok := fom.LookupParameter(reportClass, name)
			if !ok {
				t.Fatal(name)
			}
			return report.Parameters[uint64(p)]
		}
		if !bytes.Equal(read("HLAfederate"), catalogHandle(uint64(target))) {
			t.Fatal("report used requester instead of target")
		}
		if readCatalogString(t, read("HLAtransportation")) != "HLAreliable" {
			t.Fatal("reported unused transport override")
		}
		field, want := "HLAinteractionCounts", wantInteractions
		if index == 2 {
			field, want = "HLAupdateCounts", wantObjects
		}
		if index == 3 {
			field, want = "HLAreflectCounts", wantObjects
		}
		b := read(field)
		if len(b) != 4+16*len(want) || binary.BigEndian.Uint32(b) != uint32(len(want)) {
			t.Fatalf("bad count array: % x", b)
		}
		seen := map[uint64]bool{}
		for off := 4; off < len(b); off += 16 {
			class := binary.BigEndian.Uint64(b[off+4:])
			if binary.BigEndian.Uint32(b[off:]) != 8 || seen[class] || want[class] == 0 || binary.BigEndian.Uint32(b[off+12:]) != want[class] {
				t.Fatalf("wrong class counter: % x", b[off:off+16])
			}
			seen[class] = true
		}
		wire.Parameters[uint64(param)] = []byte{0, 0, 0, 8}
		if _, err := client.SendInteraction(ctx, wire); err == nil {
			t.Fatal("RPC accepted malformed target")
		}
	}
	// No report is routed to the subject instead of the requester.
	targetEvents, stop, err := out.Subscribe(ctx, catalogFederation, catalogTarget)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stop(); err != nil {
			t.Error(err)
		}
	}()
	select {
	case events := <-targetEvents:
		t.Fatalf("unexpected target callbacks: %v", events)
	default:
	}
}
