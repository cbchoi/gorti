package server

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc/metadata"
)

func TestMOMResignReportsActualOutcomeBeforeRetiringSubject(t *testing.T) {
	const fed core.FederationName = "resign-report"
	ctx, srv, conn, _ := momCompletionReviewServer(t, fed)
	generation := momCompletionReviewCreate(ctx, t, conn, string(fed), "Parent")
	a := momCompletionReviewJoin(ctx, t, conn, string(fed), "caller", "", generation)
	b := momCompletionReviewJoin(ctx, t, conn, string(fed), "observer", "", generation)
	fom, err := srv.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	class, ok := fom.LookupInteractionClass(mom.ClassReportServiceInvocation)
	if !ok {
		t.Fatal("service report class missing")
	}
	if err := srv.declMgr.SubscribeInteractionClass(ctx, fed, core.FederateHandle(b.FederateHandle), class); err != nil {
		t.Fatal(err)
	}
	// The retiring subscriber must not make routing to survivors fail.
	if err := srv.declMgr.SubscribeInteractionClass(ctx, fed, core.FederateHandle(a.FederateHandle), class); err != nil {
		t.Fatal(err)
	}
	ch, release, err := srv.outbox.Subscribe(ctx, fed, core.FederateHandle(b.FederateHandle))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	manager := srv.momMgr.(*mom.Manager)
	manager.SetServiceReporting(fed, core.FederateHandle(a.FederateHandle), true)
	check := func(want int, success bool, serial uint32) {
		t.Helper()
		count := 0
		for _, event := range drainScheduledEvents(ch) {
			wire, err := grpcadapter.EncodeOutboundEvent(event)
			if err != nil {
				t.Fatal(err)
			}
			r := wire.GetReceive()
			if r == nil || r.InteractionClassHandle != uint64(class) {
				continue
			}
			value := func(name string) []byte {
				h, ok := fom.LookupParameter(class, name)
				if !ok {
					t.Fatalf("parameter %s missing", name)
				}
				return r.Parameters[uint64(h)]
			}
			if got := catalogRPCString(t, value("HLAservice")); got != momServiceResignFederationExecution {
				t.Fatalf("service=%s", got)
			}
			if !bytes.Equal(value("HLAfederate"), catalogRPCHandle(a.FederateHandle)) {
				t.Fatal("report attributed to wrong membership")
			}
			wantSuccess := uint32(0)
			if success {
				wantSuccess = 1
			}
			if got := binary.BigEndian.Uint32(value("HLAsuccessIndicator")); got != wantSuccess {
				t.Fatalf("success=%d, want %d", got, wantSuccess)
			}
			if got := binary.BigEndian.Uint32(value("HLAserialNumber")); got != serial {
				t.Fatalf("serial=%d, want %d", got, serial)
			}
			if exception := catalogRPCString(t, value("HLAexception")); (exception == "") != success {
				t.Fatalf("exception %q does not match success %v", exception, success)
			}
			count++
		}
		if count != want {
			t.Fatalf("resign reports=%d, want %d", count, want)
		}
	}
	client := rtiv1.NewFederationServiceClient(conn)
	signed := metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(a.SessionToken))
	request := &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: a.FederateHandle}
	if _, err := client.ResignFederation(signed, request); err == nil {
		t.Fatal("invalid resign action accepted")
	}
	check(1, false, 0)
	if _, ok := manager.QueryFederateAttributes(fed, core.FederateHandle(a.FederateHandle)); !ok {
		t.Fatal("failed resign removed subject")
	}
	request.Action = rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION
	if _, err := client.ResignFederation(signed, request); err != nil {
		t.Fatal(err)
	}
	check(1, true, 1)
	if _, ok := manager.QueryFederateAttributes(fed, core.FederateHandle(a.FederateHandle)); ok {
		t.Fatal("successful resign retained MOM subject")
	}
	if _, err := client.ResignFederation(signed, request); err == nil {
		t.Fatal("retired session resigned twice")
	}
	check(0, false, 0)
}
