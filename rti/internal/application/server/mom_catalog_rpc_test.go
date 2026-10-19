package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func catalogRPCHandle(v uint64) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint32(b, 8)
	binary.BigEndian.PutUint64(b[4:], v)
	return b
}
func catalogRPCInt(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }
func catalogRPCString(t *testing.T, b []byte) string {
	t.Helper()
	if len(b) < 4 || uint64(binary.BigEndian.Uint32(b))*2+4 != uint64(len(b)) {
		t.Fatalf("invalid string: %x", b)
	}
	units := make([]uint16, (len(b)-4)/2)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(b[4+2*i:])
	}
	return string(utf16.Decode(units))
}

// Requests traverse production newRTID composition, ObjectService and actual
// StreamService callbacks, with no catalog provider replaced by a test stub.
func TestMOMCatalogProductionRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	srv, err := newRTID(rtidConfig{OutboxBatchSize: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	done := make(chan error, 1)
	go func() { done <- srv.grpcS.Serve(lis) }()
	t.Cleanup(func() {
		_ = srv.momMgr.FederationDestroyed(context.Background(), "catalog-rpc")
		srv.grpcS.Stop()
		_ = lis.Close()
		<-done
		_ = srv.plugins.Close()
	})
	conn, err := stdgrpc.NewClient("passthrough:///mom-catalog", stdgrpc.WithTransportCredentials(insecure.NewCredentials()), stdgrpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	const fed core.FederationName = "catalog-rpc"
	base := []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>value</name><dataType>HLAinteger32BE</dataType><transportation>HLAreliable</transportation><order>Receive</order></attribute></objectClass></objectClass></objects></objectModel>`)
	joined := []byte(`<objectModel><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>JoinedSignal</name></interactionClass></interactionClass></interactions></objectModel>`)
	federations := rtiv1.NewFederationServiceClient(conn)
	if _, err := federations.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: string(fed), FomModules: []*rtiv1.FOMModule{{Path: "base.xml", Xml: base}}}); err != nil {
		t.Fatal(err)
	}
	joinA, err := federations.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: string(fed), FederateName: "producer", AdditionalFomModules: []*rtiv1.FOMModule{{Path: "joined.xml", Xml: joined}}})
	if err != nil {
		t.Fatal(err)
	}
	joinB, err := federations.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: string(fed), FederateName: "observer"})
	if err != nil {
		t.Fatal(err)
	}
	a, b := core.FederateHandle(joinA.FederateHandle), core.FederateHandle(joinB.FederateHandle)
	fom, err := srv.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"HLAmanager.HLAfederate.HLAreport", "HLAmanager.HLAfederation.HLAreport"} {
		class, ok := fom.LookupInteractionClass(name)
		if !ok {
			t.Fatal(name)
		}
		if err := srv.declMgr.SubscribeInteractionClass(ctx, fed, b, class); err != nil {
			t.Fatal(err)
		}
	}
	receiverCtx := metadata.AppendToOutgoingContext(ctx, "gorti-session-token-bin", string(joinB.SessionToken))
	stream, err := rtiv1.NewStreamServiceClient(conn).Events(receiverCtx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(b)})
	if err != nil {
		t.Fatal(err)
	}
	objects := rtiv1.NewObjectServiceClient(conn)
	send := func(class string, p map[string][]byte) error {
		t.Helper()
		c, ok := fom.LookupInteractionClass(class)
		if !ok {
			t.Fatal(class)
		}
		params := map[uint64][]byte{}
		for name, v := range p {
			h, ok := fom.LookupParameter(c, name)
			if !ok {
				t.Fatal(name)
			}
			params[uint64(h)] = v
		}
		_, err := objects.SendInteraction(ctx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(b), InteractionClassHandle: uint64(c), Parameters: params})
		return err
	}
	receive := func(class string) map[string][]byte {
		t.Helper()
		c, ok := fom.LookupInteractionClass(class)
		if !ok {
			t.Fatal(class)
		}
		for {
			e, err := stream.Recv()
			if err != nil {
				t.Fatal(err)
			}
			r := e.GetReceive()
			if r == nil || r.InteractionClassHandle != uint64(c) {
				continue
			}
			p := map[string][]byte{}
			for h, v := range r.Parameters {
				name, ok := fom.(core.FOMHandleNameLookup).ParameterName(c, core.ParameterHandle(h))
				if !ok {
					t.Fatal("unchecked parameter")
				}
				p[name] = v
			}
			return p
		}
	}
	request := func(class string, p map[string][]byte) map[string][]byte {
		t.Helper()
		if err := send(class, p); err != nil {
			t.Fatal(err)
		}
		return receive(strings.ReplaceAll(class, "HLArequest", "HLAreport"))
	}
	vehicle, _ := fom.LookupObjectClass("Vehicle")
	value, _ := fom.LookupAttribute(vehicle, "value")
	privilege, _ := fom.LookupAttribute(vehicle, "HLAprivilegeToDeleteObject")
	for _, h := range []core.FederateHandle{a, b} {
		if err := srv.declMgr.PublishObjectClassAttributes(ctx, fed, h, vehicle, []core.AttributeHandle{value, privilege}); err != nil {
			t.Fatal(err)
		}
	}
	if err := srv.declMgr.SubscribeObjectClassAttributes(ctx, fed, b, vehicle, []core.AttributeHandle{value}); err != nil {
		t.Fatal(err)
	}
	obj, _, err := srv.objReg.Register(ctx, fed, a, vehicle, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := objects.UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(a), ObjectHandle: uint64(obj), Attributes: map[uint64][]byte{uint64(value): {1}}}); err != nil {
			t.Fatal(err)
		}
	}
	support := rtiv1.NewSupportServiceClient(conn)
	for entered := 0; entered < 2; {
		event, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if reflected := event.GetReflect(); reflected == nil || reflected.ObjectHandle != uint64(obj) {
			continue
		}
		if len(event.CallbackReceipt) != 32 {
			t.Fatal("actual reflection has no mandatory callback receipt")
		}
		report := &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(b), CallbackReceipt: event.CallbackReceipt, InvocationEntry: true, Success: true}
		if _, err := support.ReportCallbackInvocation(ctx, report); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("unauthenticated reflection entry accepted: %v", err)
		}
		if _, err := support.ReportCallbackInvocation(receiverCtx, report); err != nil {
			t.Fatal("authenticated reflection entry:", err)
		}
		observed, err := rtiv1.NewMomServiceClient(conn).QueryFederateAttributes(receiverCtx, &rtiv1.QueryFederateAttributesRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: uint64(b)})
		if err != nil {
			t.Fatal(err)
		}
		if why := observed.UnavailableAttributes["HLAobjectInstancesReflected"]; why != "" {
			t.Fatalf("reflected-object count unavailable after authenticated entry, before completion: %s", why)
		}
		if reflected := observed.AttributeValues["HLAobjectInstancesReflected"]; len(reflected) != 4 || binary.BigEndian.Uint32(reflected) != 1 {
			t.Fatalf("reflected-object count after entry, before completion=%x, want 1", reflected)
		}
		report.InvocationEntry = false
		if _, err := support.ReportCallbackInvocation(receiverCtx, report); err != nil {
			t.Fatal("authenticated reflection completion:", err)
		}
		entered++
	}
	for _, tc := range []struct {
		class  string
		target core.FederateHandle
	}{{mom.ClassRequestObjectInstancesThatCanBeDeleted, a}, {mom.ClassRequestObjectInstancesUpdated, a}, {mom.ClassRequestObjectInstancesReflected, b}} {
		p := request(tc.class, map[string][]byte{"HLAfederate": catalogRPCHandle(uint64(tc.target))})
		encoded := p["HLAobjectInstanceCounts"]
		if len(p) != 2 || len(encoded) != 20 || binary.BigEndian.Uint64(encoded[8:]) != uint64(vehicle) || binary.BigEndian.Uint32(encoded[16:]) != 1 {
			t.Fatalf("%s distinct counts %x", tc.class, encoded)
		}
	}
	p := request(mom.ClassRequestObjectInstanceInformation, map[string][]byte{"HLAfederate": catalogRPCHandle(uint64(b)), "HLAobjectInstance": catalogRPCHandle(uint64(obj))})
	if len(p) != 5 || !bytes.Equal(p["HLAknownClass"], catalogRPCHandle(uint64(vehicle))) || !bytes.Equal(p["HLAownedInstanceAttributeList"], catalogRPCInt(0)) {
		t.Fatal("object information", p)
	}
	if err := send(mom.ClassRequestSubscriptions, map[string][]byte{"HLAfederate": catalogRPCHandle(uint64(b))}); err != nil {
		t.Fatal(err)
	}
	p = receive(mom.ClassReportObjectClassSubscription)
	if len(p) != 6 || !bytes.Equal(p["HLAobjectClass"], catalogRPCHandle(uint64(vehicle))) {
		t.Fatal("actual subscription report", p)
	}
	p = receive(mom.ClassReportInteractionSubscription)
	if len(p) != 2 || len(p["HLAinteractionClassList"]) <= 4 {
		t.Fatal("actual report subscriptions absent")
	}
	for _, tc := range []struct {
		class  string
		target core.FederateHandle
		source []byte
	}{{mom.ClassRequestFederationFOMModuleData, 0, base}, {mom.ClassRequestFederateFOMModuleData, a, joined}} {
		params := map[string][]byte{"HLAFOMmoduleIndicator": catalogRPCInt(0)}
		if tc.target != 0 {
			params["HLAfederate"] = catalogRPCHandle(uint64(tc.target))
		}
		p = request(tc.class, params)
		if catalogRPCString(t, p["HLAFOMmoduleData"]) != string(tc.source) {
			t.Fatal("module source replaced")
		}
	}
	if err := srv.syncMgr.Register(ctx, fed, testReadyLabel, nil, []core.FederateHandle{a, b}); err != nil {
		t.Fatal(err)
	}
	p = request(mom.ClassRequestSynchronizationPoints, nil)
	if len(p["HLAsyncPoints"]) < 8 || binary.BigEndian.Uint32(p["HLAsyncPoints"]) != 1 {
		t.Fatal("sync list")
	}
	p = request(mom.ClassRequestSynchronizationPointStatus, nil)
	if len(p) != 2 || catalogRPCString(t, p["HLAsyncPointName"]) != testReadyLabel || len(p["HLAsyncPointFederates"]) != 36 {
		t.Fatal("sync status")
	}
	if err := send(mom.ClassModifyAttributeState, map[string][]byte{"HLAfederate": catalogRPCHandle(uint64(b)), "HLAobjectInstance": catalogRPCHandle(uint64(obj)), "HLAattribute": catalogRPCHandle(uint64(value)), "HLAattributeState": catalogRPCInt(1)}); err != nil {
		t.Fatal(err)
	}
	if owner, _ := srv.ownMgr.QueryOwnership(fed, obj, value); owner != b {
		t.Fatal("MOM adjustment did not change actual owner")
	}
	if err := send(mom.ClassRequestObjectInstancesUpdated, map[string][]byte{"HLAfederate": {0, 0, 0, 8}}); err == nil {
		t.Fatal("malformed target RPC accepted")
	}
	if err := send(mom.ClassModifyAttributeState, map[string][]byte{"HLAfederate": catalogRPCHandle(uint64(b)), "HLAobjectInstance": catalogRPCHandle(uint64(obj)), "HLAattribute": catalogRPCHandle(uint64(value)), "HLAattributeState": catalogRPCInt(2)}); err == nil {
		t.Fatal("malformed ownership RPC accepted")
	}
	p = receive(mom.ClassReportMOMException)
	if !bytes.Equal(p["HLAparameterError"], catalogRPCInt(1)) {
		t.Fatal("MOM failure report flag")
	}
	manager := srv.momMgr.(*mom.Manager)
	momClass, _ := fom.LookupObjectClass(mom.ClassHLAfederate)
	updates, _ := fom.LookupAttribute(momClass, "HLAupdatesSent")
	if err := srv.declMgr.SubscribeObjectClassAttributes(ctx, fed, b, momClass, []core.AttributeHandle{updates}); err != nil {
		t.Fatal(err)
	}
	var momObject core.ObjectHandle
	for _, entry := range manager.ExportState(fed).Federates {
		if entry.Handle == a {
			momObject = entry.ObjectHandle
		}
	}
	if err := send(mom.ClassSetTiming, map[string][]byte{"HLAfederate": catalogRPCHandle(uint64(a)), "HLAreportPeriod": catalogRPCInt(1)}); err != nil {
		t.Fatal(err)
	}
	for {
		e, err := stream.Recv()
		if err != nil {
			period, tickErr := manager.PeriodicStatus(fed, a)
			attrs, _ := manager.QueryFederateAttributes(fed, a)
			t.Fatalf("%v; period=%d tick=%v attrs=%+v", err, period, tickErr, attrs)
		}
		r := e.GetReflect()
		if r != nil && r.ObjectHandle == uint64(momObject) {
			v := r.Attributes[uint64(updates)]
			if len(v) == 4 && binary.BigEndian.Uint32(v) == 2 {
				break
			}
		}
	}
	if err := send(mom.ClassSetTiming, map[string][]byte{"HLAfederate": catalogRPCHandle(uint64(a)), "HLAreportPeriod": catalogRPCInt(0)}); err != nil {
		t.Fatal(err)
	}
}
